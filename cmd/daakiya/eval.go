package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/sd-strax/daakiya/internal/classify"
	"github.com/sd-strax/daakiya/internal/llm"
	"github.com/sd-strax/daakiya/internal/store"
)

type evalRow struct {
	ID       string
	Label    string // what the owner said
	Category string
	Conf     float64
	Rel      string
	Known    bool
	Tokens   int64
	Cost     float64
	Latency  int64
}

// cmdEval scores reviewed decisions. Without overrides it scores the recorded
// decisions; with --provider/--model it replays the stored excerpts through
// another classifier and scores that.
func (a *app) cmdEval(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	provider := fs.String("provider", "", "anthropic | openai-compatible")
	model := fs.String("model", "", "model name")
	baseURL := fs.String("base-url", "", "base URL for openai-compatible")
	asJSON := fs.Bool("json", false, "JSON output")
	limit := fs.Int("n", 1000, "max reviewed rows")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rows, err := a.st.ListDecisions(ctx, store.DecisionFilter{Reviewed: true, Limit: *limit})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("no reviewed decisions yet; run `daakiya review --mark` first")
	}
	replay := *provider != "" || *model != "" || *baseURL != ""
	var cl llm.Classifier
	if replay {
		if cl, err = a.classifier(&llmOverride{provider: *provider, model: *model, baseURL: *baseURL}); err != nil {
			return err
		}
	}

	var ev []evalRow
	for _, d := range rows {
		label := d.ReviewedLabel
		if label == "wrong" {
			continue // marked wrong without saying what it was: unusable for scoring
		}
		known, _ := a.st.InList(ctx, store.ListCorrespondent, d.FromAddr)
		r := evalRow{ID: d.ID, Label: label, Category: d.Category, Conf: d.Confidence, Rel: d.Relationship, Known: known, Tokens: d.InputTokens + d.OutputTokens, Cost: d.CostUSD, Latency: d.LatencyMS}
		if replay {
			var sig llm.Signals
			_ = json.Unmarshal([]byte(d.Signals), &sig)
			in := llm.Input{FromName: d.FromName, FromAddr: d.FromAddr, FromDomain: d.FromDomain, KnownToOwner: known, Subject: d.Subject, Signals: sig, Body: d.Excerpt}
			start := time.Now()
			res, usage, err := cl.Classify(ctx, llm.SystemPrompt(a.cfg.Owner.Profile), llm.SeedExamples, in)
			r.Latency = time.Since(start).Milliseconds()
			r.Tokens = usage.InputTokens + usage.OutputTokens
			r.Cost = float64(usage.InputTokens)/1e6*a.cfg.LLM.InputPricePerM + float64(usage.OutputTokens)/1e6*a.cfg.LLM.OutputPricePerM
			if err != nil {
				r.Category, r.Conf = llm.Unknown, 0
			} else {
				r.Category, r.Conf, r.Rel = res.Category, res.Confidence, res.Relationship
			}
			fmt.Fprintf(a.out, ".")
		}
		ev = append(ev, r)
	}
	if replay {
		fmt.Fprintln(a.out)
	}
	rep := score(ev, a.cfg.LLM.Threshold)
	if *asJSON {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	printReport(a.out, rep, cl)
	return nil
}

type catScore struct {
	Category                 string
	TP, FP, FN, Support      int
	Precision, Recall        float64
}

type thresholdScore struct {
	Threshold         float64
	TP, FP, FN        int
	Precision, Recall float64
}

type report struct {
	Rows       int
	Categories []catScore
	// Thresholds scores the action decision (move or not) for cold_outreach.
	Thresholds []thresholdScore
	Tokens     int64
	Cost       float64
	LatencyP50 int64
	LatencyP95 int64
}

func score(ev []evalRow, threshold float64) report {
	rep := report{Rows: len(ev)}
	cats := map[string]*catScore{}
	for _, c := range llm.Categories {
		cats[c] = &catScore{Category: c}
	}
	var lat []int64
	for _, r := range ev {
		if cs, ok := cats[r.Label]; ok {
			cs.Support++
		}
		if r.Category == r.Label {
			cats[r.Category].TP++
		} else {
			if cs, ok := cats[r.Category]; ok {
				cs.FP++
			}
			if cs, ok := cats[r.Label]; ok {
				cs.FN++
			}
		}
		rep.Tokens += r.Tokens
		rep.Cost += r.Cost
		if r.Latency > 0 {
			lat = append(lat, r.Latency)
		}
	}
	for _, c := range llm.Categories {
		cs := cats[c]
		cs.Precision = ratio(cs.TP, cs.TP+cs.FP)
		cs.Recall = ratio(cs.TP, cs.TP+cs.FN)
		rep.Categories = append(rep.Categories, *cs)
	}
	for _, t := range []float64{0.5, 0.6, 0.7, 0.75, 0.8, 0.85, 0.9, 0.95, threshold} {
		ts := thresholdScore{Threshold: t}
		for _, r := range ev {
			move := classify.Decide(llm.Result{Category: r.Category, Confidence: r.Conf, Relationship: r.Rel}, t, r.Known) == store.ActionMove
			truth := r.Label == llm.ColdOutreach
			switch {
			case move && truth:
				ts.TP++
			case move && !truth:
				ts.FP++
			case !move && truth:
				ts.FN++
			}
		}
		ts.Precision, ts.Recall = ratio(ts.TP, ts.TP+ts.FP), ratio(ts.TP, ts.TP+ts.FN)
		rep.Thresholds = append(rep.Thresholds, ts)
	}
	sort.Slice(rep.Thresholds, func(i, j int) bool { return rep.Thresholds[i].Threshold < rep.Thresholds[j].Threshold })
	if len(lat) > 0 {
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
		rep.LatencyP50 = lat[len(lat)/2]
		rep.LatencyP95 = lat[(len(lat)*95)/100]
	}
	return rep
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func printReport(w io.Writer, rep report, cl llm.Classifier) {
	if cl != nil {
		fmt.Fprintf(w, "Replayed %d reviewed decisions through %s (%s)\n\n", rep.Rows, cl.Name(), cl.Model())
	} else {
		fmt.Fprintf(w, "Scoring %d reviewed decisions as recorded\n\n", rep.Rows)
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "CATEGORY\tSUPPORT\tTP\tFP\tFN\tPRECISION\tRECALL")
	for _, c := range rep.Categories {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%.3f\t%.3f\n", c.Category, c.Support, c.TP, c.FP, c.FN, c.Precision, c.Recall)
	}
	tw.Flush()
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Move decision for cold_outreach by threshold (gate: precision >= 0.98, recall >= 0.80):")
	tw = tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "THRESHOLD\tTP\tFP\tFN\tPRECISION\tRECALL")
	for _, t := range rep.Thresholds {
		fmt.Fprintf(tw, "%.2f\t%d\t%d\t%d\t%.3f\t%.3f\n", t.Threshold, t.TP, t.FP, t.FN, t.Precision, t.Recall)
	}
	tw.Flush()
	fmt.Fprintf(w, "\ntokens %d, est. cost $%.4f, latency p50 %dms p95 %dms\n", rep.Tokens, rep.Cost, rep.LatencyP50, rep.LatencyP95)
}

func writeJSONLines(w io.Writer, rows []store.Decision) error {
	enc := json.NewEncoder(w)
	for _, d := range rows {
		if err := enc.Encode(d); err != nil {
			return err
		}
	}
	return nil
}

var _ = context.Background
