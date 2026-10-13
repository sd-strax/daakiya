# 2. Requirements

## Functional (v1)

| ID | Requirement |
|----|-------------|
| F1 | Detect new messages arriving in the Inbox of one Microsoft mailbox (Microsoft 365 work/school or Outlook.com personal) within a configurable interval (default 5 minutes). |
| F2 | Classify each new message into one of a fixed set of categories (see [pipeline](05-classification-pipeline.md)), with a confidence score and a one-line reason. |
| F3 | Use deterministic rules first (allowlists, known correspondents, in-thread replies, denylists) so that the LLM only sees genuinely ambiguous mail. |
| F4 | Call an owner-configured LLM provider for the ambiguous remainder. Supported in v1: Anthropic API (Claude Haiku class by default) and any OpenAI-compatible endpoint (covers Ollama, LM Studio, OpenAI, OpenRouter, Groq and others). |
| F5 | Apply a configurable, reversible action per category. v1 default: `cold_outreach` is moved to a folder named `Daakiya/Cold Outreach` and tagged with an Outlook category. All other categories are left untouched. |
| F6 | Support a **shadow mode** in which decisions are recorded (and optionally shown as an Outlook category) but nothing is moved. Shadow mode is the default after install. |
| F7 | Record every decision locally: message identity, category, confidence, reason, which stage decided (rule or LLM), provider, model, prompt version, token usage, latency, action taken. |
| F8 | Provide a `review` view of recent decisions with one-keystroke right/wrong marking (this is the only labelling the owner ever does), and an `undo` that moves a message back to the Inbox and records the correction. |
| F9 | Learn from corrections. A message the owner moves back out of the Daakiya folder is a false positive; a message the owner moves into it is a false negative. Both feed the allowlist/denylist and the few-shot examples. |
| F10 | Run unattended on macOS across reboots and sleep, with no terminal open. |
| F11 | Set up with a single guided `init` command: choose provider, store key, register/enter the Entra app client ID, sign in, create the folder and category. |
| F12 | Provide an `eval` command that scores the current rules and prompt against the reviewed decisions and prints precision/recall per category and per threshold, with provider/model overrides for comparison. |
| F13 | Provide a `backfill --since <date>` command to classify existing Inbox mail on demand. Never done automatically. |

## Non-functional

| ID | Requirement |
|----|-------------|
| N1 | **BYOK.** No LLM key, proxy, relay, or account is bundled. The binary makes LLM calls only to the endpoint the owner configured. |
| N2 | **No telemetry.** Nothing is sent anywhere except the owner's mailbox provider and the owner's LLM provider. |
| N3 | **Minimal data to the LLM.** Send sender, subject, a short header summary, and a bounded, HTML-stripped excerpt of the body (default 1,500 characters). Never attachments. Optional redaction of email addresses and phone numbers in the body. |
| N4 | **Secrets in the macOS Keychain.** API keys and OAuth refresh tokens are never written to plain files. |
| N5 | **Reversible by construction.** The tool never deletes, never empties folders, never reports junk/phishing, never sends mail, never unsubscribes. |
| N6 | **Idempotent.** Re-running over the same messages produces no duplicate actions. |
| N7 | **Cost-bounded.** A per-day cap on LLM calls (default 500) and a per-message input cap. Exceeding the cap logs and skips rather than failing the run. |
| N8 | **Cheap to install for others.** One binary, one config file, one Keychain entry, one launchd job. Homebrew tap at release. |
| N9 | **Portable core.** Mailbox access and LLM access sit behind interfaces so a Gmail/IMAP adapter or another provider can be added without touching the pipeline. |
| N10 | **Observable locally.** Human-readable log, SQLite store queryable with standard tools, `status` command showing last run, counts, errors, and estimated spend. |

## Explicitly out of scope for v1

- Replying, drafting, unsubscribing, or any outbound action.
- A GUI or menu bar app (candidate for a later phase).
- Classifying mail outside the Inbox (other folders, shared mailboxes, delegated mailboxes).
- Real-time push (Graph change notifications require a public endpoint; polling is enough at 5 minutes).
- Non-Microsoft mailboxes (the interface allows it; the adapter is not in v1).
- Windows or Linux hosts. The pipeline is portable, but the Keychain and launchd pieces are macOS-only in v1.
- Fine-tuning or training any model.

## Quality gates

| Gate | Threshold | Measured how |
|------|-----------|--------------|
| Leave shadow mode | Precision on `cold_outreach` at or above 0.98 and recall at or above 0.80 over at least 200 decisions reviewed by the owner | `daakiya eval` against the owner's labelled set plus the shadow-period review |
| Release to others | Same thresholds reproduced by at least one other person on a different mailbox, plus the setup guide followed end to end by someone who did not write it | Manual |

Recall is deliberately the softer target. A missed cold email costs a few seconds of
attention; a misfiled real email can cost a relationship.
