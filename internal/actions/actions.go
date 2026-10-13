// Package actions applies a decision to the mailbox and reverses it on undo.
package actions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sd-strax/daakiya/internal/config"
	"github.com/sd-strax/daakiya/internal/llm"
	"github.com/sd-strax/daakiya/internal/mail"
	"github.com/sd-strax/daakiya/internal/store"
)

type Applier struct {
	Mailbox mail.Mailbox
	Store   *store.Store
	Cfg     config.Config
	Folders mail.Folders
}

// Apply performs the decision's action. In shadow mode it only tags.
func (a *Applier) Apply(ctx context.Context, d *store.Decision) error {
	if d.Action != store.ActionMove {
		return nil
	}
	now := time.Now()
	if a.Cfg.Mode == config.ModeShadow {
		if a.Cfg.Shadow.Tag {
			if err := a.addCategory(ctx, d.ID, a.Cfg.Mailbox.ShadowCategory); err != nil {
				return err
			}
		}
		return nil
	}
	if err := a.addCategory(ctx, d.ID, a.Cfg.Mailbox.Category); err != nil {
		return err
	}
	newID, err := a.Mailbox.Move(ctx, d.ID, a.Folders.SubfolderID)
	if err != nil {
		return err
	}
	if newID != d.ID {
		// Immutable IDs should keep this equal; record the new one if not.
		d.ID = newID
	}
	d.ActionAt = &now
	return nil
}

func (a *Applier) addCategory(ctx context.Context, id, cat string) error {
	cur, err := a.Mailbox.GetCategories(ctx, id)
	if err != nil {
		return err
	}
	for _, c := range cur {
		if c == cat {
			return nil
		}
	}
	return a.Mailbox.SetCategories(ctx, id, append(cur, cat))
}

func (a *Applier) removeCategories(ctx context.Context, id string, cats ...string) error {
	cur, err := a.Mailbox.GetCategories(ctx, id)
	if err != nil {
		return err
	}
	var keep []string
	for _, c := range cur {
		drop := false
		for _, x := range cats {
			if c == x {
				drop = true
			}
		}
		if !drop {
			keep = append(keep, c)
		}
	}
	if len(keep) == len(cur) {
		return nil
	}
	return a.Mailbox.SetCategories(ctx, id, keep)
}

// Undo moves a message back to the Inbox, clears tags, records the false
// positive, and teaches the lists.
func (a *Applier) Undo(ctx context.Context, d store.Decision) error {
	loc, err := a.Mailbox.Locate(ctx, d.ID)
	if err != nil && !errors.Is(err, mail.ErrNotFound) {
		return err
	}
	if err == nil && loc != a.Folders.InboxID {
		if _, err := a.Mailbox.Move(ctx, d.ID, a.Folders.InboxID); err != nil {
			return fmt.Errorf("move back: %w", err)
		}
	}
	if err == nil {
		_ = a.removeCategories(ctx, d.ID, a.Cfg.Mailbox.Category, a.Cfg.Mailbox.ShadowCategory)
	}
	return a.RecordFalsePositive(ctx, d)
}

// RecordFalsePositive marks a decision wrong and grows the allowlist and examples.
func (a *Applier) RecordFalsePositive(ctx context.Context, d store.Decision) error {
	if err := a.Store.MarkCorrected(ctx, d.ID, "fp"); err != nil {
		return err
	}
	if d.FromAddr != "" {
		if err := a.Store.AddToList(ctx, store.ListAllowAddr, d.FromAddr, "correction"); err != nil {
			return err
		}
	}
	return a.Store.AddExample(ctx, store.Example{
		Category: llm.Correspondence, FromDomain: d.FromDomain, Subject: d.Subject, Excerpt: d.Excerpt, Source: "correction",
	})
}

// RecordFalseNegative records a message the owner filed as cold outreach themselves.
func (a *Applier) RecordFalseNegative(ctx context.Context, d store.Decision) error {
	if err := a.Store.MarkCorrected(ctx, d.ID, "fn"); err != nil {
		return err
	}
	return a.Store.AddExample(ctx, store.Example{
		Category: llm.ColdOutreach, FromDomain: d.FromDomain, Subject: d.Subject, Excerpt: d.Excerpt, Source: "correction",
	})
}
