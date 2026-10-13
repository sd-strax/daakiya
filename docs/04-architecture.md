# 4. Architecture

## Component view

```mermaid
flowchart LR
    subgraph mac[Owner's Mac]
        launchd[launchd<br/>StartInterval 300s]
        cli[daakiya CLI<br/>single binary]
        store[(SQLite<br/>decisions, sync state,<br/>lists, examples)]
        kc[(macOS Keychain<br/>API key, MSAL token cache)]
        cfg[config.toml]
        launchd -->|daakiya run| cli
        cli --> store
        cli --> kc
        cli --> cfg
    end
    graph[Microsoft Graph<br/>/me/mailFolders/inbox/messages/delta<br/>/me/messages/{id}/move]
    llm[LLM provider<br/>Anthropic API or<br/>OpenAI-compatible endpoint<br/>e.g. Ollama on localhost]
    outlook[Outlook for Mac]
    cli <-->|HTTPS, delegated OAuth| graph
    cli -->|HTTPS or localhost| llm
    graph <-->|sync| outlook
```

## Packages (logical)

| Package | Responsibility | Depends on |
|---------|----------------|------------|
| `cmd/daakiya` | CLI entry points: `init`, `auth`, `run`, `install`, `uninstall`, `status`, `review`, `undo`, `allow`, `deny`, `promote`, `backfill`, `eval`, `export-examples` | everything below |
| `internal/mail` | `Mailbox` interface and the Graph implementation: delta sync, fetch, move, categorize, folder and category bootstrap, sent-items scan | `auth`, `store` |
| `internal/auth` | MSAL public-client flow (device code, loopback browser), token cache persisted to Keychain | `secrets` |
| `internal/classify` | Pipeline orchestration: normalize, rules, LLM, decide. Owns the taxonomy and the prompt templates (versioned). | `llm`, `store` |
| `internal/llm` | `Classifier` interface and providers: `anthropic`, `openaicompat`. Structured JSON output with local schema validation. | - |
| `internal/rules` | Allow/deny lists, known-correspondent set, header heuristics | `store` |
| `internal/store` | SQLite schema and queries; migrations | - |
| `internal/secrets` | Keychain read/write | - |
| `internal/actions` | Translate a decision into mailbox operations; idempotency guard; undo | `mail`, `store` |
| `internal/feedback` | Detect owner corrections by comparing recorded state with mailbox state; update lists and examples | `mail`, `store` |
| `internal/launchd` | Write and load the LaunchAgent plist | - |

Interfaces that must stay stable so adapters can be added later (N9):

```go
type Mailbox interface {
    Bootstrap(ctx) error                       // ensure folder + category exist
    NewMessages(ctx, cursor) ([]Message, cursor, error)   // delta since cursor
    Move(ctx, id, folderID) error
    SetCategories(ctx, id, []string) error
    Locate(ctx, stableID) (folderID, error)   // where is this message now? (feedback)
    KnownCorrespondents(ctx, since) ([]string, error)   // from Sent Items
}

type Classifier interface {
    Classify(ctx, Input) (Result, Usage, error)
}
```

## Runtime model

- **Scheduling.** A macOS LaunchAgent with `StartInterval` runs `daakiya run` every 5
  minutes. Each run is a short-lived process: load config, acquire token silently, process,
  exit. No daemon, no socket, nothing to supervise. launchd handles reboot and wake. A
  lock file prevents overlapping runs if one is slow. See [ADR 0008](adr/0008-launchd-polling-with-graph-delta.md).
- **One run, step by step.**
  1. Acquire a Graph access token from the MSAL cache (refresh token in Keychain). If
     interactive sign-in is needed, log and exit non-zero; `status` surfaces it.
  2. Call the Inbox delta endpoint with the stored `deltaLink`. On first run there is no
     link, so fetch only the delta token (process nothing) and store it. Backfill is a
     separate explicit command.
  3. Filter to messages created since the cursor that are not already in `decisions`.
  4. For each message, run the [pipeline](05-classification-pipeline.md). LLM calls are
     made with bounded concurrency (default 4) and a per-run and per-day budget.
  5. Apply actions (live mode) or record only (shadow mode).
  6. Run the feedback scan: for decisions in the last 30 days where an action was taken,
     check where the message is now; record corrections.
  7. Refresh the known-correspondent set from Sent Items if it is older than 24 hours.
  8. Persist the new `deltaLink`, write a summary line to the log.

## Microsoft Graph integration

| Concern | Choice |
|---------|--------|
| Auth flow | Public client (no secret), delegated permissions, MSAL. Default to device code flow because it works in a terminal and under launchd-initiated re-auth prompts; offer browser loopback as an option. Authority defaults to `https://login.microsoftonline.com/common` so both work and personal accounts work at release; the owner's own install pins their work tenant ID, which avoids the account-picker and makes consent errors specific. |
| Tenant posture | The owner's mailbox is in a work tenant they do not administer. Both app registration and `Mail.ReadWrite` consent may be restricted; spike 1 tests both before any code. If registration is blocked, IT creates the registration and the owner only needs the client ID. If consent is blocked, IT grants admin consent once via the admin-consent URL the setup guide will print. |
| Scopes | `Mail.ReadWrite` (read, move, set categories), `MailboxSettings.ReadWrite` (create the master category so it has a colour in Outlook), `User.Read`, `offline_access`. No application permissions, ever. |
| Change detection | `GET /me/mailFolders/inbox/messages/delta` with `$select` limited to the fields the pipeline needs. Store the `deltaLink` in SQLite. Delta returns moved-out messages as removals, which the feedback scan also uses. |
| Message identity | Request `Prefer: IdType="ImmutableId"` on every call so IDs survive moves between folders. Store `internetMessageId` as a secondary key for exports and cross-checks. See [ADR 0010](adr/0010-stable-message-identity.md). |
| Body | Request `body` with `Prefer: outlook.body-content-type="text"` so Graph does the HTML-to-text conversion; still run a local cleaner to strip quoted history and signatures. |
| Headers | `internetMessageHeaders` is only returned when explicitly selected and is not available on delta; fetch it per message for the LLM-bound subset only. Headers of interest: `List-Unsubscribe`, `Precedence`, `Auto-Submitted`, `X-Mailer`, `Return-Path`, `In-Reply-To`, `References`. |
| Actions | `POST /me/messages/{id}/move` with `destinationId`; `PATCH /me/messages/{id}` with `categories`. Folder created once under the mailbox root as `Daakiya` with child `Cold Outreach`; IDs cached. |
| Throttling | Honour `Retry-After`; back off and stop the run on repeated 429s. The volume here (tens of requests per run) is far below Graph mailbox limits. |
| Consent risk | Work tenants using Microsoft's default consent policy may block users from granting `Mail.ReadWrite` themselves; admin consent would then be required. This is the first spike in the [roadmap](08-roadmap.md). Personal Outlook.com accounts consent for themselves. |

## LLM provider integration

- `anthropic`: official Anthropic SDK for the chosen language; default model
  `claude-haiku-5-5`; structured output via the API's JSON schema support; `max_tokens`
  small (classification is a few dozen tokens); low effort setting. Prompt caching on the
  static system prompt and examples so per-message cost is mostly the email itself.
- `openaicompat`: plain HTTPS client against `{base_url}/v1/chat/completions`, JSON mode
  when the server advertises it, otherwise instruct-and-parse. Covers Ollama
  (`http://localhost:11434/v1`), LM Studio, OpenAI, OpenRouter, Groq, and others. Model name
  is free text.
- Both return the same `Result` struct and the same `Usage` struct (input tokens, output
  tokens, estimated cost from a small per-model price table the owner can override).
- Output is validated locally against the schema regardless of provider. Invalid or
  missing output is treated as `unknown` with confidence 0, never as a positive.

## Local state

```
~/Library/Application Support/daakiya/
  decisions.sqlite       # decisions, sync_state, lists, examples, corrections
  daakiya.log            # human-readable, rotated by size
~/.config/daakiya/config.toml   # non-secret config
Keychain: service "daakiya", accounts "llm-api-key", "msal-token-cache"
~/Library/LaunchAgents/dev.daakiya.run.plist
```

SQLite schema (first cut):

```sql
decisions(id TEXT PK, internet_message_id TEXT, conversation_id TEXT, received_at TEXT,
          from_addr TEXT, from_domain TEXT, subject TEXT,
          category TEXT, confidence REAL, reason TEXT, stage TEXT,   -- rule | llm
          provider TEXT, model TEXT, prompt_version TEXT,
          input_tokens INT, output_tokens INT, cost_usd REAL, latency_ms INT,
          action TEXT, action_at TEXT, mode TEXT,                     -- shadow | live
          corrected TEXT, corrected_at TEXT);                         -- fp | fn | NULL
sync_state(key TEXT PK, value TEXT);          -- delta_link, folder ids, category id
lists(kind TEXT, value TEXT, source TEXT, added_at TEXT, PRIMARY KEY(kind, value));
                                              -- allow_domain, allow_addr, deny_domain, deny_addr, correspondent
examples(id INTEGER PK, category TEXT, from_domain TEXT, subject TEXT, excerpt TEXT,
         source TEXT, added_at TEXT);         -- seed | correction
```

## Failure handling

| Failure | Behaviour |
|---------|-----------|
| Token expired, refresh fails | Exit non-zero, write a clear log line, `status` says "sign-in required: run `daakiya auth`". No actions taken. |
| Graph 5xx or network down | Retry with backoff within the run; give up and leave the cursor unchanged so the next run retries the same messages. |
| LLM provider down or over budget | Message recorded as `deferred`; retried on the next run up to 3 times, then recorded as `unknown`. Never moved. |
| Malformed LLM output | Recorded as `unknown`, reason "invalid output", counted in `status`. |
| Folder deleted by owner | Bootstrap recreates it on the next run and logs. |
| Two runs overlap | Lock file; second run exits immediately. |

## Language and runtime

Proposed: Go, producing a single static binary for macOS (arm64 and amd64), distributed
via a Homebrew tap. Official SDKs exist for Anthropic (`anthropic-sdk-go`) and Microsoft
identity (`microsoft-authentication-library-for-go`); Graph is called over plain REST,
which is simpler than the generated Graph SDK for the handful of endpoints used.
This is the one decision most worth confirming with the owner before coding; see
[ADR 0009](adr/0009-language-and-runtime.md) and [Open questions](09-open-questions.md).
