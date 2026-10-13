package classify

import (
	"github.com/sd-strax/daakiya/internal/llm"
	"github.com/sd-strax/daakiya/internal/store"
)

// Decide maps a classification to an action. This is the only place an action is
// chosen, and it is code, not the model.
func Decide(r llm.Result, threshold float64, knownCorrespondent bool) string {
	if r.Category != llm.ColdOutreach {
		return store.ActionNone
	}
	if r.Confidence < threshold {
		return store.ActionNone
	}
	if r.Relationship == llm.RelStrong {
		return store.ActionNone
	}
	if knownCorrespondent {
		return store.ActionNone
	}
	return store.ActionMove
}
