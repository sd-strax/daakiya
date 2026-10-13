# 1. Problem

## The symptom

The owner uses Outlook for Mac against a Microsoft mailbox. Every day the inbox receives a
steady stream of cold outreach: sales development reps, recruiters, agencies, "quick
question" openers, follow-up chains ("bumping this to the top of your inbox"), and
LinkedIn-style marketing. None of it is spam in the classic sense. It is addressed to the
owner by name, often references their company or title, comes from real domains with valid
SPF and DKIM, and is sent by humans or tools imitating humans.

The result is that the inbox stops being a to-do list. Real mail from colleagues,
customers, and contacts is buried among messages that need no action.

## Why existing filters fail

| Mechanism | Why it does not solve this |
|-----------|----------------------------|
| Exchange Online Protection / Junk | Tuned for malicious or bulk spam. Cold outreach is low-volume per sender, authenticated, and not malicious, so it scores clean. |
| Focused Inbox | Learns from engagement signals, not from intent. Opaque, not tunable, and still leaves the message in the inbox tab structure. |
| Outlook rules | Pattern matching on sender, subject, or words. Cold outreach has no stable pattern; senders rotate, subjects are personalised, and the words that give it away ("circle back", "15 minutes", "pricing") also appear in legitimate mail. |
| Blocking senders one by one | Each sender is new. The problem is a category, not a list. |

## Why this is a good fit for a small language model

Deciding "is this unsolicited outreach trying to sell me something or get a meeting?" is a
reading-comprehension task on a few hundred words. It does not need world knowledge or
long reasoning. Humans do it in two seconds from the first paragraph. Small hosted models
(Claude Haiku class) and local 4B-parameter models handle this kind of intent
classification well when given a clear rubric, a few examples, and a constrained output.

The cost is correspondingly tiny. At Claude Haiku 5.5 list prices (checked 2026-10-13:
$0.10 per million input tokens, $0.50 per million output tokens), a message that costs
roughly 700 input tokens and 60 output tokens to classify costs about one hundredth of a
cent. One hundred messages a day is about a cent a day. A local model costs nothing per
message.

## Constraints the owner has stated

1. **Bring your own key.** The tool must not ship with, proxy, or bill for any LLM access.
   The owner configures a provider (a hosted API key, or a local inference server such as
   Ollama) and the tool uses it.
2. **Build for one, then give away.** The first deliverable is a working tool for the
   owner's own mailbox. If it works, it should be releasable to other people with minimal
   change, which rules out designs that only work for one tenant or one account type.
3. **Outlook for Mac is the client.** The owner reads mail there and wants the result to
   show up there. The tool does not have to run inside Outlook; it has to produce an
   effect Outlook displays.

## What success looks like

- Cold outreach leaves the inbox within a few minutes of arriving, without the owner
  doing anything.
- Nothing the owner cares about is ever lost. Mistakes are visible and reversible in one
  action.
- After a two-week shadow period, during which the owner's only job is to glance at a
  review table and mark rows right or wrong, measured precision on "cold outreach" is high enough
  that the owner is comfortable letting the tool move mail (target: 98 percent or better
  precision, see [Requirements](02-requirements.md)).
- A second person can install it, point it at their own mailbox and their own model, and
  get the same result without the owner's help.
