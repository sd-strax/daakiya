package graph

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type tok struct{}

func (tok) Token(context.Context) (string, error) { return "t", nil }

func TestDeltaMoveAndRetry(t *testing.T) {
	var calls int
	var gotPrefer, gotAuth string
	var moveBody map[string]string
	mux := http.NewServeMux()
	mux.HandleFunc("/me/mailFolders/inbox/messages/delta", func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotPrefer, gotAuth = r.Header.Get("Prefer"), r.Header.Get("Authorization")
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		if r.URL.Query().Get("page") == "2" {
			json.NewEncoder(w).Encode(map[string]any{
				"value":            []any{map[string]any{"id": "m2", "@removed": map[string]string{"reason": "deleted"}}},
				"@odata.deltaLink": "http://x/delta?token=final",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"value": []any{map[string]any{
				"id": "m1", "subject": "hello", "conversationId": "c1", "receivedDateTime": "2026-10-13T10:00:00Z",
				"from":         map[string]any{"emailAddress": map[string]string{"name": "Joe", "address": "JOE@X.IO"}},
				"toRecipients": []any{map[string]any{"emailAddress": map[string]string{"address": "me@y.com"}}},
			}},
			"@odata.nextLink": "http://" + r.Host + "/me/mailFolders/inbox/messages/delta?page=2",
		})
	})
	mux.HandleFunc("/me/messages/m1/move", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&moveBody)
		json.NewEncoder(w).Encode(map[string]string{"id": "m1"})
	})
	mux.HandleFunc("/me/messages/gone", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":{"code":"ErrorItemNotFound","message":"nope"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewWithBase(tok{}, srv.URL)
	d, err := c.NewMessages(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.New) != 1 || d.New[0].From.Address != "joe@x.io" || d.New[0].Subject != "hello" {
		t.Errorf("new messages: %+v", d.New)
	}
	if len(d.Removed) != 1 || d.Removed[0] != "m2" {
		t.Errorf("removed: %v", d.Removed)
	}
	if d.Cursor != "http://x/delta?token=final" {
		t.Errorf("cursor: %q", d.Cursor)
	}
	if !strings.Contains(gotPrefer, `IdType="ImmutableId"`) || gotAuth != "Bearer t" {
		t.Errorf("headers: prefer=%q auth=%q", gotPrefer, gotAuth)
	}
	if calls < 3 {
		t.Errorf("expected retry after 429, calls=%d", calls)
	}
	id, err := c.Move(context.Background(), "m1", "folder9")
	if err != nil || id != "m1" || moveBody["destinationId"] != "folder9" {
		t.Errorf("move: id=%q err=%v body=%v", id, err, moveBody)
	}
	if _, err := c.Locate(context.Background(), "gone"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("404 should map to ErrNotFound, got %v", err)
	}
}
