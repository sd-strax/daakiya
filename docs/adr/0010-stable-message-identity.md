# ADR 0010: Immutable Graph IDs plus internetMessageId

Status: accepted. Date: 2026-10-13.

## Context

Graph's default message `id` changes when a message moves between folders. The feedback
loop depends on asking "where is the message I moved last week now?", and `undo` needs to
address the moved message. Using the default ID would break both.

## Decision

Send `Prefer: IdType="ImmutableId"` on every Graph request so message IDs are stable
across moves within the mailbox. Store the immutable ID as the decision primary key and
the RFC 5322 `internetMessageId` alongside it as a secondary key for exports,
deduplication across re-syncs, and any future non-Graph adapter.

## Consequences

- `Locate(id)` is a single `GET /me/messages/{id}?$select=parentFolderId`.
- If immutable IDs are ever unavailable (they are not supported for every mailbox type),
  the adapter falls back to `internetMessageId` search, slower but correct; `init` detects
  and records which mode applies.
