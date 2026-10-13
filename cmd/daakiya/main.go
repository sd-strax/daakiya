// Command daakiya sorts cold outreach out of a Microsoft mailbox using a model
// the owner supplies. See docs/ for the design.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/sd-strax/daakiya/internal/actions"
	"github.com/sd-strax/daakiya/internal/auth"
	"github.com/sd-strax/daakiya/internal/classify"
	"github.com/sd-strax/daakiya/internal/config"
	"github.com/sd-strax/daakiya/internal/launchd"
	"github.com/sd-strax/daakiya/internal/llm"
	"github.com/sd-strax/daakiya/internal/mail"
	"github.com/sd-strax/daakiya/internal/mail/graph"
	"github.com/sd-strax/daakiya/internal/rules"
	"github.com/sd-strax/daakiya/internal/runner"
	"github.com/sd-strax/daakiya/internal/secrets"
	"github.com/sd-strax/daakiya/internal/store"
)

var version = "dev"

const usage = `daakiya - sorts cold outreach out of your inbox with a model you supply

Usage: daakiya <command> [flags]

Setup
  init                 guided setup: provider, key, app registration, sign-in, folders
  auth                 sign in to Microsoft again (device code)
  install              install the launchd job (every interval_seconds)
  uninstall [--revoke] remove the launchd job; --revoke also forgets tokens and keys

Operation
  run                  one pass: fetch new mail, classify, act, record
  status               last run, counts, mode, spend
  review [--mark] [-n N] [--unreviewed] [--category C] [--sample N] [--json]
                       recent decisions; --mark asks right/wrong per row
  undo <id>            move a message back to the Inbox and learn from it (id as shown by review)
  mark <id> <label>    grade one decision non-interactively (label: cold_outreach, marketing,
                       notification, correspondence, or ok to confirm)
  allow <addr|domain>  never treat this sender as cold outreach
  deny <addr|domain>   always treat this sender as cold outreach
  promote | demote     switch between live and shadow mode
  apply-pending [--since YYYY-MM-DD] [--dry-run]
                       in live mode, move messages whose recorded decision says move but
                       which were decided in shadow mode or before a folder was configured
  backfill --since YYYY-MM-DD [--apply] [--redo-unknown] [--reroute]
                       classify existing Inbox mail; --apply tags (shadow) or moves (live)
  eval [--provider P --model M --base-url U] [--json]
                       score the pipeline against your reviewed decisions
  refresh-correspondents
                       rebuild the known-people list from Sent Items now
  classify <message-id>
                       classify one message and print the result (no action)
  version
`

type app struct {
	paths config.Paths
	cfg   config.Config
	sec   secrets.Store
	st    *store.Store
	logf  func(string, ...any)
	out   io.Writer
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	paths, err := config.DefaultPaths()
	if err != nil {
		fatal(err)
	}
	a := &app{paths: paths, sec: secrets.Keychain{}, out: os.Stdout}
	a.logf = a.consoleLog

	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "version":
		fmt.Println("daakiya", version)
		return
	case "init":
		exitOn(a.cmdInit(ctx, args))
		return
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	}

	a.cfg, err = config.Load(paths.ConfigFile)
	if err != nil {
		fatal(err)
	}
	a.st, err = store.Open(paths.DBFile)
	if err != nil {
		fatal(err)
	}
	defer a.st.Close()

	switch cmd {
	case "auth":
		exitOn(a.cmdAuth(ctx))
	case "run":
		exitOn(a.cmdRun(ctx))
	case "install":
		exitOn(launchd.Install(paths.LaunchdPlist, a.cfg.IntervalSeconds, paths.LogFile))
		fmt.Printf("installed %s: `daakiya run` every %ds, log at %s\n", launchd.Label, a.cfg.IntervalSeconds, paths.LogFile)
	case "uninstall":
		exitOn(a.cmdUninstall(ctx, args))
	case "status":
		exitOn(a.cmdStatus(ctx))
	case "review":
		exitOn(a.cmdReview(ctx, args))
	case "undo":
		exitOn(a.cmdUndo(ctx, args))
	case "mark":
		exitOn(a.cmdMark(ctx, args))
	case "allow":
		exitOn(a.cmdList(ctx, args, store.ListAllowAddr, store.ListAllowDomain))
	case "deny":
		exitOn(a.cmdList(ctx, args, store.ListDenyAddr, store.ListDenyDomain))
	case "promote":
		exitOn(a.setMode(config.ModeLive))
	case "demote":
		exitOn(a.setMode(config.ModeShadow))
	case "backfill":
		exitOn(a.cmdBackfill(ctx, args))
	case "apply-pending":
		exitOn(a.cmdApplyPending(ctx, args))
	case "eval":
		exitOn(a.cmdEval(ctx, args))
	case "refresh-correspondents":
		r, err := a.runner(ctx, nil)
		exitOn(err)
		exitOn(r.RefreshCorrespondents(ctx))
	case "classify":
		exitOn(a.cmdClassify(ctx, args))
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "daakiya:", err)
	os.Exit(1)
}

func exitOn(err error) {
	if err != nil {
		fatal(err)
	}
}

func (a *app) consoleLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// fileLog appends to the log file and, when attached to a terminal, also to stderr.
func (a *app) fileLog() (func(string, ...any), func(), error) {
	if err := os.MkdirAll(a.paths.DataDir, 0o700); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(a.paths.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	rotate(a.paths.LogFile, f)
	l := log.New(f, "", log.LstdFlags)
	return func(format string, args ...any) { l.Printf(format, args...) }, func() { f.Close() }, nil
}

func rotate(path string, f *os.File) {
	if st, err := f.Stat(); err == nil && st.Size() > 10<<20 {
		f.Close()
		for i := 4; i >= 1; i-- {
			os.Rename(fmt.Sprintf("%s.%d", path, i), fmt.Sprintf("%s.%d", path, i+1))
		}
		os.Rename(path, path+".1")
	}
}

// --- wiring ---

func (a *app) authenticator() (*auth.Authenticator, error) {
	return auth.New(a.cfg.Mailbox.ClientID, a.cfg.Mailbox.TenantID, a.sec, a.paths.DataDir)
}

func (a *app) mailbox() (mail.Mailbox, *auth.Authenticator, error) {
	au, err := a.authenticator()
	if err != nil {
		return nil, nil, err
	}
	return graph.New(au), au, nil
}

func (a *app) classifier(override *llmOverride) (llm.Classifier, error) {
	provider, model, baseURL := a.cfg.LLM.Provider, a.cfg.LLM.Model, a.cfg.LLM.BaseURL
	if override != nil {
		if override.provider != "" {
			provider = override.provider
		}
		if override.model != "" {
			model = override.model
		}
		if override.baseURL != "" {
			baseURL = override.baseURL
		}
	}
	key, err := a.sec.Get(secrets.AccountLLMKey)
	if err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return nil, err
	}
	switch provider {
	case config.ProviderAnthropic:
		if key == "" {
			return nil, errors.New("no Anthropic API key: run `daakiya init` or set " + secrets.EnvLLMKey)
		}
		effort := "low"
		if override != nil && override.effort != "" {
			effort = override.effort
		}
		return llm.NewAnthropic(key, model, effort), nil
	case config.ProviderOpenAICompat:
		if baseURL == "" {
			return nil, errors.New("openai-compatible provider needs llm.base_url")
		}
		return llm.NewOpenAICompat(baseURL, key, model, a.cfg.LLM.JSONMode), nil
	}
	return nil, fmt.Errorf("unknown provider %q", provider)
}

type llmOverride struct{ provider, model, baseURL, effort string }

func (a *app) runner(ctx context.Context, override *llmOverride) (*runner.Runner, error) {
	mb, _, err := a.mailbox()
	if err != nil {
		return nil, err
	}
	cl, err := a.classifier(override)
	if err != nil {
		return nil, err
	}
	owner := map[string]bool{}
	if upn, _ := a.st.GetState(ctx, store.KeyAccountUPN); upn != "" {
		owner[strings.ToLower(upn)] = true
	}
	return &runner.Runner{Cfg: a.cfg, Mailbox: mb, Store: a.st, Classifier: cl, Log: a.logf, OwnerAddrs: owner}, nil
}

// --- commands ---

func (a *app) cmdInit(ctx context.Context, args []string) error {
	in := bufio.NewReader(os.Stdin)
	ask := func(prompt, def string) string {
		if def != "" {
			fmt.Printf("%s [%s]: ", prompt, def)
		} else {
			fmt.Printf("%s: ", prompt)
		}
		s, _ := in.ReadString('\n')
		s = strings.TrimSpace(s)
		if s == "" {
			return def
		}
		return s
	}

	cfg, err := config.Load(a.paths.ConfigFile)
	if err != nil && !errors.Is(err, config.ErrNotInitialized) {
		// A half-written config should not block re-running init.
		cfg = config.Default()
	}
	fmt.Println("Daakiya setup. Nothing here leaves your machine except to Microsoft and the model provider you choose.")
	fmt.Println()

	p := ask("LLM provider (anthropic | openai-compatible)", cfg.LLM.Provider)
	cfg.LLM.Provider = p
	switch p {
	case config.ProviderAnthropic:
		cfg.LLM.Model = ask("Model", cfg.LLM.Model)
		if cfg.LLM.InputPricePerM == 0 {
			cfg.LLM.InputPricePerM, cfg.LLM.OutputPricePerM = 0.10, 0.50
		}
	case config.ProviderOpenAICompat:
		def := cfg.LLM.BaseURL
		if def == "" {
			def = "http://localhost:11434/v1"
		}
		cfg.LLM.BaseURL = ask("Base URL (Ollama default shown)", def)
		if cfg.LLM.Model == "claude-haiku-5-5" {
			cfg.LLM.Model = ""
		}
		cfg.LLM.Model = ask("Model name as the server knows it", cfg.LLM.Model)
		cfg.LLM.InputPricePerM, cfg.LLM.OutputPricePerM = 0, 0
		if strings.Contains(cfg.LLM.BaseURL, "localhost") || strings.Contains(cfg.LLM.BaseURL, "127.0.0.1") {
			fmt.Println("Local endpoint: email excerpts will not leave this machine for classification.")
		}
	default:
		return fmt.Errorf("unknown provider %q", p)
	}
	if os.Getenv(secrets.EnvLLMKey) != "" {
		fmt.Printf("Using API key from %s.\n", secrets.EnvLLMKey)
	} else {
		existing, _ := a.sec.Get(secrets.AccountLLMKey)
		prompt := "API key (blank for none, e.g. local Ollama)"
		if existing != "" {
			prompt = "API key (blank keeps the stored one)"
		}
		key := readSecret(prompt)
		if key != "" {
			if err := a.sec.Set(secrets.AccountLLMKey, key); err != nil {
				return err
			}
			fmt.Println("Key stored in the macOS Keychain.")
		}
	}

	fmt.Println()
	fmt.Println("Microsoft app registration (see docs/08-roadmap.md spike 1 for the portal steps).")
	cfg.Mailbox.ClientID = ask("Application (client) ID", cfg.Mailbox.ClientID)
	cfg.Mailbox.TenantID = ask("Directory (tenant) ID, or 'common'", cfg.Mailbox.TenantID)

	fmt.Println()
	fmt.Println("Profile: two or three lines about your role, company, and which topics are legitimately yours.")
	fmt.Println("This is the single biggest quality lever for a small model. End with an empty line.")
	var lines []string
	for {
		s, _ := in.ReadString('\n')
		s = strings.TrimRight(s, "\r\n")
		if s == "" {
			break
		}
		lines = append(lines, s)
	}
	if len(lines) > 0 {
		cfg.Owner.Profile = strings.Join(lines, "\n")
	}
	cfg.Mode = config.ModeShadow
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := config.Save(a.paths.ConfigFile, cfg); err != nil {
		return err
	}
	a.cfg = cfg
	fmt.Printf("Config written to %s\n\n", a.paths.ConfigFile)

	a.st, err = store.Open(a.paths.DBFile)
	if err != nil {
		return err
	}
	defer a.st.Close()

	if err := a.cmdAuth(ctx); err != nil {
		return err
	}
	r, err := a.runner(ctx, nil)
	if err != nil {
		return err
	}
	fmt.Print("Creating folder and categories... ")
	if err := r.Bootstrap(ctx); err != nil {
		return err
	}
	fmt.Println("done.")
	fmt.Print("Scanning Sent Items for people you know... ")
	if err := r.RefreshCorrespondents(ctx); err != nil {
		return err
	}
	fmt.Println("done.")
	if _, err := r.Run(ctx); err != nil {
		return err
	}
	fmt.Println()
	fmt.Println("Mode: shadow. Decisions are recorded and would-be moves are tagged; nothing is moved.")
	fmt.Println("Next: `daakiya install` to run every few minutes, `daakiya review --mark` to grade decisions,")
	fmt.Println("      `daakiya eval` for precision, and `daakiya promote` when you trust it.")
	return nil
}

func readSecret(prompt string) string {
	fmt.Printf("%s: ", prompt)
	// Fall back to a plain read when not on a terminal.
	fd := int(os.Stdin.Fd())
	if b, err := readPassword(fd); err == nil {
		fmt.Println()
		return strings.TrimSpace(string(b))
	}
	s, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(s)
}

func (a *app) cmdAuth(ctx context.Context) error {
	mb, au, err := a.mailbox()
	if err != nil {
		return err
	}
	if _, err := au.SignIn(ctx, os.Stdout); err != nil {
		return fmt.Errorf("sign-in failed: %w", err)
	}
	upn, name, err := mb.WhoAmI(ctx)
	if err != nil {
		return err
	}
	if err := a.st.SetState(ctx, store.KeyAccountUPN, upn); err != nil {
		return err
	}
	fmt.Printf("Signed in as %s <%s>.\n", name, upn)
	if a.cfg.Owner.Domains == nil {
		if i := strings.Index(upn, "@"); i > 0 {
			a.cfg.Owner.Domains = []string{classify.RegistrableDomain(upn[i+1:])}
			_ = config.Save(a.paths.ConfigFile, a.cfg)
		}
	}
	for _, d := range a.cfg.Owner.Domains {
		_ = a.st.AddToList(ctx, store.ListAllowDomain, d, "owner_domain")
	}
	return nil
}

func (a *app) cmdRun(ctx context.Context) error {
	release, err := runner.Lock(a.paths.LockFile)
	if err != nil {
		return err
	}
	defer release()
	flog, closeLog, err := a.fileLog()
	if err != nil {
		return err
	}
	defer closeLog()
	tty := isTerminal(os.Stderr)
	a.logf = func(format string, args ...any) {
		flog(format, args...)
		if tty {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		}
	}
	r, err := a.runner(ctx, nil)
	if err != nil {
		a.logf("run aborted: %v", err)
		return err
	}
	sum, err := r.Run(ctx)
	if err != nil {
		a.logf("run failed: %v", err)
		_ = a.st.SetState(ctx, store.KeyLastRunNote, err.Error())
		return err
	}
	_ = a.st.SetState(ctx, store.KeyLastRunNote, sum.Note)
	a.logf("run: %d new, %d rule, %d llm, %d would-move, %d moved, %d errors %s",
		sum.NewMessages, sum.RuleDecisions, sum.LLMDecisions, sum.WouldMove, sum.Moved, sum.Errors, sum.Note)
	return nil
}

func (a *app) cmdUninstall(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	revoke := fs.Bool("revoke", false, "also forget Microsoft tokens and the LLM key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := launchd.Uninstall(a.paths.LaunchdPlist); err != nil {
		return err
	}
	fmt.Println("launchd job removed.")
	if *revoke {
		if au, err := a.authenticator(); err == nil {
			_ = au.SignOut(ctx)
		}
		_ = a.sec.Delete(secrets.AccountLLMKey)
		fmt.Println("Tokens and key removed from the Keychain.")
		fmt.Println("To revoke the app's access entirely: https://myapps.microsoft.com (work) or https://account.live.com/consent/Manage (personal).")
	}
	fmt.Printf("Data remains in %s; delete it to forget decisions. Messages in the Daakiya folder are untouched.\n", a.paths.DataDir)
	return nil
}

func (a *app) cmdStatus(ctx context.Context) error {
	w := tabwriter.NewWriter(a.out, 0, 2, 2, ' ', 0)
	defer w.Flush()
	fmt.Fprintf(w, "mode\t%s\n", a.cfg.Mode)
	fmt.Fprintf(w, "provider\t%s (%s)\n", a.cfg.LLM.Provider, a.cfg.LLM.Model)
	fmt.Fprintf(w, "threshold\t%.2f\n", a.cfg.LLM.Threshold)
	fmt.Fprintf(w, "folders\t%v\n", a.cfg.Folders)
	if len(a.cfg.Routes) > 0 {
		fmt.Fprintf(w, "routes\t%d sender rules\n", len(a.cfg.Routes))
	}
	fmt.Fprintf(w, "launchd\t%v\n", launchd.Installed(a.paths.LaunchdPlist))
	if upn, _ := a.st.GetState(ctx, store.KeyAccountUPN); upn != "" {
		fmt.Fprintf(w, "account\t%s\n", upn)
	}
	if au, err := a.authenticator(); err == nil {
		if _, err := au.Token(ctx); err != nil {
			fmt.Fprintf(w, "auth\tSIGN-IN REQUIRED (run `daakiya auth`)\n")
		} else {
			fmt.Fprintf(w, "auth\tok\n")
		}
	}
	if r, ok, _ := a.st.LastRun(ctx); ok {
		fmt.Fprintf(w, "last run\t%s (%d new, %d rule, %d llm, %d would-move, %d moved, %d errors) %s\n",
			r.FinishedAt.Local().Format(time.RFC3339), r.NewMessages, r.RuleDecisions, r.LLMDecisions, r.WouldMove, r.Moved, r.Errors, r.Note)
	} else {
		fmt.Fprintf(w, "last run\tnever\n")
	}
	day := time.Now().Truncate(24 * time.Hour)
	counts, _ := a.st.Counts(ctx, day)
	calls, _ := a.st.CountLLMCallsSince(ctx, day)
	cost, _ := a.st.CostSince(ctx, day)
	fmt.Fprintf(w, "today\t%d llm calls, $%.4f, categories %v\n", calls, cost, counts)
	week := time.Now().Add(-7 * 24 * time.Hour)
	counts, _ = a.st.Counts(ctx, week)
	cost, _ = a.st.CostSince(ctx, week)
	fmt.Fprintf(w, "7 days\t$%.4f, categories %v\n", cost, counts)
	unrev, _ := a.st.ListDecisions(ctx, store.DecisionFilter{Unreviewed: true, Limit: 1000})
	fmt.Fprintf(w, "unreviewed\t%d llm decisions (`daakiya review --mark`)\n", len(unrev))
	return nil
}

func (a *app) cmdReview(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	mark := fs.Bool("mark", false, "interactively mark each unreviewed decision right or wrong")
	judge := fs.Bool("judge", false, "grade unreviewed decisions with a stronger model; agreements become reviewed")
	judgeProvider := fs.String("provider", "", "judge provider (default anthropic)")
	judgeModel := fs.String("model", "claude-opus-5-5", "judge model")
	judgeBase := fs.String("base-url", "", "judge base URL for openai-compatible")
	n := fs.Int("n", 30, "rows")
	unreviewed := fs.Bool("unreviewed", false, "only unreviewed LLM decisions")
	asJSON := fs.Bool("json", false, "JSON lines output")
	category := fs.String("category", "", "only this category (cold_outreach, marketing, notification, correspondence, unknown)")
	sample := fs.Int("sample", 0, "with --mark: grade a random sample of this many rows instead of all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *judge {
		return a.judge(ctx, *judgeProvider, *judgeModel, *judgeBase, *n)
	}
	f := store.DecisionFilter{Limit: *n, Unreviewed: *unreviewed || *mark, Category: *category}
	if *sample > 0 {
		f.Limit = 0
	}
	rows, err := a.st.ListDecisions(ctx, f)
	if err != nil {
		return err
	}
	if *sample > 0 && len(rows) > *sample {
		rand.Shuffle(len(rows), func(i, j int) { rows[i], rows[j] = rows[j], rows[i] })
		rows = rows[:*sample]
	}
	if *asJSON {
		return writeJSONLines(a.out, rows)
	}
	if !*mark {
		printDecisions(a.out, rows)
		return nil
	}
	// Oldest first when marking, so the owner reads in arrival order.
	sort.Slice(rows, func(i, j int) bool { return rows[i].DecidedAt.Before(rows[j].DecidedAt) })
	in := bufio.NewReader(os.Stdin)
	fmt.Fprintf(a.out, "%d unreviewed. For each: y = correct, n = wrong, c/m/t/r = it was cold_outreach/marketing/notification/correspondence, s = skip, q = quit\n\n", len(rows))
	for _, d := range rows {
		fmt.Fprintf(a.out, "[%s] %s <%s>\n  %s\n  -> %s (%.2f, %s): %s\n",
			short(d.ID), d.FromName, d.FromAddr, d.Subject, d.Category, d.Confidence, d.Relationship, d.Reason)
		if d.JudgeLabel != "" {
			fmt.Fprintf(a.out, "  judge %s says: %s\n", d.JudgeModel, d.JudgeLabel)
		}
		fmt.Fprintf(a.out, "  %s\n", truncateStr(d.Excerpt, 300))
		fmt.Fprint(a.out, "  [y/n/c/m/t/r/s/q] ")
		s, _ := in.ReadString('\n')
		s = strings.ToLower(strings.TrimSpace(s))
		label := ""
		switch s {
		case "y":
			label = d.Category
		case "n":
			label = "wrong"
		case "c":
			label = llm.ColdOutreach
		case "m":
			label = llm.Marketing
		case "t":
			label = llm.Notification
		case "r":
			label = llm.Correspondence
		case "q":
			return nil
		default:
			continue
		}
		if err := a.applyLabel(ctx, d, label); err != nil {
			return err
		}
		fmt.Fprintln(a.out)
	}
	return nil
}

func (a *app) applier(ctx context.Context) (*actions.Applier, error) {
	r, err := a.runner(ctx, nil)
	if err != nil {
		return nil, err
	}
	if err := r.LoadFolders(ctx); err != nil {
		return nil, err
	}
	return &actions.Applier{Mailbox: r.Mailbox, Store: a.st, Cfg: a.cfg, Folders: r.Folders}, nil
}

func printDecisions(w io.Writer, rows []store.Decision) {
	tw := tabwriter.NewWriter(w, 0, 2, 1, ' ', 0)
	fmt.Fprintln(tw, "ID\tWHEN\tCATEGORY\tCONF\tSTAGE\tACTION\tFROM\tSUBJECT\tREASON")
	for _, d := range rows {
		act := d.Action
		if d.Action == store.ActionMove {
			act = "move:" + d.TargetFolder
			if d.ActionAt == nil {
				act = "would-" + act
			}
		}
		if d.Corrected != "" {
			act += "!" + d.Corrected
		}
		if d.ReviewedLabel != "" {
			act += " ✓"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%.2f\t%s\t%s\t%s\t%s\t%s\n", short(d.ID), d.DecidedAt.Local().Format("01-02 15:04"), d.Category, d.Confidence, d.Stage, act,
			truncateStr(d.FromAddr, 32), truncateStr(d.Subject, 44), truncateStr(d.Reason, 60))
	}
	tw.Flush()
}

func (a *app) cmdUndo(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: daakiya undo <id>")
	}
	d, err := a.st.FindDecisionByPrefix(ctx, args[0])
	if err != nil {
		return err
	}
	ap, err := a.applier(ctx)
	if err != nil {
		return err
	}
	if err := ap.Undo(ctx, d); err != nil {
		return err
	}
	fmt.Printf("Moved back to Inbox and allowlisted %s.\n", d.FromAddr)
	return nil
}

func (a *app) cmdList(ctx context.Context, args []string, addrKind, domainKind string) error {
	if len(args) != 1 {
		return errors.New("usage: daakiya allow|deny <address|domain>")
	}
	v := strings.ToLower(strings.TrimSpace(args[0]))
	kind := domainKind
	if strings.Contains(v, "@") {
		kind = addrKind
	} else {
		v = classify.RegistrableDomain(v)
	}
	// Allow and deny are exclusive.
	for _, k := range []string{store.ListAllowAddr, store.ListAllowDomain, store.ListDenyAddr, store.ListDenyDomain} {
		_ = a.st.RemoveFromList(ctx, k, v)
	}
	if err := a.st.AddToList(ctx, kind, v, "owner"); err != nil {
		return err
	}
	fmt.Printf("%s: %s\n", kind, v)
	return nil
}

func (a *app) setMode(mode string) error {
	a.cfg.Mode = mode
	if err := config.Save(a.paths.ConfigFile, a.cfg); err != nil {
		return err
	}
	if mode == config.ModeLive {
		for cat, folder := range a.cfg.Folders {
			fmt.Printf("Live mode: %s will be moved to %s/%s\n", cat, a.cfg.Mailbox.FolderName, folder)
		}
	} else {
		fmt.Println("Shadow mode: decisions recorded, nothing moved.")
	}
	return nil
}

func (a *app) cmdBackfill(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("backfill", flag.ContinueOnError)
	since := fs.String("since", "", "YYYY-MM-DD")
	apply := fs.Bool("apply", false, "apply actions (only has effect in live mode)")
	max := fs.Int("max", 500, "maximum messages")
	redo := fs.Bool("redo-unknown", false, "also re-run messages whose decision is unknown (e.g. after a provider error)")
	reroute := fs.Bool("reroute", false, "also re-run already-classified messages that match a sender route (after adding a route)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *since == "" {
		return errors.New("--since YYYY-MM-DD is required")
	}
	t, err := time.ParseInLocation("2006-01-02", *since, time.Local)
	if err != nil {
		return err
	}
	r, err := a.runner(ctx, nil)
	if err != nil {
		return err
	}
	if err := r.LoadFolders(ctx); err != nil {
		return err
	}
	msgs, err := r.Mailbox.ListInboxSince(ctx, t, *max)
	if err != nil {
		return err
	}
	var batch []mail.Message
	for _, m := range msgs {
		if m.IsDraft {
			continue
		}
		done, _ := a.st.HasDecision(ctx, m.ID)
		if done && *redo {
			if d, found, _ := a.st.GetDecision(ctx, m.ID); found && d.Category == llm.Unknown {
				done = false
			}
		}
		if done && *reroute {
			for _, rt := range a.cfg.Routes {
				if rules.MatchRoute(rules.Route{Match: rt.Match, Folder: rt.Folder}, m.From.Address) {
					done = false
				}
			}
		}
		if !done {
			batch = append(batch, m)
		}
	}
	fmt.Fprintf(os.Stderr, "%d messages since %s, %d not yet classified\n", len(msgs), *since, len(batch))
	res := r.Process(ctx, batch, *apply)
	fmt.Fprintf(os.Stderr, "done: %d rule, %d llm, %d would-move, %d moved, %d errors\n", res.Rules, res.LLM, res.WouldMove, res.Moved, res.Errors)
	return nil
}

func (a *app) cmdClassify(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: daakiya classify <message-id>")
	}
	r, err := a.runner(ctx, nil)
	if err != nil {
		return err
	}
	d, err := r.Classify(ctx, mail.Message{ID: args[0]})
	if err != nil {
		return err
	}
	return writeJSONLines(a.out, []store.Decision{d})
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// short returns the tail of a message id: Graph immutable ids share a long
// common prefix, so the end is what distinguishes them.
func short(id string) string {
	if len(id) > 8 {
		return id[len(id)-8:]
	}
	return id
}

func truncateStr(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// cmdApplyPending re-derives the action for every un-actioned decision from
// its recorded category and confidence using the current config, and applies
// moves. It lets a month of shadow decisions, or a newly configured folder,
// take effect without spending on the model again.
func (a *app) cmdApplyPending(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("apply-pending", flag.ContinueOnError)
	since := fs.String("since", "", "YYYY-MM-DD (default: 60 days ago)")
	dry := fs.Bool("dry-run", false, "list what would move without moving")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if a.cfg.Mode != config.ModeLive && !*dry {
		return errors.New("apply-pending only moves mail in live mode; run `daakiya promote` first, or use --dry-run")
	}
	from := time.Now().Add(-60 * 24 * time.Hour)
	if *since != "" {
		t, err := time.ParseInLocation("2006-01-02", *since, time.Local)
		if err != nil {
			return err
		}
		from = t
	}
	ap, err := a.applier(ctx)
	if err != nil {
		return err
	}
	rows, err := a.st.PendingCandidates(ctx, from)
	if err != nil {
		return err
	}
	var moved, skipped, failed int
	byFolder := map[string]int{}
	for _, d := range rows {
		known, _ := a.st.InList(ctx, store.ListCorrespondent, d.FromAddr)
		var action, folder string
		switch {
		case d.Rule == actions.RuleRoute:
			action, folder = d.Action, d.TargetFolder
		case d.Stage == "rule" && d.Category == llm.ColdOutreach:
			action, folder = store.ActionMove, a.cfg.Folders[llm.ColdOutreach]
		default:
			action, folder = classify.Decide(llm.Result{Category: d.Category, Confidence: d.Confidence, Relationship: d.Relationship}, a.cfg.LLM.Threshold, known, a.cfg.Folders)
		}
		if action != store.ActionMove || folder == "" {
			skipped++
			continue
		}
		// Only touch messages still in the Inbox; the owner may have filed them.
		loc, err := ap.Mailbox.Locate(ctx, d.ID)
		if err != nil || loc != ap.Folders.InboxID {
			skipped++
			continue
		}
		byFolder[folder]++
		if *dry {
			fmt.Fprintf(a.out, "would move  %-14s %-32s %s -> %s\n", d.Category, truncateStr(d.FromAddr, 32), truncateStr(d.Subject, 40), folder)
			continue
		}
		d.Action, d.TargetFolder, d.Mode = store.ActionMove, folder, a.cfg.Mode
		if err := ap.Apply(ctx, &d); err != nil {
			failed++
			a.logf("apply-pending: %s: %v", short(d.ID), err)
			continue
		}
		if err := a.st.UpsertDecision(ctx, d); err != nil {
			return err
		}
		moved++
	}
	if *dry {
		fmt.Fprintf(a.out, "would move %v, skipped %d\n", byFolder, skipped)
		return nil
	}
	fmt.Fprintf(a.out, "moved %d %v, skipped %d, failed %d\n", moved, byFolder, skipped, failed)
	return nil
}

// applyLabel records an owner grade and runs the learning side effects: a
// wrong cold_outreach call is a false positive (undone if it was moved), and a
// missed cold_outreach is a false negative.
func (a *app) applyLabel(ctx context.Context, d store.Decision, label string) error {
	if err := a.st.MarkReviewed(ctx, d.ID, label, "owner"); err != nil {
		return err
	}
	if label == "wrong" || label == d.Category {
		return nil
	}
	ap, err := a.applier(ctx)
	if err != nil {
		return err
	}
	if d.Category == llm.ColdOutreach && d.Corrected == "" {
		if d.ActionAt != nil {
			return ap.Undo(ctx, d)
		}
		return ap.RecordFalsePositive(ctx, d)
	}
	if label == llm.ColdOutreach && d.Corrected == "" {
		return ap.RecordFalseNegative(ctx, d)
	}
	return nil
}

func (a *app) cmdMark(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: daakiya mark <id> <cold_outreach|marketing|notification|correspondence|ok>")
	}
	d, err := a.st.FindDecisionByPrefix(ctx, args[0])
	if err != nil {
		return err
	}
	label := strings.ToLower(args[1])
	if label == "ok" || label == "y" {
		label = d.Category
	}
	valid := false
	for _, c := range llm.Categories {
		if label == c {
			valid = true
		}
	}
	if !valid {
		return fmt.Errorf("label must be one of %v or ok", llm.Categories)
	}
	if err := a.applyLabel(ctx, d, label); err != nil {
		return err
	}
	fmt.Printf("%s: %s -> %s\n", short(d.ID), d.Category, label)
	return nil
}
