# Daakiya

*Daakiya* (डाकिया, "postman") sorts cold outreach and marketing out of a Microsoft Outlook
inbox using a language model **you** supply: an Anthropic API key, or a local model through
Ollama or any OpenAI-compatible server. It runs on your Mac as a background job, talks only
to Microsoft Graph and the model endpoint you configure, and never deletes anything.

Status: live on the author's mailbox since 2026-10-13. The first full-inbox cleanup
classified 10,046 messages for $0.59 of Claude Haiku and moved 6,769 of them. Design notes
and decision records are in [`docs/`](docs/README.md).

## How it works

Every five minutes a launchd job runs `daakiya run`. It asks Microsoft Graph for mail that
arrived since the last run and settles most of it with deterministic rules: people you
have written to, replies in your own threads, your allow and deny lists, your sender
routes, and automated senders. Only the ambiguous remainder goes to your model, as a
trimmed excerpt with the quoted history and signatures stripped. The model returns one of
four labels (cold outreach, marketing, notification, correspondence) with a confidence.
Code, not the model, maps the label to an action: move to a folder you have configured for
that label, or leave it alone.

It starts in shadow mode, where it records what it would do and tags the message without
moving it. You grade a few hundred decisions, check precision with `eval`, and `promote` to
live when the numbers satisfy you. Anything you drag back to the inbox afterwards is
recorded as a correction and shapes future decisions.

## Quick start

Prerequisites: macOS, Go 1.26 or later to build, a Microsoft 365 or Outlook.com mailbox,
and an Entra app registration (next section).

```
make install                  # builds bin/daakiya and installs it to ~/.local/bin
daakiya init                  # provider, key, client ID, device-code sign-in, folders, profile
daakiya install               # launchd job, every 5 minutes, in shadow mode
daakiya backfill --since 2026-09-01   # classify existing mail to get data quickly
daakiya review --mark         # grade decisions: one keystroke per row
daakiya eval                  # precision and recall from your grades, per threshold
daakiya promote               # leave shadow mode once you trust it
daakiya apply-pending         # move what shadow mode only tagged
daakiya status
```

A full cleanup of an old inbox is `daakiya backfill --since 2000-01-01 --max 20000
--budget 20000 --apply`. It streams the inbox page by page, oldest decisions first, and
prints progress. Budget it at roughly one model call per 1.5 messages and well under a
cent each on Haiku.

Other commands: `undo <id>`, `mark <id> <label>`, `allow <sender>`, `deny <sender>`,
`classify <id>`, `review --judge` to grade a sample with a stronger model, `demote`,
`refresh-correspondents`, `uninstall --revoke`. Run `daakiya` with no arguments for the
full list.

## Registering the Entra app

Daakiya ships with no Microsoft app identity. You register one in your own tenant, once.
No client secret is involved, and only delegated permissions on your own mailbox are
requested.

1. In the Entra admin centre, go to App registrations and create a new registration.
   Name it anything (the author's is "Mail Sorter"). Single tenant is fine. Add
   `http://localhost` as a "Mobile and desktop applications" redirect URI.
2. Under API permissions add delegated Microsoft Graph permissions `Mail.ReadWrite`,
   `MailboxSettings.ReadWrite`, `User.Read` and `offline_access`.
3. Under Authentication turn on "Allow public client flows". In the newer portal this
   switch lives under "Authentication (Preview)". Without it, sign-in fails with
   AADSTS7000218 asking for a client secret.
4. Copy the Application (client) ID and Directory (tenant) ID into `daakiya init`.

On a work tenant, ordinary users can usually do all of this without an administrator. If
registration is greyed out or consent is blocked, `scripts/spike1-consent.sh <client_id>
<tenant_id>` reproduces the sign-in and tells you which of the two is the problem.

## Configuration

Config lives in `~/.config/daakiya/config.toml`. Secrets (API key, token cache key) live
in the macOS Keychain. Decisions, logs and the encrypted token cache live in
`~/Library/Application Support/daakiya/`.

The parts worth knowing about:

```toml
mode = "shadow"                # or "live"; promote/demote switch it

[llm]
provider = "anthropic"         # or "openai-compatible" for Ollama and friends
model = "claude-haiku-5-5"
threshold = 0.85               # minimum confidence before anything is moved
familiar_domain_threshold = 0.95   # stricter floor for strangers at domains you have written to

[folders]                      # which labels get moved, and where (under the Daakiya folder)
cold_outreach = "Cold Outreach"
marketing = "Marketing"        # a label with no folder is never moved

[[routes]]                     # your own sender rules; applied before the model, in every mode
match = "linkedin.com"         # an address, or a domain (subdomains included)
folder = "LinkedIn"
```

## Bring your own key, and nothing else

The binary has no model access and no Microsoft identity of its own. You register the app
and configure the endpoint. Message excerpts go only to the model endpoint you chose.
Nothing is sent anywhere else and there is no telemetry. See
[security and privacy](docs/06-security-and-privacy.md) for what is stored locally and
how prompt injection is handled.

## Building

```
make test
make build
make install
```

`make install` replaces the binary by rename rather than overwriting it in place. On Apple
Silicon, overwriting a running or recently run Mach-O invalidates its signature and the
kernel kills the next launch.

The Makefile sets `GOWORK=off` because the author's parent directory has a `go.work` that
does not list this module. From a clean checkout `go build ./cmd/daakiya` works as usual.

## Licence

[MIT](LICENSE).
