// Package graph implements mail.Mailbox against the Microsoft Graph REST API.
// Only the handful of endpoints Daakiya needs are called, over plain net/http.
package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sd-strax/daakiya/internal/mail"
)

const defaultBase = "https://graph.microsoft.com/v1.0"

// TokenSource supplies a bearer token per request.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

type Client struct {
	tokens TokenSource
	http   *http.Client
	base   string
}

func New(tokens TokenSource) *Client {
	return &Client{tokens: tokens, http: &http.Client{Timeout: 60 * time.Second}, base: defaultBase}
}

// NewWithBase is for tests against a fake server.
func NewWithBase(tokens TokenSource, base string) *Client {
	c := New(tokens)
	c.base = strings.TrimRight(base, "/")
	return c
}

type apiError struct {
	Status int
	Code   string
	Msg    string
}

func (e *apiError) Error() string { return fmt.Sprintf("graph %d %s: %s", e.Status, e.Code, e.Msg) }

// do performs one request with retries on 429/5xx. Every call asks for immutable IDs
// and text bodies.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	u := path
	if !strings.HasPrefix(path, "http") {
		u = c.base + path
	}
	backoff := 2 * time.Second
	for attempt := 0; attempt < 5; attempt++ {
		tok, err := c.tokens.Token(ctx)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Prefer", `IdType="ImmutableId", outlook.body-content-type="text"`)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if attempt < 4 {
				sleep(ctx, backoff)
				backoff *= 2
				continue
			}
			return err
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		switch {
		case resp.StatusCode == 429 || resp.StatusCode >= 500:
			wait := backoff
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, err := strconv.Atoi(ra); err == nil {
					wait = time.Duration(secs) * time.Second
				}
			}
			if attempt < 4 {
				sleep(ctx, wait)
				backoff *= 2
				continue
			}
			return &apiError{Status: resp.StatusCode, Code: "throttled", Msg: string(data)}
		case resp.StatusCode == 404:
			return mail.ErrNotFound
		case resp.StatusCode >= 400:
			var e struct {
				Error struct {
					Code, Message string
				} `json:"error"`
			}
			_ = json.Unmarshal(data, &e)
			return &apiError{Status: resp.StatusCode, Code: e.Error.Code, Msg: e.Error.Message}
		}
		if out != nil && len(data) > 0 {
			return json.Unmarshal(data, out)
		}
		return nil
	}
	return errors.New("graph: retries exhausted")
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// --- wire types ---

type emailAddress struct {
	EmailAddress struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddress"`
}

func (e emailAddress) addr() mail.Address {
	return mail.Address{Name: e.EmailAddress.Name, Address: strings.ToLower(e.EmailAddress.Address)}
}

func addrs(in []emailAddress) []mail.Address {
	out := make([]mail.Address, 0, len(in))
	for _, a := range in {
		out = append(out, a.addr())
	}
	return out
}

type wireMessage struct {
	ID                string         `json:"id"`
	InternetMessageID string         `json:"internetMessageId"`
	ConversationID    string         `json:"conversationId"`
	ReceivedDateTime  time.Time      `json:"receivedDateTime"`
	From              *emailAddress  `json:"from"`
	Sender            *emailAddress  `json:"sender"`
	ReplyTo           []emailAddress `json:"replyTo"`
	ToRecipients      []emailAddress `json:"toRecipients"`
	CcRecipients      []emailAddress `json:"ccRecipients"`
	Subject           string         `json:"subject"`
	BodyPreview       string         `json:"bodyPreview"`
	HasAttachments    bool           `json:"hasAttachments"`
	IsDraft           bool           `json:"isDraft"`
	WebLink           string         `json:"webLink"`
	ParentFolderID    string         `json:"parentFolderId"`
	Categories        []string       `json:"categories"`
	Body              *struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
	InternetMessageHeaders []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"internetMessageHeaders"`
	Removed *struct {
		Reason string `json:"reason"`
	} `json:"@removed"`
}

func (w wireMessage) toMessage() mail.Message {
	m := mail.Message{
		ID:                w.ID,
		InternetMessageID: w.InternetMessageID,
		ConversationID:    w.ConversationID,
		ReceivedAt:        w.ReceivedDateTime,
		ReplyTo:           addrs(w.ReplyTo),
		To:                addrs(w.ToRecipients),
		CC:                addrs(w.CcRecipients),
		Subject:           w.Subject,
		BodyPreview:       w.BodyPreview,
		HasAttachments:    w.HasAttachments,
		IsDraft:           w.IsDraft,
		WebLink:           w.WebLink,
		ParentFolderID:    w.ParentFolderID,
	}
	if w.From != nil {
		m.From = w.From.addr()
	}
	if w.Sender != nil {
		m.Sender = w.Sender.addr()
	}
	if w.Body != nil {
		if strings.EqualFold(w.Body.ContentType, "html") {
			m.BodyHTML = w.Body.Content
		} else {
			m.BodyText = w.Body.Content
		}
	}
	if len(w.InternetMessageHeaders) > 0 {
		m.Headers = map[string][]string{}
		for _, h := range w.InternetMessageHeaders {
			k := strings.ToLower(h.Name)
			m.Headers[k] = append(m.Headers[k], h.Value)
		}
	}
	return m
}

type page struct {
	Value     []wireMessage `json:"value"`
	NextLink  string        `json:"@odata.nextLink"`
	DeltaLink string        `json:"@odata.deltaLink"`
}

const listSelect = "id,internetMessageId,conversationId,receivedDateTime,from,sender,replyTo,toRecipients,ccRecipients,subject,bodyPreview,hasAttachments,isDraft,webLink,parentFolderId"

// --- Mailbox implementation ---

func (c *Client) WhoAmI(ctx context.Context) (string, string, error) {
	var me struct {
		UserPrincipalName string `json:"userPrincipalName"`
		DisplayName       string `json:"displayName"`
	}
	if err := c.do(ctx, http.MethodGet, "/me?$select=userPrincipalName,displayName", nil, &me); err != nil {
		return "", "", err
	}
	return me.UserPrincipalName, me.DisplayName, nil
}

type folder struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

func (c *Client) findFolder(ctx context.Context, listPath, name string) (string, error) {
	var out struct {
		Value    []folder `json:"value"`
		NextLink string   `json:"@odata.nextLink"`
	}
	next := listPath + "?$top=100"
	for next != "" {
		if err := c.do(ctx, http.MethodGet, next, nil, &out); err != nil {
			return "", err
		}
		for _, f := range out.Value {
			if strings.EqualFold(f.DisplayName, name) {
				return f.ID, nil
			}
		}
		next = out.NextLink
		out.Value, out.NextLink = nil, ""
	}
	return "", nil
}

func (c *Client) ensureFolder(ctx context.Context, listPath, name string) (string, error) {
	id, err := c.findFolder(ctx, listPath, name)
	if err != nil || id != "" {
		return id, err
	}
	var created folder
	if err := c.do(ctx, http.MethodPost, listPath, map[string]string{"displayName": name}, &created); err != nil {
		return "", fmt.Errorf("create folder %q: %w", name, err)
	}
	return created.ID, nil
}

func (c *Client) Bootstrap(ctx context.Context, folderName string, subfolders []string, categories []string) (mail.Folders, error) {
	f := mail.Folders{Sub: map[string]string{}}
	var inbox folder
	if err := c.do(ctx, http.MethodGet, "/me/mailFolders/inbox?$select=id", nil, &inbox); err != nil {
		return f, err
	}
	f.InboxID = inbox.ID
	var err error
	if f.FolderID, err = c.ensureFolder(ctx, "/me/mailFolders", folderName); err != nil {
		return f, err
	}
	for _, name := range subfolders {
		id, err := c.ensureFolder(ctx, "/me/mailFolders/"+f.FolderID+"/childFolders", name)
		if err != nil {
			return f, err
		}
		f.Sub[name] = id
	}
	// Master categories give the tag a colour in Outlook. Needs MailboxSettings.ReadWrite.
	var cats struct {
		Value []struct {
			DisplayName string `json:"displayName"`
		} `json:"value"`
	}
	if err := c.do(ctx, http.MethodGet, "/me/outlook/masterCategories", nil, &cats); err != nil {
		return f, err
	}
	have := map[string]bool{}
	for _, cat := range cats.Value {
		have[strings.ToLower(cat.DisplayName)] = true
	}
	for i, name := range categories {
		if have[strings.ToLower(name)] {
			continue
		}
		body := map[string]string{"displayName": name, "color": fmt.Sprintf("preset%d", (i+4)%25)}
		if err := c.do(ctx, http.MethodPost, "/me/outlook/masterCategories", body, nil); err != nil {
			return f, fmt.Errorf("create category %q: %w", name, err)
		}
	}
	return f, nil
}

func (c *Client) InitialCursor(ctx context.Context) (string, error) {
	// $deltaToken=latest returns a delta link for "now" without listing messages.
	var p page
	if err := c.do(ctx, http.MethodGet, "/me/mailFolders/inbox/messages/delta?$deltaToken=latest", nil, &p); err == nil && p.DeltaLink != "" {
		return p.DeltaLink, nil
	}
	// Fallback: a delta restricted to the last minute returns few messages and a delta link.
	since := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	q := "/me/mailFolders/inbox/messages/delta?$select=id&$filter=" + url.QueryEscape("receivedDateTime ge "+since)
	d, err := c.NewMessages(ctx, q)
	if err != nil {
		return "", err
	}
	if d.Cursor == "" || d.Cursor == q {
		return "", errors.New("could not obtain a delta link")
	}
	return d.Cursor, nil
}

func (c *Client) NewMessages(ctx context.Context, cursor string) (mail.Delta, error) {
	var d mail.Delta
	next := cursor
	if next == "" {
		next = "/me/mailFolders/inbox/messages/delta?$select=" + url.QueryEscape(listSelect) + "&$top=50"
	}
	for next != "" {
		var p page
		if err := c.do(ctx, http.MethodGet, next, nil, &p); err != nil {
			return d, err
		}
		for _, w := range p.Value {
			if w.Removed != nil {
				d.Removed = append(d.Removed, w.ID)
				continue
			}
			d.New = append(d.New, w.toMessage())
		}
		if p.DeltaLink != "" {
			d.Cursor = p.DeltaLink
			break
		}
		next = p.NextLink
	}
	if d.Cursor == "" {
		d.Cursor = cursor
	}
	return d, nil
}

func (c *Client) Fetch(ctx context.Context, id string) (mail.Message, error) {
	var w wireMessage
	sel := listSelect + ",body,internetMessageHeaders"
	if err := c.do(ctx, http.MethodGet, "/me/messages/"+url.PathEscape(id)+"?$select="+url.QueryEscape(sel), nil, &w); err != nil {
		return mail.Message{}, err
	}
	return w.toMessage(), nil
}

func (c *Client) listMessages(ctx context.Context, first string, max int) ([]mail.Message, error) {
	var out []mail.Message
	next := first
	for next != "" && (max <= 0 || len(out) < max) {
		var p page
		if err := c.do(ctx, http.MethodGet, next, nil, &p); err != nil {
			return out, err
		}
		for _, w := range p.Value {
			out = append(out, w.toMessage())
		}
		next = p.NextLink
	}
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out, nil
}

func (c *Client) ListInboxSince(ctx context.Context, since time.Time, max int) ([]mail.Message, error) {
	q := url.Values{}
	q.Set("$select", listSelect)
	q.Set("$filter", "receivedDateTime ge "+since.UTC().Format(time.RFC3339))
	q.Set("$orderby", "receivedDateTime desc")
	q.Set("$top", "50")
	return c.listMessages(ctx, "/me/mailFolders/inbox/messages?"+q.Encode(), max)
}

func (c *Client) ListFolder(ctx context.Context, folderID string, max int) ([]mail.Message, error) {
	q := url.Values{}
	q.Set("$select", listSelect)
	q.Set("$orderby", "receivedDateTime desc")
	q.Set("$top", "50")
	return c.listMessages(ctx, "/me/mailFolders/"+url.PathEscape(folderID)+"/messages?"+q.Encode(), max)
}

func (c *Client) Move(ctx context.Context, id, dest string) (string, error) {
	var w wireMessage
	if err := c.do(ctx, http.MethodPost, "/me/messages/"+url.PathEscape(id)+"/move", map[string]string{"destinationId": dest}, &w); err != nil {
		return "", err
	}
	if w.ID == "" {
		return id, nil
	}
	return w.ID, nil
}

func (c *Client) SetCategories(ctx context.Context, id string, categories []string) error {
	if categories == nil {
		categories = []string{}
	}
	return c.do(ctx, http.MethodPatch, "/me/messages/"+url.PathEscape(id), map[string]any{"categories": categories}, nil)
}

func (c *Client) GetCategories(ctx context.Context, id string) ([]string, error) {
	var w wireMessage
	if err := c.do(ctx, http.MethodGet, "/me/messages/"+url.PathEscape(id)+"?$select=categories", nil, &w); err != nil {
		return nil, err
	}
	return w.Categories, nil
}

func (c *Client) Locate(ctx context.Context, id string) (string, error) {
	var w wireMessage
	if err := c.do(ctx, http.MethodGet, "/me/messages/"+url.PathEscape(id)+"?$select=parentFolderId", nil, &w); err != nil {
		return "", err
	}
	return w.ParentFolderID, nil
}

func (c *Client) KnownCorrespondents(ctx context.Context, since time.Time, maxMessages int) ([]string, []string, error) {
	q := url.Values{}
	q.Set("$select", "toRecipients,ccRecipients,conversationId,sentDateTime")
	q.Set("$filter", "sentDateTime ge "+since.UTC().Format(time.RFC3339))
	q.Set("$orderby", "sentDateTime desc")
	q.Set("$top", "100")
	msgs, err := c.listMessages(ctx, "/me/mailFolders/sentitems/messages?"+q.Encode(), maxMessages)
	if err != nil {
		return nil, nil, err
	}
	addrSet, convSet := map[string]bool{}, map[string]bool{}
	for _, m := range msgs {
		for _, a := range append(m.To, m.CC...) {
			if a.Address != "" {
				addrSet[a.Address] = true
			}
		}
		if m.ConversationID != "" {
			convSet[m.ConversationID] = true
		}
	}
	return keys(addrSet), keys(convSet), nil
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
