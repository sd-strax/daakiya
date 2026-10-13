// Package rules is the deterministic stage that runs before any model call.
package rules

import (
	"context"
	"strings"

	"github.com/sd-strax/daakiya/internal/classify"
	"github.com/sd-strax/daakiya/internal/llm"
	"github.com/sd-strax/daakiya/internal/store"
)

// Verdict is a rule-stage decision. Matched=false means "ambiguous, ask the model".
type Verdict struct {
	Matched    bool
	Category   string
	Confidence float64
	Reason     string
	Rule       string
}

var automatedLocalParts = map[string]bool{
	"noreply": true, "no-reply": true, "no_reply": true, "donotreply": true, "do-not-reply": true, "do_not_reply": true,
	"notifications": true, "notification": true, "notify": true, "mailer-daemon": true, "postmaster": true,
	"alerts": true, "alert": true, "billing": true, "receipts": true, "receipt": true, "invoices": true,
	"support": true, "security": true, "calendar": true, "bounce": true, "bounces": true, "system": true,
}

type Lists interface {
	InList(ctx context.Context, kind, value string) (bool, error)
}

// Evaluate applies the rules in order; first match wins.
func Evaluate(ctx context.Context, lists Lists, n classify.Normalized, conversationID string) (Verdict, error) {
	in := func(kind, v string) (bool, error) {
		if v == "" {
			return false, nil
		}
		return lists.InList(ctx, kind, v)
	}

	// 1. Known correspondent.
	if ok, err := in(store.ListCorrespondent, n.FromAddr); err != nil || ok {
		return verdict(ok, llm.Correspondence, 1.0, "sender is someone you have written to", "known_correspondent"), err
	}
	// 2. In-thread reply to a conversation the owner took part in.
	if ok, err := in(store.ListSentConversation, conversationID); err != nil || ok {
		return verdict(ok, llm.Correspondence, 1.0, "reply in a thread you took part in", "in_thread"), err
	}
	// 3. Allowlist.
	if ok, err := in(store.ListAllowAddr, n.FromAddr); err != nil || ok {
		return verdict(ok, llm.Correspondence, 1.0, "sender address is allowlisted", "allow_addr"), err
	}
	if ok, err := in(store.ListAllowDomain, n.FromDomain); err != nil || ok {
		return verdict(ok, llm.Correspondence, 1.0, "sender domain is allowlisted", "allow_domain"), err
	}
	// 4. Denylist.
	if ok, err := in(store.ListDenyAddr, n.FromAddr); err != nil || ok {
		return verdict(ok, llm.ColdOutreach, 1.0, "sender address is denylisted", "deny_addr"), err
	}
	if ok, err := in(store.ListDenyDomain, n.FromDomain); err != nil || ok {
		return verdict(ok, llm.ColdOutreach, 1.0, "sender domain is denylisted", "deny_domain"), err
	}
	// 5. Automated mail: never worth a model call.
	if n.Signals.AutoSubmitted || n.Signals.Bulk {
		return verdict(true, llm.Notification, 0.9, "automated or bulk headers", "automated_headers"), nil
	}
	if automatedLocalParts[strings.ToLower(n.FromLocal)] && !n.Signals.InReplyTo {
		return verdict(true, llm.Notification, 0.9, "automated sender address", "automated_sender"), nil
	}
	return Verdict{}, nil
}

func verdict(matched bool, cat string, conf float64, reason, rule string) Verdict {
	if !matched {
		return Verdict{}
	}
	return Verdict{Matched: true, Category: cat, Confidence: conf, Reason: reason, Rule: rule}
}
