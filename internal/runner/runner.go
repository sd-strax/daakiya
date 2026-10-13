// Package runner orchestrates one pass: delta sync, pipeline, actions, feedback.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sd-strax/daakiya/internal/actions"
	"github.com/sd-strax/daakiya/internal/classify"
	"github.com/sd-strax/daakiya/internal/config"
	"github.com/sd-strax/daakiya/internal/feedback"
	"github.com/sd-strax/daakiya/internal/llm"
	"github.com/sd-strax/daakiya/internal/mail"
	"github.com/sd-strax/daakiya/internal/rules"
	"github.com/sd-strax/daakiya/internal/store"
)

type Runner struct {
	Cfg        config.Config
	Mailbox    mail.Mailbox
	Store      *store.Store
	Classifier llm.Classifier
	Log        func(format string, args ...any)
	OwnerAddrs map[string]bool
	Folders    mail.Folders
}

var ErrBudget = errors.New("daily LLM call budget exhausted")

// LoadFolders reads cached folder IDs or bootstraps them.
func (r *Runner) LoadFolders(ctx context.Context) error {
	f := mail.Folders{}
	f.InboxID, _ = r.Store.GetState(ctx, store.KeyInboxID)
	f.FolderID, _ = r.Store.GetState(ctx, store.KeyFolderID)
	f.SubfolderID, _ = r.Store.GetState(ctx, store.KeySubfolderID)
	if f.InboxID != "" && f.FolderID != "" && f.SubfolderID != "" {
		// Verify the subfolder still exists; recreate if the owner deleted it.
		if _, err := r.Mailbox.ListFolder(ctx, f.SubfolderID, 1); err == nil {
			r.Folders = f
			return nil
		}
		r.Log("folder missing, re-bootstrapping")
	}
	return r.Bootstrap(ctx)
}

func (r *Runner) Bootstrap(ctx context.Context) error {
	cats := []string{r.Cfg.Mailbox.Category, r.Cfg.Mailbox.ShadowCategory}
	f, err := r.Mailbox.Bootstrap(ctx, r.Cfg.Mailbox.FolderName, r.Cfg.Mailbox.SubfolderName, cats)
	if err != nil {
		return err
	}
	r.Folders = f
	_ = r.Store.SetState(ctx, store.KeyInboxID, f.InboxID)
	_ = r.Store.SetState(ctx, store.KeyFolderID, f.FolderID)
	_ = r.Store.SetState(ctx, store.KeySubfolderID, f.SubfolderID)
	return nil
}

// Run performs one scheduled pass.
func (r *Runner) Run(ctx context.Context) (store.RunSummary, error) {
	sum := store.RunSummary{StartedAt: time.Now()}
	defer func() {
		sum.FinishedAt = time.Now()
		_ = r.Store.RecordRun(ctx, sum)
		_ = r.Store.SetState(ctx, store.KeyLastRunAt, sum.FinishedAt.UTC().Format(time.RFC3339))
	}()

	if err := r.LoadFolders(ctx); err != nil {
		sum.Note = "bootstrap: " + err.Error()
		return sum, err
	}
	if err := r.refreshCorrespondents(ctx, false); err != nil {
		r.Log("correspondents refresh failed: %v", err)
		sum.Errors++
	}

	cursor, _ := r.Store.GetState(ctx, store.KeyDeltaLink)
	if cursor == "" {
		c, err := r.Mailbox.InitialCursor(ctx)
		if err != nil {
			sum.Note = "initial cursor: " + err.Error()
			return sum, err
		}
		if err := r.Store.SetState(ctx, store.KeyDeltaLink, c); err != nil {
			return sum, err
		}
		sum.Note = "first run: cursor set, nothing processed (use `daakiya backfill` for existing mail)"
		r.Log("%s", sum.Note)
		return sum, nil
	}

	delta, err := r.Mailbox.NewMessages(ctx, cursor)
	if err != nil {
		sum.Note = "delta: " + err.Error()
		return sum, err
	}
	// Retry deferred decisions from earlier runs.
	deferred, _ := r.Store.ListDecisions(ctx, store.DecisionFilter{OnlyAction: store.ActionDeferred, Limit: 50})
	var batch []mail.Message
	for _, m := range delta.New {
		if m.IsDraft {
			continue
		}
		if done, _ := r.Store.HasDecision(ctx, m.ID); done {
			continue
		}
		batch = append(batch, m)
	}
	for _, d := range deferred {
		if d.Attempts >= 3 {
			_ = r.Store.SetAction(ctx, d.ID, store.ActionNone, time.Now())
			continue
		}
		batch = append(batch, mail.Message{ID: d.ID})
	}
	sum.NewMessages = len(delta.New)

	res := r.Process(ctx, batch, true)
	sum.RuleDecisions, sum.LLMDecisions, sum.WouldMove, sum.Moved, sum.Errors = res.Rules, res.LLM, res.WouldMove, res.Moved, res.Errors+sum.Errors

	if err := r.Store.SetState(ctx, store.KeyDeltaLink, delta.Cursor); err != nil {
		return sum, err
	}

	fb := &feedback.Scanner{Applier: r.applier(), Log: r.Log}
	if fp, fn, err := fb.Scan(ctx); err != nil {
		r.Log("feedback scan failed: %v", err)
		sum.Errors++
	} else if fp+fn > 0 {
		sum.Note = fmt.Sprintf("feedback: %d fp, %d fn", fp, fn)
	}
	_ = r.Store.PruneExamples(ctx, 200)
	return sum, nil
}

func (r *Runner) applier() *actions.Applier {
	return &actions.Applier{Mailbox: r.Mailbox, Store: r.Store, Cfg: r.Cfg, Folders: r.Folders}
}

// ProcessResult tallies one batch.
type ProcessResult struct {
	Rules, LLM, WouldMove, Moved, Errors int
}

// Process classifies a batch of messages and applies actions when apply is true.
// Messages with only an ID are re-fetched.
func (r *Runner) Process(ctx context.Context, batch []mail.Message, apply bool) ProcessResult {
	var res ProcessResult
	var mu sync.Mutex
	sem := make(chan struct{}, max(1, r.Cfg.LLM.Concurrency))
	var wg sync.WaitGroup
	applier := r.applier()

	for _, m := range batch {
		wg.Add(1)
		sem <- struct{}{}
		go func(m mail.Message) {
			defer wg.Done()
			defer func() { <-sem }()
			d, err := r.classifyOne(ctx, m)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				res.Errors++
				r.Log("error: %s: %v", m.ID, err)
				if d.ID != "" {
					_ = r.Store.UpsertDecision(ctx, d)
				}
				return
			}
			if d.Stage == "llm" {
				res.LLM++
			} else {
				res.Rules++
			}
			if d.Action == store.ActionMove {
				res.WouldMove++
				if apply {
					if err := applier.Apply(ctx, &d); err != nil {
						res.Errors++
						r.Log("apply failed for %s: %v", d.ID, err)
					} else if r.Cfg.Mode == config.ModeLive {
						res.Moved++
					}
				}
			}
			if err := r.Store.UpsertDecision(ctx, d); err != nil {
				res.Errors++
				r.Log("store failed for %s: %v", d.ID, err)
			}
			r.Log("%-14s %.2f %-7s %-40s %s", d.Category, d.Confidence, d.Stage, truncate(d.FromAddr, 40), truncate(d.Subject, 60))
		}(m)
	}
	wg.Wait()
	return res
}

// Classify runs the pipeline for one message without applying anything.
func (r *Runner) Classify(ctx context.Context, m mail.Message) (store.Decision, error) {
	return r.classifyOne(ctx, m)
}

func (r *Runner) classifyOne(ctx context.Context, m mail.Message) (store.Decision, error) {
	// Always fetch the full message: delta listings carry no body or headers.
	full, err := r.Mailbox.Fetch(ctx, m.ID)
	if err != nil {
		if errors.Is(err, mail.ErrNotFound) {
			return store.Decision{}, fmt.Errorf("message gone before classification")
		}
		return store.Decision{}, err
	}
	n := classify.Normalize(full, classify.Options{MaxBodyChars: r.Cfg.LLM.MaxBodyChars, Redact: r.Cfg.Privacy.Redact, OwnerAddrs: r.OwnerAddrs})
	sig, _ := json.Marshal(n.Signals)
	d := store.Decision{
		ID: full.ID, InternetMessageID: full.InternetMessageID, ConversationID: full.ConversationID, ReceivedAt: full.ReceivedAt,
		FromName: n.FromName, FromAddr: n.FromAddr, FromDomain: n.FromDomain, Subject: n.Subject, Excerpt: n.Body, Signals: string(sig),
		Mode: r.Cfg.Mode, DecidedAt: time.Now(), Action: store.ActionNone, PromptVersion: llm.PromptVersion,
	}
	if prev, found, _ := r.Store.GetDecision(ctx, full.ID); found {
		d.Attempts = prev.Attempts
	}

	v, err := rules.Evaluate(ctx, r.Store, n, full.ConversationID)
	if err != nil {
		return d, err
	}
	if v.Matched {
		d.Category, d.Confidence, d.Reason, d.Stage, d.Relationship = v.Category, v.Confidence, v.Reason, "rule", llm.RelNone
		if v.Category == llm.ColdOutreach {
			d.Action = store.ActionMove
		}
		return d, nil
	}

	// LLM stage, subject to the daily budget.
	calls, _ := r.Store.CountLLMCallsSince(ctx, startOfDay(time.Now()))
	if calls >= r.Cfg.LLM.MaxCallsPerDay {
		d.Category, d.Confidence, d.Reason, d.Stage, d.Action = llm.Unknown, 0, "daily budget exhausted", "deferred", store.ActionDeferred
		d.Attempts++
		return d, ErrBudget
	}
	known, _ := r.Store.InList(ctx, store.ListCorrespondent, n.FromAddr)
	examples := r.examples(ctx)
	start := time.Now()
	res, usage, err := r.Classifier.Classify(ctx, llm.SystemPrompt(r.Cfg.Owner.Profile), examples, classify.ToInput(n, known))
	d.LatencyMS = time.Since(start).Milliseconds()
	d.Provider, d.Model, d.Stage = r.Classifier.Name(), r.Classifier.Model(), "llm"
	d.InputTokens, d.OutputTokens = usage.InputTokens, usage.OutputTokens
	d.CostUSD = float64(usage.InputTokens)/1e6*r.Cfg.LLM.InputPricePerM + float64(usage.OutputTokens)/1e6*r.Cfg.LLM.OutputPricePerM
	if err != nil {
		if errors.Is(err, llm.ErrInvalidOutput) {
			d.Category, d.Confidence, d.Reason, d.Relationship = llm.Unknown, 0, "invalid model output: "+truncate(err.Error(), 120), llm.RelNone
			return d, nil
		}
		// Transport or provider failure: defer and retry next run.
		d.Category, d.Confidence, d.Reason, d.Stage, d.Action = llm.Unknown, 0, "provider error: "+truncate(err.Error(), 120), "deferred", store.ActionDeferred
		d.Attempts++
		return d, err
	}
	d.Category, d.Confidence, d.Reason, d.Relationship = res.Category, res.Confidence, res.Reason, res.Relationship
	d.Action = classify.Decide(res, r.Cfg.LLM.Threshold, known)
	return d, nil
}

func (r *Runner) examples(ctx context.Context) []llm.Example {
	rows, err := r.Store.Examples(ctx, r.Cfg.LLM.MaxExamples)
	if err != nil || len(rows) == 0 {
		return llm.SeedExamples
	}
	out := make([]llm.Example, 0, len(rows))
	for _, e := range rows {
		out = append(out, llm.Example{Category: e.Category, FromDomain: e.FromDomain, Subject: e.Subject, Excerpt: truncate(e.Excerpt, 600)})
	}
	return out
}

// refreshCorrespondents rebuilds the known-correspondent list from Sent Items
// once a day, or now when force is set.
func (r *Runner) refreshCorrespondents(ctx context.Context, force bool) error {
	last, _ := r.Store.GetState(ctx, store.KeyCorrespondentsAt)
	if !force && last != "" {
		if t, err := time.Parse(time.RFC3339, last); err == nil && time.Since(t) < 24*time.Hour {
			return nil
		}
	}
	since := time.Now().AddDate(0, -r.Cfg.Mailbox.CorrespondentMonths, 0)
	addrs, convs, err := r.Mailbox.KnownCorrespondents(ctx, since, 2000)
	if err != nil {
		return err
	}
	for a := range r.OwnerAddrs {
		addrs = append(addrs, a)
	}
	if err := r.Store.ReplaceList(ctx, store.ListCorrespondent, "sent_items", addrs); err != nil {
		return err
	}
	if err := r.Store.ReplaceList(ctx, store.ListSentConversation, "sent_items", convs); err != nil {
		return err
	}
	r.Log("correspondents: %d addresses, %d conversations", len(addrs), len(convs))
	return r.Store.SetState(ctx, store.KeyCorrespondentsAt, time.Now().UTC().Format(time.RFC3339))
}

func (r *Runner) RefreshCorrespondents(ctx context.Context) error { return r.refreshCorrespondents(ctx, true) }

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// Lock prevents overlapping runs. It is advisory: a stale lock older than an
// hour is ignored.
func Lock(path string) (release func(), err error) {
	if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < time.Hour {
		return nil, fmt.Errorf("another run is in progress (lock %s)", path)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(fmt.Sprint(os.Getpid()))), 0o600); err != nil {
		return nil, err
	}
	return func() { _ = os.Remove(path) }, nil
}
