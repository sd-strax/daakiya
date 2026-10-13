package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAICompat talks to any /v1/chat/completions endpoint: Ollama, LM Studio,
// OpenAI, OpenRouter, Groq and others.
type OpenAICompat struct {
	baseURL  string
	apiKey   string
	model    string
	jsonMode bool
	http     *http.Client
}

func NewOpenAICompat(baseURL, apiKey, model string, jsonMode bool) *OpenAICompat {
	return &OpenAICompat{
		baseURL:  strings.TrimRight(baseURL, "/"),
		apiKey:   apiKey,
		model:    model,
		jsonMode: jsonMode,
		http:     &http.Client{Timeout: 120 * time.Second},
	}
}

func (o *OpenAICompat) Name() string  { return "openai-compatible" }
func (o *OpenAICompat) Model() string { return o.model }

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Temperature    float64       `json:"temperature"`
	MaxTokens      int           `json:"max_tokens"`
	ResponseFormat any           `json:"response_format,omitempty"`
	Stream         bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (o *OpenAICompat) Classify(ctx context.Context, sys string, examples []Example, in Input) (Result, Usage, error) {
	system := sys
	if ex := RenderExamples(examples); ex != "" {
		system += "\n\n" + ex
	}
	schemaJSON, _ := json.Marshal(Schema)
	system += "\n\nThe JSON schema of your output is:\n" + string(schemaJSON)

	req := chatRequest{
		Model:       o.model,
		Messages:    []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: RenderInput(in)}},
		Temperature: 0,
		MaxTokens:   400,
	}
	if o.jsonMode {
		req.ResponseFormat = map[string]string{"type": "json_object"}
	}
	body, _ := json.Marshal(req)
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Result{}, Usage{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		hreq.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.http.Do(hreq)
	if err != nil {
		return Result{}, Usage{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return Result{}, Usage{}, fmt.Errorf("%s: HTTP %d: %s", o.baseURL, resp.StatusCode, truncate(string(data), 300))
	}
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return Result{}, Usage{}, fmt.Errorf("decode response: %w", err)
	}
	if cr.Error != nil {
		return Result{}, Usage{}, fmt.Errorf("%s: %s", o.baseURL, cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return Result{}, Usage{}, fmt.Errorf("%w: no choices", ErrInvalidOutput)
	}
	u := Usage{InputTokens: cr.Usage.PromptTokens, OutputTokens: cr.Usage.CompletionTokens}
	content := cr.Choices[0].Message.Content
	content = strings.TrimPrefix(strings.TrimSpace(content), "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(strings.TrimSpace(content), "```")
	r, err := ParseResult(content)
	return r, u, err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
