# ADR 0009: Go, single static binary

Status: accepted (confirmed 2026-10-13). Date: 2026-10-13.

## Context

The tool is a CLI plus a scheduled job, to be run by the author first and then given to
others on macOS. The recipients are not necessarily developers. Three realistic choices:
Go, TypeScript on Node, and Python.

| Concern | Go | TypeScript | Python |
|---------|----|------------|--------|
| Distribution to non-developers | Single static binary, Homebrew tap, notarizable | Needs Node or a bundler such as `pkg`/bun compile | Needs an interpreter or PyInstaller, fragile on macOS |
| Anthropic SDK | Official (`anthropic-sdk-go`) | Official | Official |
| Microsoft auth | Official MSAL for Go | Official MSAL Node | Official MSAL Python |
| Graph client | Plain REST is enough; generated SDK exists but is heavy | Same | Same |
| SQLite, Keychain, launchd | Mature libraries | Mature | Mature |
| Speed of first prototype | Medium | Fast | Fastest |
| Long-term maintenance by one person | Strong typing, few dependencies, boring | Good | Good, more runtime surprises on other machines |

## Decision

Go. The decisive factor is phase 3: "install one binary, run `init`" is the experience
that makes the giveaway work, and Go gives it with the least machinery.

## Alternatives

TypeScript is the runner-up and would be chosen if the author were markedly more productive
in it; `bun build --compile` narrows the distribution gap. Python is best kept for the
spike 2 labelling and comparison scripts, where speed of iteration matters and distribution
does not.

## Consequences

- Slightly slower first prototype than a script, offset by the spikes being done in
  whatever is fastest.
- Interfaces (`Mailbox`, `Classifier`) are idiomatic Go interfaces; adapters are packages.
- Cross-compilation for macOS arm64 and amd64 from CI is trivial.
