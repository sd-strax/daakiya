# ADR 0001: Access the mailbox through Microsoft Graph

Status: accepted. Date: 2026-10-13.

## Context

You read mail in Outlook for Mac against a Microsoft mailbox. The tool needs to see
new messages and move or tag them. Candidate access paths:

1. Script Outlook for Mac itself (AppleScript / JXA).
2. Read and write Outlook's local message database.
3. IMAP with OAuth against Exchange Online or Outlook.com.
4. Microsoft Graph REST API with delegated permissions.
5. An Outlook add-in (Office.js).

Facts checked 2026-10-13:

- Microsoft cancelled roadmap item RM88537 (AppleScript support in the new Outlook for
  Mac): "decided not to proceed".
- Legacy Outlook for Mac, which did support AppleScript, stopped working with Exchange
  Online mailboxes in October 2026 because Exchange Web Services were retired.
- Office.js has no event for a message arriving in the Inbox, so an add-in cannot run
  background triage.
- Exchange Online and Outlook.com both require OAuth for IMAP, which means an Entra app
  registration anyway, and IMAP exposes no categories and a weaker change-detection story
  than Graph delta queries.

## Decision

Use Microsoft Graph with delegated permissions (`Mail.ReadWrite`, `MailboxSettings.ReadWrite`,
`User.Read`, `offline_access`) obtained through a public-client OAuth flow in your
own app registration. Treat Outlook for Mac purely as a viewer of the result.

## Alternatives rejected

- AppleScript: no longer exists for the supported client.
- Local database: undocumented schema, write risk of corrupting the mail store, breaks on
  every Outlook update, and does nothing until Outlook syncs anyway.
- IMAP: same auth burden as Graph with fewer features; kept as a future adapter for
  non-Microsoft mailboxes, not for this one.
- Add-in: cannot run in the background; noted as a possible feedback surface later.

## Consequences

- Works whether Outlook is running or not, and from any machine you choose.
- Requires an Entra app registration (see ADR 0002) and is exposed to tenant consent
  policy; spike 1 in the roadmap tests this before any code.
- The `Mailbox` interface is kept adapter-shaped so Gmail or IMAP can be added later.
