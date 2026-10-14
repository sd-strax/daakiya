# ADR 0002: Bring your own key and your own app registration

Status: accepted. Date: 2026-10-13.

## Context

The founding constraint is that the software performs no LLM inference on its own account
and bills nothing. Separately, Microsoft Graph access requires an app registration. A
publisher that wants other people to consent to its multi-tenant app without friction must
complete Microsoft publisher verification and, in many tenants, still needs admin consent
for mail write permissions under the Microsoft-managed default consent policies (which
have been excluding more Exchange permissions from user self-consent since late 2025).

Shipping a shared client ID would also mean the project owns an identity that every
user's token is issued against, a support burden and a trust question the project does not
want.

## Decision

1. **LLM:** you configure a provider endpoint, model, and key (or a local server
   with no key). The tool never contacts any LLM endpoint it was not configured with and
   ships with no key.
2. **Identity:** you register your own Entra public-client app and paste the
   client ID into `init`. The setup guide walks through it for work and personal accounts.
   The tool ships with no client ID.
3. **No hosted component of any kind:** no relay, no update server, no telemetry.

## Alternatives rejected

- Shared client ID with publisher verification: possible later if the project grows and
  the author wants to take it on; it does not change the architecture, only `init`.
- A hosted classification service: contradicts the founding constraint outright.

## Consequences

- Setup has one more step than a consumer product. This is the price of the trust model
  and is called out plainly in the README.
- Every user's data path is theirs alone; the project has nothing to leak and nothing to
  be subpoenaed for.
- Tenant consent policy is yours to solve with your admin, and the docs say
  so honestly.
