package classify

import (
	"github.com/sd-strax/daakiya/internal/llm"
	"github.com/sd-strax/daakiya/internal/store"
)

// Thresholds are the confidence floors for moving a message.
type Thresholds struct {
	// Move is the floor for a sender you have no history with.
	Move float64
	// FamiliarDomain is the floor for a stranger at a domain you have written
	// to. Zero means "same as Move".
	FamiliarDomain float64
}

// Familiarity is what the local lists say about the sender.
type Familiarity struct {
	// Address: you have written to this exact address.
	Address bool
	// Domain: you have written to someone else at this registrable domain
	// (public mail providers excluded).
	Domain bool
}

// Decide maps a classification to an action and target folder. This is the
// only place an action is chosen for a model decision, and it is code, not the
// model. folders maps category to subfolder; a category without a folder never
// moves.
func Decide(r llm.Result, th Thresholds, fam Familiarity, folders map[string]string) (action, folder string) {
	folder = folders[r.Category]
	if folder == "" {
		return store.ActionNone, ""
	}
	if fam.Address {
		return store.ActionNone, ""
	}
	floor := th.Move
	if fam.Domain && th.FamiliarDomain > floor {
		floor = th.FamiliarDomain
	}
	if r.Confidence < floor {
		return store.ActionNone, ""
	}
	if r.Category == llm.ColdOutreach && r.Relationship == llm.RelStrong {
		return store.ActionNone, ""
	}
	return store.ActionMove, folder
}
