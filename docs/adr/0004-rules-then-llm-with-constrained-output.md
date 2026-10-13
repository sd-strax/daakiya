# ADR 0004: Rules first; the LLM returns a label and code chooses the action

Status: accepted. Date: 2026-10-13.

## Context

Two forces: cost and exposure favour sending as little as possible to a model, and
security favours never letting a model that reads attacker-controlled email decide what to
do about it. Agent-style designs, where the model has `move_message` and similar tools,
are easy to build and are the dominant pattern in current tooling, including general
agent gateways.

## Decision

The pipeline is: normalize, deterministic rules (known correspondents, in-thread replies,
allow/deny lists, automated-mail headers), then an LLM call only for what remains. The LLM
is given no tools. It returns a JSON object with a category from a fixed enum, a
confidence, a one-sentence reason, and a relationship-evidence flag. Code validates the
object and maps category plus confidence to an action using thresholds from config.

## Alternatives rejected

- LLM on every message: several times the cost, more data leaving the machine, and it
  wastes the owner's own strongest signal (who they already correspond with).
- Agentic loop with mailbox tools: moves the action decision into the model, which is the
  component reading untrusted input. See ADR 0011.
- Rules only: cannot judge intent; this is the status quo that failed.

## Consequences

- A successful prompt injection can at most mislabel one message, which the review loop
  catches like any other error.
- Rule coverage is measurable (`eval` reports the rule/LLM split) and improves as the
  feedback loop grows the lists.
- Adding a category requires a prompt version bump and a code change to the enum, which
  is intentional friction.
