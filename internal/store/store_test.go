package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	d := Decision{ID: "abc123", FromAddr: "joe@x.io", FromDomain: "x.io", Subject: "hi", Category: "cold_outreach", Confidence: 0.9,
		Stage: "llm", Action: ActionMove, Mode: "shadow", DecidedAt: time.Now(), CostUSD: 0.0001}
	if err := s.UpsertDecision(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, found, err := s.GetDecision(ctx, "abc123")
	if err != nil || !found || got.FromAddr != "joe@x.io" || got.Action != ActionMove {
		t.Fatalf("get: %+v %v %v", got, found, err)
	}
	if ok, _ := s.HasDecision(ctx, "abc123"); !ok {
		t.Error("HasDecision false")
	}
	byPrefix, err := s.FindDecisionByPrefix(ctx, "abc")
	if err != nil || byPrefix.ID != "abc123" {
		t.Errorf("prefix: %v %v", byPrefix.ID, err)
	}
	if err := s.MarkReviewed(ctx, "abc123", "cold_outreach", "owner"); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.ListDecisions(ctx, DecisionFilter{Reviewed: true})
	if len(rows) != 1 || rows[0].ReviewedLabel != "cold_outreach" {
		t.Errorf("reviewed filter: %+v", rows)
	}
	rows, _ = s.ListDecisions(ctx, DecisionFilter{Unreviewed: true})
	if len(rows) != 0 {
		t.Errorf("unreviewed should be empty: %+v", rows)
	}
	if err := s.MarkCorrected(ctx, "abc123", "fp"); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.ListDecisions(ctx, DecisionFilter{OnlyAction: ActionMove, Uncorrected: true})
	if len(rows) != 0 {
		t.Errorf("corrected row should be excluded: %+v", rows)
	}

	// Deferred rows do not count as decided.
	if err := s.UpsertDecision(ctx, Decision{ID: "def", Category: "unknown", Stage: "deferred", Action: ActionDeferred, Mode: "shadow", DecidedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.HasDecision(ctx, "def"); ok {
		t.Error("deferred should not count as decided")
	}

	// Lists and state.
	_ = s.AddToList(ctx, ListAllowDomain, "x.io", "test")
	if ok, _ := s.InList(ctx, ListAllowDomain, "x.io"); !ok {
		t.Error("list add failed")
	}
	_ = s.ReplaceList(ctx, ListCorrespondent, "t", []string{"a@b.c", "d@e.f"})
	if l, _ := s.List(ctx, ListCorrespondent); len(l) != 2 {
		t.Errorf("replace list: %v", l)
	}
	_ = s.SetState(ctx, KeyDeltaLink, "link1")
	_ = s.SetState(ctx, KeyDeltaLink, "link2")
	if v, _ := s.GetState(ctx, KeyDeltaLink); v != "link2" {
		t.Errorf("state upsert: %q", v)
	}
	calls, _ := s.CountLLMCallsSince(ctx, time.Now().Add(-time.Hour))
	cost, _ := s.CostSince(ctx, time.Now().Add(-time.Hour))
	if calls != 1 || cost < 0.00009 {
		t.Errorf("calls=%d cost=%f", calls, cost)
	}

	// Examples prefer corrections and prune.
	for i := 0; i < 5; i++ {
		_ = s.AddExample(ctx, Example{Category: "marketing", Source: "seed", Subject: "s"})
	}
	_ = s.AddExample(ctx, Example{Category: "correspondence", Source: "correction", Subject: "c"})
	ex, _ := s.Examples(ctx, 3)
	if len(ex) != 3 || ex[0].Source != "correction" {
		t.Errorf("examples ordering: %+v", ex)
	}
	_ = s.PruneExamples(ctx, 2)
	ex, _ = s.Examples(ctx, 10)
	if len(ex) != 2 || ex[0].Source != "correction" {
		t.Errorf("prune: %+v", ex)
	}

	// Runs.
	_ = s.RecordRun(ctx, RunSummary{StartedAt: time.Now(), FinishedAt: time.Now(), NewMessages: 3, Note: "n"})
	if r, ok, _ := s.LastRun(ctx); !ok || r.NewMessages != 3 {
		t.Errorf("last run: %+v %v", r, ok)
	}
}
