package llm

import (
	"errors"
	"strings"
	"testing"
)

func TestParseResult(t *testing.T) {
	good := `{"category":"cold_outreach","confidence":0.92,"reason":"pitch with calendar link","relationship_evidence":"none"}`
	r, err := ParseResult(good)
	if err != nil || r.Category != ColdOutreach || r.Confidence != 0.92 {
		t.Fatalf("good parse failed: %+v %v", r, err)
	}
	fenced := "Here you go:\n```json\n" + good + "\n```"
	if _, err := ParseResult(fenced); err != nil {
		t.Errorf("fenced JSON should parse: %v", err)
	}
	for name, bad := range map[string]string{
		"not json":       "cold_outreach",
		"bad category":   `{"category":"spam","confidence":0.9,"reason":"x","relationship_evidence":"none"}`,
		"bad confidence": `{"category":"cold_outreach","confidence":1.5,"reason":"x","relationship_evidence":"none"}`,
		"bad relation":   `{"category":"cold_outreach","confidence":0.5,"reason":"x","relationship_evidence":"maybe"}`,
	} {
		if _, err := ParseResult(bad); !errors.Is(err, ErrInvalidOutput) {
			t.Errorf("%s: expected ErrInvalidOutput, got %v", name, err)
		}
	}
	// Missing relationship defaults to none; control characters are stripped from reason.
	r, err = ParseResult(`{"category":"marketing","confidence":0.7,"reason":"news\u0007letter\n"}`)
	if err != nil || r.Relationship != RelNone || strings.ContainsAny(r.Reason, "\a\n") {
		t.Errorf("defaults/sanitize wrong: %+v %v", r, err)
	}
}

func TestRenderInputStable(t *testing.T) {
	in := Input{FromName: "Joe", FromAddr: "joe@x.io", FromDomain: "x.io", Subject: "Hi", Body: "body", Signals: Signals{Links: 2, CalendarLink: true}}
	s := RenderInput(in)
	for _, want := range []string{"From: Joe <joe@x.io>", "known to you: no", "Subject: Hi", "links=2", "calendar-link=yes", "\nbody"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %q", want, s)
		}
	}
	if strings.Contains(s, "Reply-To") {
		t.Error("empty reply-to should be omitted")
	}
}

func TestSystemPromptMentionsThreat(t *testing.T) {
	p := SystemPrompt("CTO at Example")
	for _, want := range []string{"CTO at Example", "untrusted", "cold_outreach", "relationship_evidence"} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}
