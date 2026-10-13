# Daakiya

*Daakiya* (डाकिया, "postman") sorts cold outreach out of a Microsoft Outlook inbox using a
language model **you** supply: an Anthropic API key, or a local model through Ollama or any
OpenAI-compatible server. It runs on your Mac, talks only to Microsoft Graph and the model
endpoint you configure, and never deletes anything.

Status: phase 1 (shadow mode) implemented, being tested on the author's mailbox. Design and
decisions are in [`docs/`](docs/README.md).

## How it works

Every five minutes a launchd job runs `daakiya run`. It asks Microsoft Graph for mail that
arrived since the last run, settles most of it with deterministic rules (people you have
written to, replies in your threads, allow and deny lists, automated senders), sends only
the ambiguous remainder to your model as a trimmed excerpt, and gets back a label. Code,
not the model, maps the label to the only action allowed: move to `Daakiya/Cold Outreach`
and tag. It starts in shadow mode, where it records what it would do and tags the message
without moving it, until you promote it.

## Quick start

Prerequisites: macOS, Go 1.26 or later to build, a Microsoft 365 or Outlook.com mailbox,
and an Entra app registration (steps in [`docs/08-roadmap.md`](docs/08-roadmap.md), spike 1).

```
make install                  # builds bin/daakiya and copies it to ~/.local/bin
daakiya init                  # provider, key, client ID, device-code sign-in, folders, profile
daakiya install               # launchd job, every 5 minutes
daakiya status
daakiya review --mark         # grade recent decisions: one keystroke per row
daakiya eval                  # precision and recall from your grades, per threshold
daakiya promote               # leave shadow mode once you trust it
```

Other commands: `undo <id>`, `allow <sender>`, `deny <sender>`, `backfill --since DATE`,
`eval --provider openai-compatible --model qwen3:4b --base-url http://localhost:11434/v1`
to compare a local model on the same reviewed decisions, `demote`, `uninstall --revoke`.

Config lives in `~/.config/daakiya/config.toml`. Secrets live in the macOS Keychain. State
and logs live in `~/Library/Application Support/daakiya/`.

Two things worth knowing about in the config:

```toml
[folders]                      # which categories get moved, and where (under the Daakiya folder)
cold_outreach = "Cold Outreach"
marketing = "Marketing"        # off unless you add it

[[routes]]                     # your own sender rules; applied before the model, in every mode
match = "linkedin.com"         # an address, or a domain (subdomains included)
folder = "LinkedIn"
```

## Bring your own key

The binary ships with no model access and no Microsoft app identity. You register a
public-client app in your own tenant (no secret, delegated mail permissions only) and
configure a model endpoint. Nothing is sent anywhere else, and there is no telemetry.

## Building

The parent directory on the author's machine has a `go.work` that does not list this
module, so the Makefile sets `GOWORK=off`. From a clean checkout `go build ./cmd/daakiya`
works as usual.

```
make test
make build
```

## Licence

MIT (to be added before release).
