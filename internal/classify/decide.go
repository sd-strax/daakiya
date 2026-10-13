package classify

import (
	"github.com/sd-strax/daakiya/internal/llm"
	"github.com/sd-strax/daakiya/internal/store"
)

// Decide maps a classification to an action and target folder. This is the
// only place an action is chosen for a model decision, and it is code, not the
// model. folders maps category to subfolder; a category without a folder never
// moves.
func Decide(r llm.Result, threshold float64, knownCorrespondent bool, folders map[string]string) (action, folder string) {
	folder = folders[r.Category]
	if folder == "" {
		return store.ActionNone, ""
	}
	if r.Confidence < threshold {
		return store.ActionNone, ""
	}
	if knownCorrespondent {
		return store.ActionNone, ""
	}
	if r.Category == llm.ColdOutreach && r.Relationship == llm.RelStrong {
		return store.ActionNone, ""
	}
	return store.ActionMove, folder
}
