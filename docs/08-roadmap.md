# 8. Roadmap

Phases are gated. Nothing in a later phase starts until the gate of the previous one is
met, because each gate answers a question that could change the design.

## Phase 0: Spikes (target: 2 to 3 days of effort)

Two cheap experiments that de-risk the two things the design cannot control.

### Spike 1: Can the owner's mailbox be reached through Graph?

The owner's mailbox is a work Microsoft 365 account, so this spike runs against an Entra
ID tenant the owner does not administer. There are two separate things that can be
locked down: whether ordinary users may register applications at all, and whether they
may consent to `Mail.ReadWrite`. Check the first before spending time on the second.

0. In the Entra portal, attempt to create an app registration. If the option is greyed
   out or returns an authorisation error, stop and record "registration requires IT".
1. Register a public-client app in the owner's Entra tenant. Redirect URI: `http://localhost` for loopback
   and the device-code default.
2. Request delegated `Mail.ReadWrite`, `MailboxSettings.ReadWrite`, `User.Read`,
   `offline_access`.
3. Sign in with the owner's account and attempt consent.
4. With a token, call the Inbox delta endpoint with `Prefer: IdType="ImmutableId"`,
   create a test folder, move one message into it and back, set a category.

Outcomes:

| Result | Consequence |
|--------|-------------|
| Consent succeeds | Proceed as designed. |
| Consent blocked, admin consent obtainable | Proceed; the setup guide gets an "ask your admin" path and an admin-consent URL. |
| Consent blocked, no admin path | Design change required. Candidates, in order: ask whether `Mail.ReadWrite` is allowed under the tenant's "popular mail clients" policy for a different app; fall back to `Mail.Read` plus shadow-only mode (classification without moves); or treat the work mailbox as out of reach and target personal accounts first. Decide before writing code. |

**Result, 2026-10-13: PROCEED.** The owner registered a single-tenant public-client app
(display name "Mail Sorter") in the work tenant without IT involvement. User consent for
`Mail.ReadWrite`, `MailboxSettings.ReadWrite`, `User.Read`, and `offline_access` was granted
on the standard consent screen with no admin-approval prompt. `scripts/spike1-consent.sh`
then verified against the live mailbox: inbox delta with a delta link, immutable IDs
surviving a move, folder create/delete, message move out and back, category set/clear on a
message, and master category create/delete. All ok. One portal gotcha for the setup guide:
in the new App registrations UI the "Allow public client flows" switch is under
"Authentication (Preview)", and until it is on, device-code sign-in fails with
AADSTS7000218 asking for a client secret. The client and tenant IDs live in the owner's
config, not in this repo.

### Spike 2: Does a local model work at all? (half a day)

Revised 2026-10-13. The original version of this spike asked the owner to hand-label 300
to 500 messages up front. That is unnecessary: shadow mode (phase 1) produces labelled
decisions as a by-product of the owner glancing at the review table, so the measured
comparison between Claude Haiku and a local model moves to the end of phase 1 where the
data already exists. What remains up front is a viability check, not a measurement.

1. Install Ollama; pull one 4B-class model and one 7B-to-9B-class model. Note RAM use and
   tokens per second on the owner's Mac.
2. Paste a dozen real emails from the owner's inbox (a few obvious cold pitches, a few
   obvious real ones, a couple of newsletters) into the v0 prompt and run each model once.
3. Check only three things: the model returns valid JSON matching the schema every time,
   its answers on the obvious cases are right, and latency per message is tolerable for a
   five-minute batch (under a few seconds each).

Outcome: a yes/no on whether `openaicompat` plus Ollama is worth wiring in phase 1
alongside `anthropic`, and a first feel for which model size to default to. No thresholds
are set here; they come from the shadow-period eval.

### Spike 3: OpenClaw prototype (dropped)

Dropped 2026-10-13: the owner does not run OpenClaw, so the calibration experiment has no
existing setup to piggyback on and is not worth a fresh install. The assessment in
[Alternatives considered](07-alternatives-considered.md) stands on the architectural
arguments alone.

**Gate to phase 1:** Spike 1 reached "proceed". Spike 2 is informative only and does not
gate anything.

## Phase 1: MVP for the owner, shadow mode

**Status 2026-10-13: implemented, not yet run against the live mailbox.** Everything in the
scope list below exists and has unit tests for the pure stages (normalise, rules, decide,
LLM parsing, store) and fake-server tests for the Graph and OpenAI-compatible adapters.
The `promote`/`demote`, `undo`, `allow`/`deny`, feedback scan and `backfill` pieces from
phase 2 were small enough to land at the same time. The first live exercise is the owner
running `daakiya init`.

Scope:

- Repository scaffold, lint, test, CI building macOS arm64 and amd64 binaries.
- `init` (provider, key to Keychain, client ID, device-code sign-in, folder and category
  bootstrap, owner profile text, seed allowlist with owner's domains).
- `run` with the full pipeline: delta sync, normalize, rules, LLM (both providers),
  decide, record. Shadow mode only; `shadow.tag` optional.
- `install`/`uninstall` for the LaunchAgent.
- `status`, `review` (with one-keystroke right/wrong marking, which is how labelled data
  is produced), `eval`.
- Known-correspondent build from Sent Items.
- Logging, lock file, budget caps.

Gate: two weeks of shadow running on the owner's mailbox; owner marks decisions in
`review`; measured precision on `cold_outreach` at or above 0.98 and recall at or above
0.80 over at least 200 reviewed decisions. At this point `eval --provider --model` replays
the reviewed decisions through the local model(s) from spike 2, and the owner picks the
default model and threshold from numbers.

## Phase 2: Live mode and the feedback loop

Scope:

- `promote` and `demote` between shadow and live.
- Actions applied: move and tag.
- Feedback scan (FP/FN detection from mailbox state), `undo`, `allow`, `deny`.
- Examples table feeding few-shot selection; denylist promotion rule.
- `backfill --since`.
- Per-provider thresholds in config; `eval --provider --model` comparison.

Gate: four weeks live on the owner's mailbox with no uncorrected false positive the owner
considers serious, and the owner no longer manually triaging cold outreach.

## Phase 3: Release

Scope:

- Setup guide, including a screenshot-free, step-by-step Entra app registration
  walkthrough for both work and personal accounts, and the admin-consent path.
- Homebrew tap, signed and notarized binaries, checksums.
- `uninstall --revoke` instructions for tokens and Keychain cleanup.
- A second tester on a different mailbox and different provider follows the guide without
  help; their eval clears the same gate.
- Licence (MIT or Apache-2.0; owner's call), README, CONTRIBUTING, issue templates.

Gate: the second tester succeeds; the owner is happy to put their name on it.

## Phase 4: Candidates, in no committed order

- `marketing` category with its own folder, off by default.
- IMAP adapter (Gmail, Fastmail, iCloud) behind the `Mailbox` interface; Gmail would more
  naturally use its own API, which is a separate adapter.
- Small MCP server exposing `review`, `undo`, `allow`, `deny` so agents (OpenClaw, Claude,
  others) can drive the tool without touching the mailbox pipeline.
- Menu bar app wrapping the CLI: status, last decisions, one-click undo.
- Outlook add-in button that calls the local tool to mark the open message as cold
  outreach or not (feedback only).
- Linux and Windows hosts (secret storage and scheduling adapters).
- Weekly digest of what was filed, as a local notification or a mail to self (would be the
  first outbound action and needs its own ADR).

## Non-goals, restated

No hosted service, no bundled model access, no deletion, no outbound mail in v1, no
training. If a feature needs one of these, it needs a new ADR first.
