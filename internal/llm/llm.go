// Package llm defines the classifier abstraction and the two providers.
// The model returns a label; it never chooses actions.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Categories. Only ColdOutreach triggers an action.
const (
	ColdOutreach   = "cold_outreach"
	Marketing      = "marketing"
	Notification   = "notification"
	Correspondence = "correspondence"
	Unknown        = "unknown"
)

var Categories = []string{ColdOutreach, Marketing, Notification, Correspondence, Unknown}

// Relationship evidence values.
const (
	RelNone   = "none"
	RelWeak   = "weak"
	RelStrong = "strong"
)

// Input is the compact, provider-neutral record sent to the model.
type Input struct {
	FromName      string
	FromAddr      string
	FromDomain    string
	KnownToOwner  bool
	ReplyTo       string
	Subject       string
	Signals       Signals
	Body          string
}

// Signals are header- and content-derived booleans computed locally.
type Signals struct {
	ListUnsubscribe bool `json:"list_unsubscribe"`
	Bulk            bool `json:"bulk"`
	AutoSubmitted   bool `json:"auto_submitted"`
	InReplyTo       bool `json:"in_reply_to"`
	ToOwnerDirectly bool `json:"to_owner_directly"`
	Links           int  `json:"links"`
	CalendarLink    bool `json:"calendar_link"`
	Mailer          string `json:"mailer,omitempty"`
}

type Result struct {
	Category     string  `json:"category"`
	Confidence   float64 `json:"confidence"`
	Reason       string  `json:"reason"`
	Relationship string  `json:"relationship_evidence"`
}

type Usage struct {
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
}

type Example struct {
	Category   string
	FromDomain string
	Subject    string
	Excerpt    string
}

type Classifier interface {
	Name() string
	Model() string
	Classify(ctx context.Context, sys string, examples []Example, in Input) (Result, Usage, error)
}

var ErrInvalidOutput = errors.New("model returned invalid output")

// Schema is the JSON schema every provider is asked to honour and that every
// response is validated against locally.
var Schema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"category": map[string]any{
			"type": "string",
			"enum": Categories,
		},
		"confidence": map[string]any{
			"type":        "number",
			"description": "0 to 1, how sure you are of the category",
		},
		"reason": map[string]any{
			"type":        "string",
			"description": "one sentence, under 160 characters, citing the specific tell",
		},
		"relationship_evidence": map[string]any{
			"type": "string",
			"enum": []string{RelNone, RelWeak, RelStrong},
			"description": "evidence in the message that sender and recipient already know each other or have ongoing business",
		},
	},
	"required":             []string{"category", "confidence", "reason", "relationship_evidence"},
	"additionalProperties": false,
}

// ParseResult parses and validates a model response. Any failure yields
// ErrInvalidOutput so that callers record `unknown` and take no action.
func ParseResult(raw string) (Result, error) {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "{"); i > 0 {
		raw = raw[i:]
	}
	if j := strings.LastIndex(raw, "}"); j >= 0 && j < len(raw)-1 {
		raw = raw[:j+1]
	}
	var r Result
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidOutput, err)
	}
	ok := false
	for _, c := range Categories {
		if r.Category == c {
			ok = true
		}
	}
	if !ok {
		return Result{}, fmt.Errorf("%w: category %q", ErrInvalidOutput, r.Category)
	}
	if r.Confidence < 0 || r.Confidence > 1 {
		return Result{}, fmt.Errorf("%w: confidence %v", ErrInvalidOutput, r.Confidence)
	}
	switch r.Relationship {
	case RelNone, RelWeak, RelStrong:
	case "":
		r.Relationship = RelNone
	default:
		return Result{}, fmt.Errorf("%w: relationship_evidence %q", ErrInvalidOutput, r.Relationship)
	}
	r.Reason = sanitize(r.Reason, 200)
	return r, nil
}

// sanitize strips control characters and truncates; reasons are displayed, never interpreted.
func sanitize(s string, max int) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			r = ' '
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// RenderInput produces the compact text block the model sees.
func RenderInput(in Input) string {
	var b strings.Builder
	known := "no"
	if in.KnownToOwner {
		known = "yes"
	}
	fmt.Fprintf(&b, "From: %s <%s>   (domain: %s; known to you: %s)\n", in.FromName, in.FromAddr, in.FromDomain, known)
	if in.ReplyTo != "" && !strings.EqualFold(in.ReplyTo, in.FromAddr) {
		fmt.Fprintf(&b, "Reply-To: %s\n", in.ReplyTo)
	}
	fmt.Fprintf(&b, "Subject: %s\n", in.Subject)
	s := in.Signals
	fmt.Fprintf(&b, "Signals: list-unsubscribe=%s, bulk=%s, auto-submitted=%s, in-reply-to=%s, links=%d, calendar-link=%s, to-you-directly=%s",
		yn(s.ListUnsubscribe), yn(s.Bulk), yn(s.AutoSubmitted), yn(s.InReplyTo), s.Links, yn(s.CalendarLink), yn(s.ToOwnerDirectly))
	if s.Mailer != "" {
		fmt.Fprintf(&b, ", mailer=%s", s.Mailer)
	}
	b.WriteString("\nBody (quoted text and signature removed, truncated):\n")
	b.WriteString(in.Body)
	return b.String()
}

func yn(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// RenderExamples renders few-shot examples in the same shape as the input.
func RenderExamples(ex []Example) string {
	if len(ex) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Examples the owner has confirmed:\n")
	for i, e := range ex {
		fmt.Fprintf(&b, "\n--- example %d ---\nFrom domain: %s\nSubject: %s\nBody: %s\nCorrect category: %s\n", i+1, e.FromDomain, e.Subject, e.Excerpt, e.Category)
	}
	return b.String()
}
