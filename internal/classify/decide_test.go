package classify

import (
	"testing"

	"github.com/sd-strax/daakiya/internal/llm"
	"github.com/sd-strax/daakiya/internal/store"
)

func TestDecide(t *testing.T) {
	folders := map[string]string{"cold_outreach": "Cold Outreach", "marketing": "Marketing"}
	th := Thresholds{Move: 0.85, FamiliarDomain: 0.95}
	cases := []struct {
		name   string
		r      llm.Result
		fam    Familiarity
		want   string
		folder string
	}{
		{"confident cold", llm.Result{Category: llm.ColdOutreach, Confidence: 0.95, Relationship: llm.RelNone}, Familiarity{}, store.ActionMove, "Cold Outreach"},
		{"below threshold", llm.Result{Category: llm.ColdOutreach, Confidence: 0.8, Relationship: llm.RelNone}, Familiarity{}, store.ActionNone, ""},
		{"strong relationship guard", llm.Result{Category: llm.ColdOutreach, Confidence: 0.99, Relationship: llm.RelStrong}, Familiarity{}, store.ActionNone, ""},
		{"known address guard", llm.Result{Category: llm.ColdOutreach, Confidence: 0.99, Relationship: llm.RelNone}, Familiarity{Address: true}, store.ActionNone, ""},
		{"familiar domain below its floor", llm.Result{Category: llm.ColdOutreach, Confidence: 0.90, Relationship: llm.RelNone}, Familiarity{Domain: true}, store.ActionNone, ""},
		{"familiar domain above its floor", llm.Result{Category: llm.ColdOutreach, Confidence: 0.97, Relationship: llm.RelNone}, Familiarity{Domain: true}, store.ActionMove, "Cold Outreach"},
		{"familiar domain applies to marketing too", llm.Result{Category: llm.Marketing, Confidence: 0.90, Relationship: llm.RelNone}, Familiarity{Domain: true}, store.ActionNone, ""},
		{"marketing with folder moves", llm.Result{Category: llm.Marketing, Confidence: 0.9, Relationship: llm.RelNone}, Familiarity{}, store.ActionMove, "Marketing"},
		{"notification has no folder", llm.Result{Category: llm.Notification, Confidence: 1, Relationship: llm.RelNone}, Familiarity{}, store.ActionNone, ""},
		{"unknown never moves", llm.Result{Category: llm.Unknown, Confidence: 1}, Familiarity{}, store.ActionNone, ""},
	}
	for _, c := range cases {
		got, folder := Decide(c.r, th, c.fam, folders)
		if got != c.want || folder != c.folder {
			t.Errorf("%s: got %s/%q want %s/%q", c.name, got, folder, c.want, c.folder)
		}
	}
	if a, _ := Decide(llm.Result{Category: llm.Marketing, Confidence: 1}, th, Familiarity{}, map[string]string{"cold_outreach": "x"}); a != store.ActionNone {
		t.Error("marketing without a configured folder must not move")
	}
	// A zero familiar threshold falls back to the move threshold.
	if a, _ := Decide(llm.Result{Category: llm.ColdOutreach, Confidence: 0.86}, Thresholds{Move: 0.85}, Familiarity{Domain: true}, folders); a != store.ActionMove {
		t.Error("zero familiar threshold should not block a move above the base threshold")
	}
}

func TestFamiliarDomains(t *testing.T) {
	got := FamiliarDomains([]string{"a@mail.trellix.com", "b@trellix.com", "c@gmail.com", "d@x.co.uk", "bad", "E@TRELLIX.com"})
	want := []string{"trellix.com", "x.co.uk"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v want %v", got, want)
		}
	}
}
