# ADR 0005: Reversible actions only

Status: accepted. Date: 2026-10-13.

## Context

The cost asymmetry is severe: a missed cold email costs seconds; a lost real email can
cost a customer or a job. The tool runs unattended, so every action must be one the owner
can discover and reverse later without help.

## Decision

The only mailbox mutations the tool performs are: move a message to a folder the tool
created, set or clear an Outlook category on a message, and create that folder and
category. It never deletes, never empties a folder, never marks junk or phishing (which
trains tenant-wide filters and can block legitimate senders), never marks read or unread,
never unsubscribes, never sends or drafts mail.

`undo` reverses a move in one command and records the correction. `uninstall` offers to
move everything in the Daakiya folder back to the Inbox.

## Alternatives rejected

- Delete after N days: convenient, irreversible, rejected for v1. If ever added it gets its
  own ADR and is off by default.
- Report as junk: irreversible side effects beyond the owner's mailbox.
- Mark as read: hides the folder's unread count, which is the owner's main audit cue.

## Consequences

- The Daakiya folder grows until the owner clears it; that is acceptable and visible.
- Any feature that needs a non-reversible action needs a new ADR first.
