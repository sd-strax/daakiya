package classify

import (
	"testing"

	"github.com/sd-strax/daakiya/internal/llm"
	"github.com/sd-strax/daakiya/internal/store"
)

func TestDecide(t *testing.T) {
	cases := []struct {
		name  string
		r     llm.Result
		known bool
		want  string
	}{
		{"confident cold", llm.Result{Category: llm.ColdOutreach, Confidence: 0.95, Relationship: llm.RelNone}, false, store.ActionMove},
		{"below threshold", llm.Result{Category: llm.ColdOutreach, Confidence: 0.8, Relationship: llm.RelNone}, false, store.ActionNone},
		{"strong relationship guard", llm.Result{Category: llm.ColdOutreach, Confidence: 0.99, Relationship: llm.RelStrong}, false, store.ActionNone},
		{"known correspondent guard", llm.Result{Category: llm.ColdOutreach, Confidence: 0.99, Relationship: llm.RelNone}, true, store.ActionNone},
		{"marketing never moves", llm.Result{Category: llm.Marketing, Confidence: 1, Relationship: llm.RelNone}, false, store.ActionNone},
		{"unknown never moves", llm.Result{Category: llm.Unknown, Confidence: 1}, false, store.ActionNone},
	}
	for _, c := range cases {
		if got := Decide(c.r, 0.85, c.known); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}
