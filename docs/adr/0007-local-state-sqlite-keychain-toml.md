# ADR 0007: SQLite for state, Keychain for secrets, TOML for config

Status: accepted. Date: 2026-10-13.

## Context

The tool needs durable local state (decisions, delta cursor, lists, examples), secrets (an
API key and an OAuth refresh token), and human-editable configuration. It runs as a
short-lived process many times a day with no server.

## Decision

- **SQLite** file in `~/Library/Application Support/daakiya/` for all structured state.
  Single file, transactional, queryable with standard tools, no service to run.
- **macOS Keychain** for the LLM key and the serialized MSAL token cache, under service
  `daakiya`. Environment-variable override for the key for people who manage secrets
  differently.
- **TOML** at `~/.config/daakiya/config.toml` for everything non-secret: provider, model,
  base URL, thresholds, actions, mode, caps, privacy flags, your profile text.

## Alternatives rejected

- JSON files for state: no transactions, awkward queries, easy to corrupt on crash.
- Secrets in the config file with 0600 permissions: common, but Keychain is the platform
  norm and `init` can do it transparently.
- YAML config: TOML is simpler to hand-edit and has no indentation traps.

## Consequences

- macOS-specific secret storage; other hosts need an adapter (phase 4).
- `status` and `review` are thin queries over SQLite; you can run your own.
- Backups are one directory plus one Keychain export; documented in the guide.
