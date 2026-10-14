# Architecture decision records

One file per decision. Format: context, decision, alternatives, consequences, status.
Statuses: `proposed` (awaiting confirmation), `accepted`, `superseded by NNNN`. All eleven are accepted as of 2026-10-13.

| ADR | Title | Status |
|-----|-------|--------|
| [0001](0001-mailbox-access-via-microsoft-graph.md) | Access the mailbox through Microsoft Graph, not Outlook automation or IMAP | accepted |
| [0002](0002-byok-and-byo-app-registration.md) | Bring your own key and your own app registration; no hosted component | accepted |
| [0003](0003-llm-provider-abstraction.md) | Two LLM adapters: Anthropic native and OpenAI-compatible | accepted |
| [0004](0004-rules-then-llm-with-constrained-output.md) | Deterministic rules first; the LLM returns a label, code chooses the action | accepted |
| [0005](0005-reversible-actions-only.md) | Reversible actions only: move and tag, never delete or report | accepted |
| [0006](0006-shadow-mode-first.md) | Shadow mode by default; promotion gated on measured precision | accepted |
| [0007](0007-local-state-sqlite-keychain-toml.md) | Local state in SQLite, secrets in Keychain, config in TOML | accepted |
| [0008](0008-launchd-polling-with-graph-delta.md) | launchd-scheduled one-shot runs polling Graph delta, not webhooks or a daemon | accepted |
| [0009](0009-language-and-runtime.md) | Go, single static binary | accepted |
| [0010](0010-stable-message-identity.md) | Immutable Graph IDs plus internetMessageId as message identity | accepted |
| [0011](0011-purpose-built-tool-not-general-agent.md) | A purpose-built pipeline rather than a general agent framework such as OpenClaw | accepted |
