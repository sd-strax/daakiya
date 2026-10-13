package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/sd-strax/daakiya/internal/llm"
	"github.com/sd-strax/daakiya/internal/store"
)

// judge replays unreviewed LLM decisions through a stronger model on the same
// stored input. Agreement marks the row reviewed with source "judge";
// disagreement records the judge's label and leaves the row for the owner.
func (a *app) judge(ctx context.Context, provider, model, baseURL string, limit int) error {
	rows, err := a.st.ListDecisions(ctx, store.DecisionFilter{Unreviewed: true, Limit: limit})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintln(a.out, "nothing unreviewed")
		return nil
	}
	cl, err := a.classifier(&llmOverride{provider: provider, model: model, baseURL: baseURL, effort: "medium"})
	if err != nil {
		return err
	}
	sys := llm.SystemPrompt(a.cfg.Owner.Profile)
	examples := llm.SeedExamples
	fmt.Fprintf(a.out, "judging %d decisions with %s (%s)\n", len(rows), cl.Name(), cl.Model())
	var agreed, disagreed, failed int
	var cost float64
	type dis struct{ d store.Decision }
	var disagreements []store.Decision
	for i, d := range rows {
		var sig llm.Signals
		_ = json.Unmarshal([]byte(d.Signals), &sig)
		known, _ := a.st.InList(ctx, store.ListCorrespondent, d.FromAddr)
		in := llm.Input{FromName: d.FromName, FromAddr: d.FromAddr, FromDomain: d.FromDomain, KnownToOwner: known, Subject: d.Subject, Signals: sig, Body: d.Excerpt}
		res, usage, err := cl.Classify(ctx, sys, examples, in)
		cost += float64(usage.InputTokens)/1e6*4.0 + float64(usage.OutputTokens)/1e6*20.0
		if err != nil {
			failed++
			a.logf("judge failed for %s: %v", short(d.ID), err)
			continue
		}
		if res.Category == d.Category {
			agreed++
			if err := a.st.MarkReviewed(ctx, d.ID, d.Category, "judge"); err != nil {
				return err
			}
		} else {
			disagreed++
			if err := a.st.SetJudge(ctx, d.ID, res.Category, cl.Model()); err != nil {
				return err
			}
			d.JudgeLabel = res.Category
			disagreements = append(disagreements, d)
		}
		if (i+1)%20 == 0 {
			fmt.Fprintf(a.out, "  %d/%d (agree %d, disagree %d)\n", i+1, len(rows), agreed, disagreed)
		}
	}
	fmt.Fprintf(a.out, "\nagreed %d, disagreed %d, failed %d; est. judge cost $%.2f (Anthropic list prices; other providers differ)\n", agreed, disagreed, failed, cost)
	if len(disagreements) > 0 {
		sort.Slice(disagreements, func(i, j int) bool { return disagreements[i].Category < disagreements[j].Category })
		fmt.Fprintln(a.out, "\nDisagreements (yours to settle with `daakiya review --mark`):")
		for _, d := range disagreements {
			fmt.Fprintf(a.out, "  [%s] %-14s judge: %-14s %-30s %s\n", short(d.ID), d.Category, d.JudgeLabel, truncateStr(d.FromAddr, 30), truncateStr(d.Subject, 50))
		}
	}
	return nil
}
