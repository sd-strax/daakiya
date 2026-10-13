# ADR 0008: launchd-scheduled one-shot runs polling Graph delta

Status: accepted. Date: 2026-10-13.

## Context

The tool must run unattended across sleep and reboot. Options: a long-running daemon, a
launchd job that starts the tool on an interval, Graph change notifications (webhooks),
or Graph's delta query polled on a timer.

## Decision

A macOS LaunchAgent with `StartInterval` (default 300 seconds) runs `daakiya run`. Each run
is a short process that reads the stored `deltaLink`, fetches changes to the Inbox, does
its work, stores the new link, and exits. A lock file prevents overlap.

## Alternatives rejected

- Webhooks: require a public HTTPS endpoint and subscription renewal, which contradicts
  "nothing hosted" and adds nothing at a five-minute tolerance.
- Resident daemon: more to supervise, more to go wrong after sleep, no benefit at this
  cadence.
- Polling a full message list: wasteful and loses the moved-out signal that delta provides
  for free, which the feedback scan uses.

## Consequences

- Latency to action is at most the interval plus the run time; five minutes is acceptable
  and configurable.
- launchd handles restart, reboot, and wake semantics.
- If the Mac is asleep, mail is processed on wake; nothing is missed because delta is
  cursor-based.
