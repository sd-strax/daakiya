package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Anthropic calls the Claude API with structured output and a cached static prefix.
type Anthropic struct {
	client anthropic.Client
	model  string
	effort anthropic.OutputConfigEffort
}

// NewAnthropic builds a classifier. effort is "low" for the cheap pipeline
// model and typically "medium" for a judge model.
func NewAnthropic(apiKey, model, effort string) *Anthropic {
	opts := []option.RequestOption{option.WithMaxRetries(2), option.WithRequestTimeout(120 * time.Second)}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	if effort == "" {
		effort = "low"
	}
	return &Anthropic{client: anthropic.NewClient(opts...), model: model, effort: anthropic.OutputConfigEffort(effort)}
}

func (a *Anthropic) Name() string  { return "anthropic" }
func (a *Anthropic) Model() string { return a.model }

func (a *Anthropic) Classify(ctx context.Context, sys string, examples []Example, in Input) (Result, Usage, error) {
	system := []anthropic.TextBlockParam{{Text: sys}}
	if ex := RenderExamples(examples); ex != "" {
		system = append(system, anthropic.TextBlockParam{Text: ex})
	}
	// Cache the whole static prefix (system prompt + examples); only the email varies.
	system[len(system)-1].CacheControl = anthropic.NewCacheControlEphemeralParam()

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: 4096,
		System:    system,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(RenderInput(in))),
		},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: a.effort,
			Format: anthropic.JSONOutputFormatParam{Schema: Schema},
		},
	}
	// Haiku thinks by default and the thinking counts against max_tokens; a label
	// needs none of it. Disabling is only accepted at effort high or below, which
	// low satisfies. Other model families reject the disabled setting, so leave
	// them on adaptive thinking.
	if strings.Contains(a.model, "haiku") && a.effort == anthropic.OutputConfigEffortLow {
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfDisabled: &anthropic.ThinkingConfigDisabledParam{}}
	}
	resp, err := a.client.Messages.New(ctx, params)
	if err != nil {
		return Result{}, Usage{}, err
	}
	u := Usage{InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens, CacheRead: resp.Usage.CacheReadInputTokens}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return Result{}, u, fmt.Errorf("%w: model refused (%s)", ErrInvalidOutput, resp.StopDetails.Category)
	}
	if resp.StopReason == anthropic.StopReasonMaxTokens {
		return Result{}, u, fmt.Errorf("%w: output truncated", ErrInvalidOutput)
	}
	var text strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	if text.Len() == 0 {
		return Result{}, u, errors.New("empty response")
	}
	r, err := ParseResult(text.String())
	return r, u, err
}
