package rules

import (
	"context"
	"testing"

	"github.com/sd-strax/daakiya/internal/classify"
	"github.com/sd-strax/daakiya/internal/llm"
	"github.com/sd-strax/daakiya/internal/store"
)

type fakeLists map[string]bool

func (f fakeLists) InList(_ context.Context, kind, v string) (bool, error) { return f[kind+":"+v], nil }

func TestEvaluateOrder(t *testing.T) {
	lists := fakeLists{
		store.ListCorrespondent + ":friend@x.com": true,
		store.ListSentConversation + ":conv1":     true,
		store.ListAllowDomain + ":partner.com":    true,
		store.ListDenyDomain + ":spammy.io":       true,
	}
	ctx := context.Background()
	cases := []struct {
		name string
		n    classify.Normalized
		conv string
		cat  string
		rule string
	}{
		{"known", classify.Normalized{FromAddr: "friend@x.com", FromDomain: "x.com"}, "", llm.Correspondence, "known_correspondent"},
		{"thread", classify.Normalized{FromAddr: "new@y.com", FromDomain: "y.com"}, "conv1", llm.Correspondence, "in_thread"},
		{"allow domain", classify.Normalized{FromAddr: "a@partner.com", FromDomain: "partner.com"}, "", llm.Correspondence, "allow_domain"},
		{"deny domain", classify.Normalized{FromAddr: "a@spammy.io", FromDomain: "spammy.io"}, "", llm.ColdOutreach, "deny_domain"},
		{"automated", classify.Normalized{FromAddr: "noreply@saas.com", FromDomain: "saas.com", FromLocal: "noreply"}, "", llm.Notification, "automated_sender"},
		{"bulk", classify.Normalized{FromAddr: "news@z.com", FromDomain: "z.com", FromLocal: "news", Signals: llm.Signals{Bulk: true}}, "", llm.Notification, "automated_headers"},
	}
	for _, c := range cases {
		v, err := Evaluate(ctx, lists, c.n, c.conv)
		if err != nil || !v.Matched || v.Category != c.cat || v.Rule != c.rule {
			t.Errorf("%s: got %+v (%v), want %s/%s", c.name, v, err, c.cat, c.rule)
		}
	}
	// Allowlisted sender that is also denylisted: allow wins (earlier rule).
	lists[store.ListAllowAddr+":a@spammy.io"] = true
	v, _ := Evaluate(ctx, lists, classify.Normalized{FromAddr: "a@spammy.io", FromDomain: "spammy.io"}, "")
	if v.Category != llm.Correspondence {
		t.Errorf("allow should beat deny, got %+v", v)
	}
	// Ambiguous human mail reaches the model.
	v, _ = Evaluate(ctx, lists, classify.Normalized{FromAddr: "joe@growth.io", FromDomain: "growth.io", FromLocal: "joe"}, "")
	if v.Matched {
		t.Errorf("ambiguous mail should not match a rule: %+v", v)
	}
}
