# 3. Solution overview

Daakiya is a command-line tool and background job that runs on the owner's Mac. Every few
minutes it asks Microsoft Graph for messages that arrived in the Inbox since the last run,
decides for each whether it is cold outreach, and moves the ones that are into a dedicated
folder. Outlook for Mac, which syncs from the same mailbox, shows the result without being
involved in producing it.

```
   Outlook for Mac  <----sync----  Microsoft 365 / Outlook.com mailbox
                                             ^
                                             | Microsoft Graph (delegated, owner's own consent)
                                             v
                                   +--------------------+
   launchd (every 5 min) -------->  |  daakiya run        |
                                   |                    |
                                   |  1. fetch delta     |
                                   |  2. rules           |
                                   |  3. LLM (if needed) |---> owner's LLM provider
                                   |  4. decide + act    |     (Anthropic API, Ollama,
                                   |  5. record          |      any OpenAI-compatible)
                                   +--------------------+
                                             |
                                             v
                               ~/Library/Application Support/daakiya/
                                   decisions.sqlite, log
                               macOS Keychain: API key, OAuth tokens
```

## The five ideas that matter

1. **Go around Outlook, not through it.** Outlook for Mac cannot be scripted any more
   (Microsoft cancelled AppleScript support for the new client, and the legacy client lost
   Exchange Online access in October 2026). Microsoft Graph gives the same mailbox with a
   supported, documented API, and works whether Outlook is open or not.
   See [ADR 0001](adr/0001-mailbox-access-via-microsoft-graph.md).

2. **Rules first, model second.** Most mail can be settled without a model: replies in a
   thread the owner started, mail from people the owner has written to, allowlisted
   domains, obviously automated notifications. The LLM sees only the remainder, which keeps
   cost, latency, and exposure low. See [ADR 0004](adr/0004-rules-then-llm-with-constrained-output.md).

3. **The model classifies; the code acts.** The LLM returns a category from a fixed list
   plus a confidence and a reason. It has no tools and chooses no actions. Email is
   untrusted input, and an agent that can be talked into moving or deleting mail by the
   contents of an email is the wrong design for this job.
   See [ADR 0004](adr/0004-rules-then-llm-with-constrained-output.md) and [ADR 0011](adr/0011-purpose-built-tool-not-general-agent.md).

4. **Reversible only, shadow first.** Move and tag, never delete or report junk. Start in
   shadow mode, measure, then promote. Every decision is recorded and can be undone with
   one command, and undoing teaches the tool.
   See [ADR 0005](adr/0005-reversible-actions-only.md) and [ADR 0006](adr/0006-shadow-mode-first.md).

5. **Bring your own key, and your own app registration.** The tool holds no credentials of
   its own. The owner supplies an LLM endpoint and key (or a local model), and an Entra
   app client ID they registered themselves. Nothing phones home.
   See [ADR 0002](adr/0002-byok-and-byo-app-registration.md).

## What the owner experiences

```
$ brew install sdas/tap/daakiya          # at release; before that, a built binary
$ daakiya init
  Provider?            [anthropic | openai-compatible]
  Model?               claude-haiku-5-5
  API key:             ********  (stored in Keychain)
  Entra app client ID: 1a2b...   (guided registration, one time)
  Sign in:             open https://microsoft.com/devicelogin and enter ABCD-EFGH
  Creating folder "Daakiya/Cold Outreach" and category "Daakiya"... done
  Mode: shadow (decisions recorded, nothing moved). Run `daakiya promote` when ready.
$ daakiya install                        # launchd job, every 5 minutes
$ daakiya status
  last run 2026-10-13T09:40:12+01:00, 14 new, 3 rules, 11 llm, 6 would-move, $0.0011 today
$ daakiya review                         # table of recent decisions, with reasons
$ daakiya undo 7f3c                      # move back, mark as false positive
$ daakiya eval                           # precision/recall against labelled set
$ daakiya promote                        # leave shadow mode
```

## What it is not

Not a mail client, not an agent that reads and answers mail, not a spam filter
replacement, not a hosted service. It does one narrow thing well enough to be trusted to
run unattended.
