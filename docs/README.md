# Daakiya design docs

*Daakiya* (डाकिया) is Hindi for "postman". The project is a small, self-hosted tool that
sorts cold outreach and marketing mail out of a Microsoft Outlook inbox using a language
model that the user supplies (bring your own key, or a local model). The software never
calls an LLM on its own account.

These documents were written before any code. They are the contract for what gets built
first, and the record of why.

## Reading order

| # | Document | What it answers |
|---|----------|-----------------|
| 1 | [Problem](01-problem.md) | What hurts, for whom, and why existing filters do not fix it |
| 2 | [Requirements](02-requirements.md) | What v1 must do, must not do, and what is out of scope |
| 3 | [Solution overview](03-solution-overview.md) | The shape of the answer in one page |
| 4 | [Architecture](04-architecture.md) | Components, data flow, Graph integration, runtime model |
| 5 | [Classification pipeline](05-classification-pipeline.md) | Rules, prompt, taxonomy, thresholds, feedback loop, evaluation |
| 6 | [Security and privacy](06-security-and-privacy.md) | What leaves the machine, where secrets live, prompt-injection posture |
| 7 | [Alternatives considered](07-alternatives-considered.md) | Outlook rules, SpamSieve, Power Automate, add-ins, SaaS, and OpenClaw |
| 8 | [Roadmap](08-roadmap.md) | Spikes, phases, and the gate between "for me" and "give it away" |
| 9 | [Open questions](09-open-questions.md) | Decisions still needed from the owner before coding |
| - | [ADRs](adr/README.md) | One file per architectural decision, with status and consequences |

## Status

Design reviewed with the owner on 2026-10-13 (see [Open questions](09-open-questions.md)).
Phases 0 and 1 are complete and phase 2 (live mode) has been running on the owner's mailbox
since 2026-10-13; the [roadmap](08-roadmap.md) carries the measured results. Facts that
depend on Microsoft or vendor behaviour were checked on 2026-10-13 and are cited where
they appear; re-verify before release.

## Conventions

- "Owner" means the person running Daakiya against their own mailbox.
- "Provider" means an LLM API or local inference server the owner configures.
- "Decision" means one classification result for one message, stored locally.
- Dates are absolute (ISO 8601). Relative phrases like "next month" are avoided.
