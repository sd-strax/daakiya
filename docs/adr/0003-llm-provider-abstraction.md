# ADR 0003: Two LLM adapters, Anthropic native and OpenAI-compatible

Status: accepted. Date: 2026-10-13.

## Context

BYOK means the tool must work with whatever model the owner has: a hosted API (the owner
named Claude Haiku as the obvious choice), or a self-hosted small model via Ollama or LM
Studio. Supporting every vendor SDK is not viable for a one-person project. Most local
servers and many hosted vendors expose an OpenAI-compatible chat completions endpoint.

## Decision

A `Classifier` interface with exactly two implementations in v1:

- `anthropic`: official Anthropic SDK, default model `claude-haiku-5-5`, structured output
  via the API's JSON schema support, prompt caching on the static prefix, low effort.
- `openaicompat`: standard-library HTTPS against `{base_url}/v1/chat/completions` with a
  free-text model name. JSON mode when advertised; otherwise instruct and parse. Default
  `base_url` presets for Ollama (`http://localhost:11434/v1`) and LM Studio.

Both normalise to the same `Result` and `Usage` types. Output is schema-validated locally
regardless of provider. A small per-model price table (overridable) turns token counts
into an estimated cost for `status`.

## Alternatives rejected

- OpenAI-compatible only, including for Anthropic: loses structured outputs and prompt
  caching, and depends on a compatibility shim rather than the vendor's own contract.
- A multi-vendor abstraction library: adds a large dependency for two call shapes.
- Native adapters for every vendor: unbounded maintenance.

## Consequences

- Owners of OpenAI, OpenRouter, Groq, Mistral, Gemini-via-compat, and local models are
  all covered by the second adapter without code changes.
- Confidence calibration differs across models; thresholds are per provider/model in
  config and chosen with `eval`.
- Adding a third native adapter later is a contained change behind the interface.
