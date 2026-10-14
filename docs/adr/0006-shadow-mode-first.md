# ADR 0006: Shadow mode by default, promotion gated on measurement

Status: accepted. Date: 2026-10-13.

## Context

Trust in an automated filer is earned by watching it be right. Small models and new
prompts should not be allowed to move mail until their precision on your actual
inbox is known.

## Decision

After `init` the tool is in shadow mode: it runs the full pipeline, records every decision,
and optionally tags would-be-moved messages with an Outlook category so you can audit
from inside Outlook, but it moves nothing. `promote` switches to live mode; `demote`
switches back. The documented gate for the author's own promotion is precision at or above
0.98 and recall at or above 0.80 on `cold_outreach` over at least 200 reviewed decisions,
measured with `eval` and `review`.

## Consequences

- The first two weeks produce labelled data for free, which also seeds few-shot examples.
- Changing provider, model, or prompt version prompts a recommendation (not a forced
  switch) to return to shadow mode for a period.
- `status` always shows the current mode so unattended operation is never ambiguous.
