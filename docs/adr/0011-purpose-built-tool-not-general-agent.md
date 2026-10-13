# ADR 0011: A purpose-built pipeline rather than a general agent framework

Status: accepted. Date: 2026-10-13.

## Context

The owner asked whether OpenClaw, a self-hosted agent gateway with a scheduler, skills, and
tool access, could do this more easily. It could do it quickly: a skill, a Graph
connection, and a heartbeat would produce a working triage agent without new code. The
full comparison is in [Alternatives considered](../07-alternatives-considered.md).

## Decision

Build a narrow, deterministic pipeline (ADR 0004) as a standalone CLI, and do not build on
OpenClaw or any general agent framework. Expose the CLI, and later a small MCP server, so
such frameworks can drive the tool from the outside if the owner wants a chat interface.

## Reasons

1. **Cost shape.** An agent heartbeat is a full reasoning loop with tool calls per check;
   this task is one cheap classification per message and should stay that way, which is
   also what makes a local 4B model viable.
2. **Safety shape.** Email is attacker-written input. A model that reads it and then picks
   tools is the prompt-injection shape; a model that returns an enum and code that acts on
   it is not.
3. **Measurability.** Fixed schema, thresholds, prompt versions, and an `eval` command give
   numbers; a skill description gives behaviour.
4. **Giveaway.** "One binary and your key" reaches far more people than "run a gateway,
   then add this skill", and does not hand recipients a general agent when they wanted a
   filter.

## Consequences

- Faster first prototype is forgone in exchange for a design that can be trusted
  unattended and shipped.
- Spike 3 in the roadmap leaves room to use OpenClaw as a calibration experiment.
- Interoperability is a stated goal, so the CLI surface is designed to be scriptable from
  day one (stable exit codes, `--json` output on `review` and `status`).
