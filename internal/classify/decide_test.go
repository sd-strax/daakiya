package classify

import (
	"testing"

	"github.com/sd-strax/daakiya/internal/llm"
	"github.com/sd-strax/daakiya/internal/store"
)

func TestDecide(t *testing.T) {
	folders := map[string]string{"cold_outreach": "Cold Outreach", "marketing": "Marketing"}
	cases := []struct {
		name   string
		r      llm.Result
		known  bool
		want   string
		folder string
	}{
		{"confident cold", llm.Result{Category: llm.ColdOutreach, Confidence: 0.95, Relationship: llm.RelNone}, false, store.ActionMove, "Cold Outreach"},
		{"below threshold", llm.Result{Category: llm.ColdOutreach, Confidence: 0.8, Relationship: llm.RelNone}, false, store.ActionNone, ""},
		{"strong relationship guard", llm.Result{Category: llm.ColdOutreach, Confidence: 0.99, Relationship: llm.RelStrong}, false, store.ActionNone, ""},
		{"known correspondent guard", llm.Result{Category: llm.ColdOutreach, Confidence: 0.99, Relationship: llm.RelNone}, true, store.ActionNone, ""},
		{"marketing with folder moves", llm.Result{Category: llm.Marketing, Confidence: 0.9, Relationship: llm.RelNone}, false, store.ActionMove, "Marketing"},
		{"notification has no folder", llm.Result{Category: llm.Notification, Confidence: 1, Relationship: llm.RelNone}, false, store.ActionNone, ""},
		{"unknown never moves", llm.Result{Category: llm.Unknown, Confidence: 1}, false, store.ActionNone, ""},
	}
	for _, c := range cases {
		got, folder := Decide(c.r, 0.85, c.known, folders)
		if got != c.want || folder != c.folder {
			t.Errorf("%s: got %s/%q want %s/%q", c.name, got, folder, c.want, c.folder)
		}
	}
	if a, _ := Decide(llm.Result{Category: llm.Marketing, Confidence: 1}, 0.85, false, map[string]string{"cold_outreach": "x"}); a != store.ActionNone {
		t.Error("marketing without a configured folder must not move")
	}
}
