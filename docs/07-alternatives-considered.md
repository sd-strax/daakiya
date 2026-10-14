# 7. Alternatives considered

Each alternative is judged against the project's constraints: works with Outlook for Mac on a
Microsoft mailbox, bring your own key, buildable by one person, releasable to others,
reversible, and cheap.

## Summary

| Option | Can judge intent? | Runs unattended? | BYOK? | Releasable? | Verdict |
|--------|-------------------|------------------|-------|-------------|---------|
| Outlook rules / Sweep | No | Yes | n/a | n/a | Keep as a complement for fixed senders; cannot solve the core problem |
| Focused Inbox / Clutter | Partly, opaque | Yes | n/a | n/a | Already failing on the author's inbox |
| SpamSieve or similar Bayesian plug-ins | Partly | Yes | n/a | Third-party product | Requires AppleScript, which the new Outlook for Mac does not have |
| Power Automate flow calling an LLM | Yes | Yes | Awkward | Only to M365 users with premium licences | Wrong licensing and trust model for a giveaway |
| Outlook add-in (Office.js) | Yes | **No** | Yes | Yes, via AppSource or sideload | No event for incoming mail; possible later as a feedback UI |
| Mail-triage SaaS (SaneBox class) | Yes | Yes | No | Not ours | Pays someone else to read the mailbox |
| OpenClaw (self-hosted agent gateway) | Yes | Yes | Yes | Only to OpenClaw users | Good for a quick personal experiment; wrong foundation for the product. Details below |
| **Purpose-built local tool over Microsoft Graph** | Yes | Yes | Yes | Yes | **Chosen** |

## Outlook rules and Sweep

Rules match on sender, recipients, subject words, and a few headers. The problem here is
a category of mail with rotating senders and personalised subjects, so there is no pattern
to match. Rules remain useful for the deterministic part of the pipeline (for example,
routing `linkedin.com` notifications), and Daakiya's rule stage deliberately covers the
same ground so you do not need to maintain both.

## SpamSieve and Bayesian plug-ins

SpamSieve is excellent at spam and integrates with mail clients through AppleScript. The
new Outlook for Mac does not support AppleScript, and Microsoft has cancelled the roadmap
item that would have added it (RM88537, "decided not to proceed"). The legacy client, which
did support it, stopped working with Exchange Online in October 2026 when Exchange Web
Services were retired. This path is closed. Bayesian word statistics are also a weaker fit
than an LLM for intent, because cold outreach shares vocabulary with legitimate business
mail.

## Power Automate

A flow triggered "when a new email arrives" could post the message to an LLM and move it.
Problems: the HTTP action needed to call an arbitrary LLM is a premium connector; the
flow, and your LLM key, live in Microsoft's cloud rather than on your machine; personal Outlook.com accounts have a different connector; and giving this away
means giving away a flow template that each recipient must licence. It also cannot call a
local model on your Mac. Reasonable for an enterprise with a Power Platform licence,
wrong for this project.

## Outlook add-in (Office.js)

Add-ins run inside Outlook on every platform, can read the open message, and can move it.
But Office.js event-based activation only fires on compose-side and a few organiser
events; there is no event for a message arriving in the Inbox, so an add-in cannot triage
in the background. It could be a nice later surface for the feedback loop (a "Cold
outreach" button in the reading pane that calls the local tool), which is noted in the
roadmap as a maybe.

## Mail-triage SaaS

Products like SaneBox do roughly this, well, for a subscription, by holding IMAP or Graph
credentials to the mailbox on their servers. That is the opposite of this project's BYOK and
self-hosted constraints, and it is not something to give away.

## OpenClaw

The author asked whether OpenClaw could handle this more easily. Assessment, based on the
official docs (docs.openclaw.ai) and third-party guides checked 2026-10-13:

**What it is.** A self-hosted, MIT-licensed gateway that connects chat apps to AI agents,
with a persistent scheduler ("automations", including heartbeat monitors that run periodic
agent turns), on-demand "skills" that teach the agent procedures, and a tool layer. Several
third-party guides describe pointing it at a Microsoft 365 mailbox through Graph and
asking it to triage.

**Where it would be easier.**

- No code to write: a skill file plus a Graph connection and a heartbeat gets a first
  version in an afternoon.
- A chat interface for free: "what did you file today?" and "put that one back" are
  natural.
- Extensible to drafting replies, summarising, and other inbox tasks later.
- If you already run OpenClaw for other things, it is one more job in an existing
  system.

**Where it is a worse fit for this problem.**

- *Cost and latency model.* Heartbeats run full agent turns (the docs say so explicitly:
  shorter intervals burn more tokens). Every check is a multi-step reasoning loop with tool
  calls, not one cheap classification per message. The whole point here is that the task
  is trivial for a small model; an agent loop spends an order of magnitude more per message
  and makes a local 4B model impractical.
- *Safety model.* An agent with mailbox tools reads attacker-written emails and then
  decides what tools to call. That is the textbook prompt-injection shape. OpenClaw's
  permission layer helps, but the architecture still has the model choosing actions over
  untrusted text. Daakiya's design has the model choose a label and the code choose the
  action, which removes the class of failure rather than guarding it.
  See [Security](06-security-and-privacy.md).
- *Determinism and measurement.* A pipeline with rules, a fixed schema, thresholds, and an
  eval command can be measured and tuned. An agent following a skill description is
  harder to evaluate, and its behaviour can shift with the gateway's own model or prompt
  changes.
- *Giving it away.* The audience for "install this binary, paste your key" is much larger
  than the audience for "run an OpenClaw gateway, then add this skill". Recipients would
  also inherit a general agent with broad access when they wanted a filter.
- *Operational weight.* A gateway process, a chat channel, and the agent's own
  configuration, versus a launchd job.

**Verdict.** OpenClaw is a fine way to prototype the prompt and to confirm that an LLM
can do this at all, and it is a reasonable personal solution for someone who already runs
it. It is not the right foundation for a small, cheap, safe, giveaway tool. The two can
also coexist: Daakiya will expose its CLI and a small MCP server (roadmap, phase 4
candidate) so an OpenClaw agent, or any other agent, can call `daakiya review` or
`daakiya undo` without owning the mailbox pipeline.

This is recorded as [ADR 0011](adr/0011-purpose-built-tool-not-general-agent.md).

## Sources checked 2026-10-13

- Microsoft 365 roadmap item RM88537, "Outlook: Support for AppleScript in the New Outlook for Mac", status: decided not to proceed. https://mc.merill.net/message/RM88537
- Microsoft Support, "End of support for legacy Outlook for Mac" (legacy client stops working with Exchange Online in October 2026 due to EWS retirement). https://support.microsoft.com/outlook/end-of-support-for-legacy-outlook-for-mac
- Microsoft-managed default app consent policy changes (Exchange and mail permissions excluded from user consent, November 2025 and mid-2026 updates). https://blog-en.topedia.com/2025/11/microsoft-managed-default-app-consent-policy-now-blocks-20-additional-permissions/ and https://blog-en.topedia.com/2026/05/microsoft-managed-default-app-consent-policy-will-block-8-additional-permissions/
- OpenClaw documentation: overview, automation, heartbeat. https://docs.openclaw.ai/ , https://docs.openclaw.ai/automation , https://docs.openclaw.ai/gateway/heartbeat
- Third-party OpenClaw inbox-triage guides (vendor content, treated as illustrative only): https://clawdrop.org/playbooks/inbox-triage-agent/ , https://openclawlaunch.com/guides/ai-agent-outlook
- SpamSieve forum threads on new Outlook for Mac and AppleScript. https://forum.c-command.com/t/spamsieve-training-scripts-not-working-with-new-outlook-for-mac-16-4-4/13501
