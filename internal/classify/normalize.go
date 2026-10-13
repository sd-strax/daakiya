// Package classify runs the pipeline: normalize, rules, LLM, decide.
package classify

import (
	"regexp"
	"strings"

	"github.com/sd-strax/daakiya/internal/llm"
	"github.com/sd-strax/daakiya/internal/mail"
)

// Normalized is the cleaned, compact view of a message that both the rules and
// the LLM input are derived from.
type Normalized struct {
	FromName   string
	FromAddr   string
	FromDomain string // registrable domain, e.g. example.com for mail.example.com
	FromLocal  string
	ReplyTo    string
	Subject    string
	Body       string // cleaned and truncated
	Signals    llm.Signals
}

var (
	quoteStart = regexp.MustCompile(`(?mi)^(On .{0,200}wrote:|-----\s*Original Message\s*-----|From: .+\n(Sent|Date): .+|_{10,}|Begin forwarded message:)`)
	sigStart   = regexp.MustCompile(`(?mi)^(-- ?$|Sent from my |Get Outlook for |Thanks,?$|Best( regards)?,?$|Regards,?$|Cheers,?$|Kind regards,?$)`)
	urlRe      = regexp.MustCompile(`https?://[^\s<>"')\]]+`)
	calendarRe = regexp.MustCompile(`(?i)(calendly\.com|hubspot\.com/meetings|meetings\.hubspot|cal\.com/|savvycal\.com|chilipiper\.com|outlook\.office\.com/bookwithme|zcal\.co|tidycal\.com)`)
	emailRe    = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	phoneRe    = regexp.MustCompile(`\+?\d[\d\s().-]{7,}\d`)
	wsRe       = regexp.MustCompile(`[ \t]+`)
	blankRe    = regexp.MustCompile(`\n{3,}`)
	tagRe      = regexp.MustCompile(`(?s)<[^>]*>`)
	styleRe    = regexp.MustCompile(`(?is)<(style|script|head)[^>]*>.*?</(style|script|head)>`)
	pixelRe    = regexp.MustCompile(`(?i)<img[^>]+(width|height)\s*=\s*["']?1["']?`)
)

// Options control normalisation.
type Options struct {
	MaxBodyChars int
	Redact       bool
	OwnerAddrs   map[string]bool // owner's own addresses, lowercased
}

func Normalize(m mail.Message, opt Options) Normalized {
	n := Normalized{
		FromName: m.From.Name,
		FromAddr: strings.ToLower(m.From.Address),
		Subject:  strings.TrimSpace(m.Subject),
	}
	if n.FromAddr == "" && m.Sender.Address != "" {
		n.FromAddr = strings.ToLower(m.Sender.Address)
	}
	if i := strings.Index(n.FromAddr, "@"); i > 0 {
		n.FromLocal = n.FromAddr[:i]
		n.FromDomain = RegistrableDomain(n.FromAddr[i+1:])
	}
	if len(m.ReplyTo) > 0 {
		n.ReplyTo = strings.ToLower(m.ReplyTo[0].Address)
	}

	body := m.BodyText
	html := m.BodyHTML
	if body == "" && html != "" {
		body = htmlToText(html)
	}
	if body == "" {
		body = m.BodyPreview
	}
	n.Signals.Links = len(urlRe.FindAllString(body, -1))
	n.Signals.CalendarLink = calendarRe.MatchString(body) || calendarRe.MatchString(html)
	n.Body = CleanBody(body, opt.MaxBodyChars, opt.Redact)

	h := m.Headers
	n.Signals.ListUnsubscribe = has(h, "list-unsubscribe")
	prec := strings.ToLower(first(h, "precedence"))
	n.Signals.Bulk = prec == "bulk" || prec == "list" || prec == "junk" || has(h, "list-id")
	n.Signals.AutoSubmitted = has(h, "auto-submitted") && !strings.EqualFold(first(h, "auto-submitted"), "no")
	n.Signals.InReplyTo = has(h, "in-reply-to") || has(h, "references")
	n.Signals.Mailer = mailerName(first(h, "x-mailer"))

	for _, a := range m.To {
		if opt.OwnerAddrs[strings.ToLower(a.Address)] {
			n.Signals.ToOwnerDirectly = true
		}
	}
	if len(opt.OwnerAddrs) == 0 && len(m.To) == 1 {
		n.Signals.ToOwnerDirectly = true
	}
	return n
}

func has(h map[string][]string, k string) bool { return len(h[k]) > 0 }
func first(h map[string][]string, k string) string {
	if v := h[k]; len(v) > 0 {
		return v[0]
	}
	return ""
}

func mailerName(x string) string {
	x = strings.ToLower(strings.TrimSpace(x))
	if x == "" {
		return ""
	}
	if i := strings.IndexAny(x, " /("); i > 0 {
		x = x[:i]
	}
	if len(x) > 30 {
		x = x[:30]
	}
	return x
}

// CleanBody drops quoted history and signatures, collapses whitespace,
// optionally redacts, and truncates keeping the start.
func CleanBody(body string, max int, redact bool) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if loc := quoteStart.FindStringIndex(body); loc != nil && loc[0] > 0 {
		body = body[:loc[0]]
	}
	// Drop ">"-quoted lines.
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), ">") {
			continue
		}
		kept = append(kept, line)
	}
	body = strings.Join(kept, "\n")
	// Only cut at a signature marker if it leaves a meaningful amount of text.
	if loc := sigStart.FindStringIndex(body); loc != nil && loc[0] > 80 {
		body = body[:loc[0]]
	}
	if redact {
		body = emailRe.ReplaceAllString(body, "[email]")
		body = phoneRe.ReplaceAllString(body, "[phone]")
		body = urlRe.ReplaceAllString(body, "[url]")
	}
	body = wsRe.ReplaceAllString(body, " ")
	body = blankRe.ReplaceAllString(body, "\n\n")
	body = strings.TrimSpace(body)
	if max > 0 && len(body) > max {
		cut := body[:max]
		if i := strings.LastIndexAny(cut, " \n"); i > max/2 {
			cut = cut[:i]
		}
		body = cut + " ..."
	}
	return body
}

func htmlToText(h string) string {
	h = styleRe.ReplaceAllString(h, " ")
	h = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</tr>|</li>|</h[1-6]>`).ReplaceAllString(h, "\n")
	h = tagRe.ReplaceAllString(h, " ")
	h = strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&zwnj;", "", "&zwj;", "").Replace(h)
	return h
}

// HasTrackingPixel is a cheap hint computed from the HTML body.
func HasTrackingPixel(html string) bool { return pixelRe.MatchString(html) }

var secondLevel = map[string]bool{
	"co.uk": true, "org.uk": true, "ac.uk": true, "gov.uk": true, "com.au": true, "net.au": true, "org.au": true,
	"co.in": true, "co.jp": true, "co.nz": true, "com.br": true, "com.sg": true, "com.mx": true, "co.za": true,
	"com.hk": true, "com.tw": true, "co.kr": true, "com.cn": true, "com.ar": true, "co.il": true,
}

// RegistrableDomain reduces a hostname to its registrable part with a small
// second-level list. Good enough for allow/deny lists; not a public suffix parser.
func RegistrableDomain(host string) string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	parts := strings.Split(host, ".")
	if len(parts) <= 2 {
		return host
	}
	last2 := strings.Join(parts[len(parts)-2:], ".")
	if secondLevel[last2] && len(parts) >= 3 {
		return strings.Join(parts[len(parts)-3:], ".")
	}
	return last2
}

// ToInput converts a normalized message into the LLM input.
func ToInput(n Normalized, known bool) llm.Input {
	return llm.Input{
		FromName: n.FromName, FromAddr: n.FromAddr, FromDomain: n.FromDomain, KnownToOwner: known,
		ReplyTo: n.ReplyTo, Subject: n.Subject, Signals: n.Signals, Body: n.Body,
	}
}
