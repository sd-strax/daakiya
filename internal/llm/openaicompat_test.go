package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAICompat(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "```json\n{\"category\":\"cold_outreach\",\"confidence\":0.9,\"reason\":\"pitch\",\"relationship_evidence\":\"none\"}\n```"}}},
			"usage":   map[string]int{"prompt_tokens": 120, "completion_tokens": 30},
		})
	}))
	defer srv.Close()
	c := NewOpenAICompat(srv.URL+"/v1", "", "qwen3:4b", true)
	r, u, err := c.Classify(context.Background(), SystemPrompt("x"), SeedExamples, Input{FromAddr: "a@b.c", Subject: "s", Body: "b"})
	if err != nil || r.Category != ColdOutreach || u.InputTokens != 120 {
		t.Fatalf("classify: %+v %+v %v", r, u, err)
	}
	if got.Model != "qwen3:4b" || got.Temperature != 0 || got.ResponseFormat == nil || len(got.Messages) != 2 {
		t.Errorf("request: %+v", got)
	}
	if !strings.Contains(got.Messages[0].Content, "additionalProperties") {
		t.Error("schema not included in system prompt")
	}
}
