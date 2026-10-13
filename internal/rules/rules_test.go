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
func (f fakeLists) ConversationHasCorrespondence(_ context.Context, c string) (bool, error) {
	return f["conv_corr:"+c], nil
}

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
		v, err := Evaluate(ctx, lists, c.n, c.conv, nil)
		if err != nil || !v.Matched || v.Category != c.cat || v.Rule != c.rule {
			t.Errorf("%s: got %+v (%v), want %s/%s", c.name, v, err, c.cat, c.rule)
		}
	}
	// Allowlisted sender that is also denylisted: allow wins (earlier rule).
	lists[store.ListAllowAddr+":a@spammy.io"] = true
	v, _ := Evaluate(ctx, lists, classify.Normalized{FromAddr: "a@spammy.io", FromDomain: "spammy.io"}, "", nil)
	if v.Category != llm.Correspondence {
		t.Errorf("allow should beat deny, got %+v", v)
	}
	// Ambiguous human mail reaches the model.
	v, _ = Evaluate(ctx, lists, classify.Normalized{FromAddr: "joe@growth.io", FromDomain: "growth.io", FromLocal: "joe"}, "", nil)
	if v.Matched {
		t.Errorf("ambiguous mail should not match a rule: %+v", v)
	}
}

func TestRoutes(t *testing.T) {
	routes := []Route{{Match: "linkedin.com", Folder: "LinkedIn"}, {Match: "bob@partner.com", Folder: "Partners"}}
	for addr, want := range map[string]bool{
		"jobs-noreply@linkedin.com": true, "x@mail.linkedin.com": true, "x@notlinkedin.com": false,
		"bob@partner.com": true, "alice@partner.com": false, "BOB@PARTNER.COM": true,
	} {
		got := false
		for _, r := range routes {
			if MatchRoute(r, addr) {
				got = true
			}
		}
		if got != want {
			t.Errorf("%s: got %v want %v", addr, got, want)
		}
	}
	// A route beats the allowlist and the automated-sender rule, but not a known correspondent.
	lists := fakeLists{store.ListAllowDomain + ":linkedin.com": true, store.ListCorrespondent + ":friend@linkedin.com": true}
	v, _ := Evaluate(context.Background(), lists, classify.Normalized{FromAddr: "jobs-noreply@linkedin.com", FromDomain: "linkedin.com", FromLocal: "jobs-noreply"}, "", routes)
	if v.Rule != RuleRoute || v.Folder != "LinkedIn" || v.Category != llm.Notification {
		t.Errorf("route verdict: %+v", v)
	}
	v, _ = Evaluate(context.Background(), lists, classify.Normalized{FromAddr: "friend@linkedin.com", FromDomain: "linkedin.com"}, "", routes)
	if v.Rule != "known_correspondent" {
		t.Errorf("known correspondent should win over route: %+v", v)
	}
}

func TestThreadConsistency(t *testing.T) {
	lists := fakeLists{"conv_corr:thread9": true}
	v, _ := Evaluate(context.Background(), lists, classify.Normalized{FromAddr: "nitin@firm.in", FromDomain: "firm.in", FromLocal: "nitin"}, "thread9", nil)
	if v.Rule != "thread_consistency" || v.Category != llm.Correspondence {
		t.Errorf("thread consistency: %+v", v)
	}
	v, _ = Evaluate(context.Background(), lists, classify.Normalized{FromAddr: "nitin@firm.in", FromDomain: "firm.in", FromLocal: "nitin"}, "thread10", nil)
	if v.Matched {
		t.Errorf("other thread should not match: %+v", v)
	}
}
