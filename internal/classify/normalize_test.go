package classify

import (
	"strings"
	"testing"

	"github.com/sd-strax/daakiya/internal/mail"
)

func TestRegistrableDomain(t *testing.T) {
	cases := map[string]string{
		"example.com":        "example.com",
		"mail.example.com":   "example.com",
		"a.b.example.co.uk":  "example.co.uk",
		"EXAMPLE.COM.":       "example.com",
		"localhost":          "localhost",
		"news.vendor.com.au": "vendor.com.au",
	}
	for in, want := range cases {
		if got := RegistrableDomain(in); got != want {
			t.Errorf("RegistrableDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanBodyDropsQuotesAndSignature(t *testing.T) {
	body := "Hi Sam,\n\nQuick question about your outbound strategy. We help teams book meetings.\nWould you be open to a 15 minute call?\n\nThanks,\nJoe\nSales Rep\n\nOn Mon, Oct 13, 2026 at 9:00 AM Sam Owner <owner@example.com> wrote:\n> old stuff\n> more old stuff\n"
	got := CleanBody(body, 1500, false)
	if strings.Contains(got, "old stuff") {
		t.Errorf("quoted text not removed: %q", got)
	}
	if strings.Contains(got, "Sales Rep") {
		t.Errorf("signature not removed: %q", got)
	}
	if !strings.Contains(got, "15 minute call") {
		t.Errorf("content lost: %q", got)
	}
}

func TestCleanBodyTruncatesAndRedacts(t *testing.T) {
	long := strings.Repeat("word ", 1000) + "contact me at joe@example.com or +1 (415) 555-0100 https://calendly.com/joe"
	got := CleanBody(long, 200, true)
	if len(got) > 210 {
		t.Errorf("not truncated: %d chars", len(got))
	}
	short := CleanBody("contact me at joe@example.com or call +1 (415) 555-0100, see https://calendly.com/joe", 0, true)
	for _, leak := range []string{"joe@example.com", "555-0100", "calendly.com"} {
		if strings.Contains(short, leak) {
			t.Errorf("redaction leaked %q in %q", leak, short)
		}
	}
}

func TestNormalizeSignals(t *testing.T) {
	m := mail.Message{
		From:     mail.Address{Name: "Joe", Address: "Joe@Mail.Growth.IO"},
		To:       []mail.Address{{Address: "owner@example.com"}},
		Subject:  " Quick question ",
		BodyHTML: `<html><head><style>p{}</style></head><body><p>Hi there,</p><p>Book time: <a href="https://calendly.com/joe/15min">here</a></p><img src="x" width="1" height="1"></body></html>`,
		Headers: map[string][]string{
			"list-unsubscribe": {"<mailto:u@x>"},
			"precedence":       {"bulk"},
			"x-mailer":         {"HubSpot Marketing/1.2 (Foo)"},
		},
	}
	n := Normalize(m, Options{MaxBodyChars: 500, OwnerAddrs: map[string]bool{"owner@example.com": true}})
	if n.FromAddr != "joe@mail.growth.io" || n.FromDomain != "growth.io" || n.FromLocal != "joe" {
		t.Errorf("from parsing wrong: %+v", n)
	}
	if n.Subject != "Quick question" {
		t.Errorf("subject not trimmed: %q", n.Subject)
	}
	if !n.Signals.CalendarLink || !n.Signals.ListUnsubscribe || !n.Signals.Bulk || !n.Signals.ToOwnerDirectly {
		t.Errorf("signals wrong: %+v", n.Signals)
	}
	if n.Signals.Mailer != "hubspot" {
		t.Errorf("mailer = %q", n.Signals.Mailer)
	}
	if strings.Contains(n.Body, "<") || !strings.Contains(n.Body, "Hi there") {
		t.Errorf("html not converted: %q", n.Body)
	}
	if !HasTrackingPixel(m.BodyHTML) {
		t.Error("tracking pixel not detected")
	}
}
