# 9. Open questions and decisions

Updated 2026-10-13 after review with the owner. Questions with a decision are kept here as
the record of who decided what; the few still open are marked.

| # | Question | Status | Decision / default applied |
|---|----------|--------|----------------------------|
| Q1 | Account type? | **Decided** | Work Microsoft 365 (Entra ID tenant). Authority for the owner's own install is pinned to that tenant; `common` remains the default for other users at release. |
| Q2 | Can the owner get admin consent for a self-registered app with `Mail.ReadWrite`? | **Decided by spike 1** | Not needed. The tenant allows user app registration and user consent for the mail scopes; spike 1 passed on 2026-10-13 with no admin involvement. The setup guide still needs the admin-consent path for other tenants. |
| Q3 | Language and runtime? | **Decided** | Go. ADR 0009 moved to accepted. Python allowed for throwaway spike scripts only. |
| Q4 | Which local models are available? | **Decided** | None installed yet. Spike 2 installs Ollama and checks that a 4B-class and a 7B-to-9B-class model return valid output on a dozen sample emails. The measured Haiku-versus-local comparison happens at the end of the phase 1 shadow period on reviewed decisions. The owner's stated preference is to run local if quality holds; the eval decides. |
| Q5 | Folder vs category as the primary effect? | Default applied | Folder plus category, as designed. |
| Q6 | Handle `marketing` in v1? | **Decided** | Off by default, one config line to enable (`folders.marketing`). Added 2026-10-13 after the first month of data showed marketing was 40% of the inbox. Same mechanism as the cold outreach folder. |
| Q7 | Folder location? | Default applied | Top-level `Daakiya/Cold Outreach`. |
| Q8 | Owner profile text for the system prompt? | **Open, drafted at `init`** | Two or three lines: role, company, which topics are legitimately the owner's. Needed before the first prompt is measured. |
| Q9 | Does the owner run OpenClaw or another agent gateway? | **Decided** | No. Spike 3 is dropped. The MCP server stays a phase 4 candidate on its own merits. |
| Q10 | Licence? | Default applied | MIT, revisit before phase 3. |
| Q11 | Name and Homebrew tap? | Default applied | Daakiya; `sdas/tap`. |
| Q12 | Local spend cap? | Default applied | 500 LLM calls per day. |
| Q13 | Polling interval? | **Decided** | 5 minutes, configurable. Owner considered hourly and realtime and chose to keep the original. Realtime via IMAP IDLE wake-up is noted as a phase 4 candidate; webhooks remain rejected (would need a hosted endpoint). |
| Q14 | Upfront labelled dataset? | **Decided** | No. Owner questioned the need; labels come from one-keystroke marking in `review` during shadow mode and from corrections. Spike 2 shrank to a local-model viability check. |

## Still needed from the owner

1. The profile text (Q8), written during `init`; it can be revised at any time and the
   change is tracked by `prompt_version`.
| Q15 | LinkedIn mail? | **Decided** | Owner asked for a plain sender rule. Added `[[routes]]`: sender address or domain to folder, applied before the model and in every mode. The owner's config routes `linkedin.com` to a `LinkedIn` folder. |
| Q16 | Threshold after the first month? | **Decided: stay at 0.85** | The first eval showed precision 1.0 at 0.80, so the threshold was briefly lowered. Three rows were still `unknown` when that eval ran; once re-run, two one-line replies in a live accounting thread came back as cold outreach at exactly 0.80. Reverted to 0.85, added the thread-consistency rule, and recorded the lesson: re-run eval after any redo before touching the threshold. |
