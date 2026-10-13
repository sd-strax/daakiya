// Package feedback detects the owner's corrections from mailbox state.
package feedback

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sd-strax/daakiya/internal/actions"
	"github.com/sd-strax/daakiya/internal/llm"
	"github.com/sd-strax/daakiya/internal/mail"
	"github.com/sd-strax/daakiya/internal/store"
)

type Scanner struct {
	Applier *actions.Applier
	Log     func(format string, args ...any)
}

// Scan looks for two things: moved messages that are no longer in the Daakiya
// folder (false positives) and messages in the folder that Daakiya never moved
// (false negatives the owner filed by hand).
func (s *Scanner) Scan(ctx context.Context) (fp, fn int, err error) {
	a := s.Applier
	since := time.Now().Add(-30 * 24 * time.Hour)
	moved, err := a.Store.ListDecisions(ctx, store.DecisionFilter{OnlyAction: store.ActionMove, Since: since, Uncorrected: true, Limit: 500})
	if err != nil {
		return 0, 0, err
	}
	for _, d := range moved {
		if d.ActionAt == nil {
			continue // shadow decision, never moved
		}
		loc, err := a.Mailbox.Locate(ctx, d.ID)
		if errors.Is(err, mail.ErrNotFound) {
			continue // deleted by owner: no signal either way
		}
		if err != nil {
			return fp, fn, err
		}
		if loc != a.Folders.SubfolderID {
			if err := a.RecordFalsePositive(ctx, d); err != nil {
				return fp, fn, err
			}
			fp++
			s.Log("feedback: false positive, %s moved out of the folder by owner", d.FromAddr)
		}
	}

	inFolder, err := a.Mailbox.ListFolder(ctx, a.Folders.SubfolderID, 500)
	if err != nil {
		return fp, fn, err
	}
	for _, m := range inFolder {
		d, found, err := a.Store.GetDecision(ctx, m.ID)
		if err != nil {
			return fp, fn, err
		}
		if found && d.Action == store.ActionMove && d.ActionAt != nil {
			continue // we put it there
		}
		if found && d.Corrected != "" {
			continue
		}
		if !found {
			d = store.Decision{
				ID: m.ID, InternetMessageID: m.InternetMessageID, ConversationID: m.ConversationID, ReceivedAt: m.ReceivedAt,
				FromName: m.From.Name, FromAddr: m.From.Address, Subject: m.Subject, Excerpt: m.BodyPreview,
				Category: llm.ColdOutreach, Confidence: 1, Reason: "filed by owner", Stage: "owner",
				Action: store.ActionNone, Mode: a.Cfg.Mode, DecidedAt: time.Now(),
			}
			if err := a.Store.UpsertDecision(ctx, d); err != nil {
				return fp, fn, err
			}
		}
		if err := a.RecordFalseNegative(ctx, d); err != nil {
			return fp, fn, err
		}
		fn++
		s.Log("feedback: false negative, owner filed %s", d.FromAddr)
		if n, _ := a.Store.ConfirmedColdFromDomain(ctx, d.FromDomain); n >= 3 && d.FromDomain != "" {
			_ = a.Store.AddToList(ctx, store.ListDenyDomain, d.FromDomain, fmt.Sprintf("auto after %d confirmed", n))
		}
	}
	return fp, fn, nil
}
