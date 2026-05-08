# Platform-readiness critique — commit0 as a daily-collaboration platform

| Field | Value |
|---|---|
| Date | 2026-05-08 |
| Author persona | Principal engineer, 400-person product company, 70 services, 8 years of code |
| Audience | commit0 maintainers; future-self when revisiting direction |
| Status | Captured for reference. Not a plan. Not approved scope. |
| Triggered by | Session 2026-05-08 (PRs #55, #58, #59, #60, #61 merged — 18-tool MCP surface live, HTTP MCP transport at `/mcp`, docker bind-mount, integration loop closed) |
| Related ROADMAP | (this doc seeds a new `[ROADMAP-2]` issue — see References) |

---

## TL;DR

commit0 today is a well-built code-search server with an MCP surface. It is **not** a daily-collaboration platform, and several of the gaps are structural — they cannot be patched with more tools. Three architectural decisions gate everything that follows:

1. **Single-user tool or team platform?** — currently a single-user tool with team-platform branding.
2. **Database or event log?** — currently a database; no append-only log, so freshness, time travel, and reactive workflows are out of reach.
3. **Code graph or knowledge graph?** — currently a code graph; the schema doesn't admit people, decisions, incidents, deploys, or telemetry as first-class citizens.

Until those three decisions are made and the consequences followed, more MCP tools, more endpoints, and more UIs will keep paving over a foundation that doesn't carry the weight of real-org adoption. This document captures the principal-engineer view of what's missing and why it matters.

---

## What this document is, and isn't

**Is:** an honest critique from the perspective of someone we want as a customer. The voice is sharper than usual on purpose — the goal is signal, not consensus.

**Isn't:**
- A roadmap. A separate ROADMAP issue tracks scope.
- A negation of what's already shipped. Several things are right (see *Calibration* below).
- A rejection of MCP. MCP is fine; it's the framing that needs work.

---

## The frame

> "A tool gets used when a human remembers to call it. A platform produces value whether or not anyone's looking at it."

A **tool** is something my team explicitly invokes (CLI, MCP call, button click). A **platform** is something my team *passively benefits from* (PR review bot, Slack DMs about what changed, IDE hover, on-call summaries). Today commit0 is firmly in the tool category — it has 18 ways to be invoked and zero ways to push value at the team unprompted.

The gap is not "ship more invocation methods." The gap is "build the substrate that lets value flow without invocation."

---

## Architectural complaints (the three that gate everything)

### A1. Single-tenant by default, and the design knows it

There is no concept of a team. No subscriptions. No "what changed since you last looked." Two engineers running commit0 on the same codebase produce two separate truths and have no path to reconcile them. Setup instructions assume me-on-laptop. There is no answer to:

- Where does my team's instance live?
- Who owns its uptime?
- How do team members share state?
- Who pays for the embeddings?
- How do we reconcile when two laptops have different indexes?
- What happens when an instance restarts?

This is not a missing feature. It's a missing model. Every interesting design decision downstream — identity, ACLs, freshness, federation — was deferred. You can feel the deferral in every corner.

**Why this gates everything:** without a team model, commit0 cannot collaborate. A tool helps one person. A platform connects many. None of the workflow surfaces (PR bot, Slack, IDE hover, on-call) make sense without team identity.

### A2. The substrate is a database, not an event log

The graph is a single mutable state in SurrealDB. There is no append-only log of changes. Consequences:

- I cannot ask "what did this look like 3 months ago" — there is nowhere for the past to live.
- Freshness is a manual operation (`commit0-cli index .`). The system does not react to git push, GitHub webhook, Slack message, OTel span. I have to remember to refresh; if I forget, I work off stale data silently.
- Subscriptions are impossible without an event stream. You cannot subscribe to a snapshot.
- Federation is impossible without an event stream. You cannot replicate a snapshot.

**Why this gates everything:** the entire push channel (notifications, PR bot, Slack DMs about change) is downstream of "the system reacts to events." Today the system reacts to nothing.

### A3. Code graph cosplaying as knowledge graph

Edges today: `calls`, `data_flow`, `tests`, `route`, `implements`. Decent for code search. Useless for the questions I actually ask in a real org:

| Question | Required node type | Have it? |
|---|---|---|
| Who owns this? | Person, Team | No |
| Why is it written this way? | ADR, Decision | No |
| Did the latest incident touch this? | Incident, Postmortem | No |
| Is this on the hot path? | TelemetrySample | No |
| Has anyone touched this in 6 months? | Commit | Partially (git metadata exists but isn't a queryable node) |
| What did we agree on in the design review? | DesignDoc, MeetingNote | No |
| What's the runbook for this? | Runbook | No |

The schema admits one kind of fact: *what code calls what*. Real platform questions are about *people, decisions, time, operations*. Until the schema admits the rest of the world, commit0 will keep answering technically-pedantic questions and ignoring the human ones.

**Why this gates everything:** the "live wiki" promise is impossible without these node types. A live wiki is not "a chatbot in front of a code search engine"; it's "the graph itself, queried." Today the graph has nothing to say about the things people actually wiki about.

---

## Operational complaints

### O1. The deployment story is broken

`docker compose up -d` works for me-on-laptop. It does not answer:
- How does my team share an instance?
- How do dev laptops stay in sync with the team instance?
- Where do API keys live? (The current `.env` file ships keys into containers in plaintext.)
- What's the upgrade path when the SurrealDB schema migrates?
- How do I run a CI instance vs a prod instance vs a dev laptop instance from the same image with different configs?

The day commit0 wants to be adopted by a serious org, these questions arrive in week one, not week three.

### O2. Freshness is manual

The index goes stale silently. There is no detection ("your index is 11 days old; some answers may be wrong"), no auto-refresh on git push, no webhook intake. The closest mechanism is `commit0-cli index .` and a discipline note in `CLAUDE.md`. Discipline notes do not scale to teams.

### O3. ACLs and privacy are absent

The minute commit0 ingests Slack threads, private design docs, or HIPAA-flagged code, the question "who can search what" becomes existential. Today the answer is "anyone who can reach `/api/v1/query` can read everything." For any org with a Security review, this is a non-starter. Not a follow-up — a non-starter.

The right answer is not "we'll add ACLs later." It's "the schema needs to carry an `access_scope` column on every node and edge from day one, even if every value is currently `public`." That data-model decision is cheap now and impossibly expensive later.

### O4. Operability is opaque

A query takes 5 seconds. Was it embedding? FTS? RRF fusion? SurrealDB query latency? The MCP serializer? Network hop? There is essentially no tracing on commit0 itself. structured logs exist but do not correlate by request ID across stages. There is no slow-query log, no token-cost dashboard, no on-call runbook for commit0 itself.

If commit0 wants to be infrastructure, it has to act like infrastructure.

### O5. Identity resolution is missing

A function survives a rename refactor as a different node ID — its history breaks. The same person across email, Slack, GitHub is three separate facts. The same incident across PagerDuty, Linear, and a post-mortem PR is three uncorrelated rows. Without entity resolution, "show me everything related to X" returns the splinters and asks the human to do the merge.

Entity resolution is genuinely hard. The right move is not to solve it now; it's to **acknowledge identity as a first-class column** on every node and edge so it can be resolved lazily later. Today there is nowhere to even put the question.

---

## Trust complaints

### T1. Confidence is invisible

`commit0_blast User.Email` returns 12 affected nodes. Is that complete? The footer says `edge_resolution: 46.1%`. So no — more than half the call edges in the relevant scope are unresolved. The tool did not bother to mention this at the top of the answer. For a refactor I'm shipping today, that gap is the entire decision.

Confidence is not a UI feature. It is a data-model feature. Every edge needs provenance, freshness, and a confidence score. Every answer needs to inherit and aggregate them. Today commit0 returns answers as if they're facts.

### T2. No time dimension

I cannot ask "why did the auth chain change between v2.3 and v2.4," "when did this become reachable from public input," "show me all functions that gained a new caller in the last week." The DB knows only "now." A wiki without history is a notepad that the last writer wins.

### T3. The tool can't tell me when it's wrong

Hallucinated qualified names from semantic search. Missed callers because of an unresolved interface. Stale data from a week-old index. All silent failures. The tool answers confidently regardless of how shaky the foundation is. Trust comes from answers that include their own uncertainty — and from a tool that says "I don't know" when it shouldn't guess.

---

## Workflow complaints

### W1. The MCP surface is celebrated as if it were the product

The README and the codebase celebrate MCP. 18 tools. `node://` resources. Streamable HTTP. All real, all beside the point. MCP is plumbing — the wire format for agents to call tools. Counting tools is a vanity metric.

Adoption does not come from "we support MCP." Adoption comes from outcomes:
- "My Slack DM'd me when a load-bearing function in payments changed."
- "My PR got auto-reviewed with blast and security findings before a human looked."
- "My IDE shows callers and recent PRs when I hover."
- "My on-call alert came with a snippet of the code that's failing."

Stop selling the protocol. Sell the workflow.

### W2. Authoring doesn't exist

ADRs, runbooks, postmortems — these are the high-value knowledge in any org. They live in Confluence, Notion, `docs/`, Slack threads. commit0 ingests none of them. Worse: it does not help me *write* them. A live wiki should make authoring as cheap as querying. commit0 makes authoring exactly as expensive as it was before — i.e. expensive enough that nobody does it.

### W3. The agent layer is grafted on

A real assistant maintains conversational state, learns from corrections ("no, that wasn't the function I meant"), gets better as I use it. commit0's agent runs the same prompt every time, with no per-user memory, no correction loop. Without state, the assistant cannot improve. It just responds.

---

## What "good" looks like (expectations summary)

| # | Expectation | Why it matters |
|---|---|---|
| 1 | **Append-only event log** as the substrate; the graph is a projection. | Time travel, freshness, federation, subscriptions all become possible — and most become free. |
| 2 | **Knowledge-graph schema:** code, people, decisions, incidents, deploys, runs, alerts, conversations are co-equal node types. | Admits the questions humans actually ask. |
| 3 | **Identity as a first-class column** on every node and edge. | Function survives rename. Person is the same across systems. Incident is one thing. |
| 4 | **Confidence and provenance** travel with every fact. Queries return uncertainty alongside facts. | Trust calibration; safety-critical answers actionable. |
| 5 | **Subscriptions are symmetric with queries.** Anything you can ask, you can subscribe to. | The push channel — the leverage of "platform" over "tool." |
| 6 | **Per-source ACLs** scoped by subject identity. | Adoption in any org with a Security function. |
| 7 | **Authoring as cheap as querying.** Drop a Markdown file, it's queryable in 30s. The platform is the wiki. | Eliminates the "stale doc" problem at its source. |
| 8 | **Federation, not centralization.** Each laptop runs an instance; team instance federates. | Resilient, fast locally, no central choke point. |
| 9 | **Workflow surfaces are the product.** PR bot, Slack bot, IDE hover, on-call summary. CLI / MCP are integration points, not headlines. | Adoption follows where humans already work. |
| 10 | **Operability built in.** OTel traces, slow-query log, cost dashboard, on-call runbook for commit0 itself. | Infrastructure must behave like infrastructure. |
| 11 | **It tells you when it's wrong.** Stale-index banner. Low-confidence flag. Refusal to guess on safety-critical queries. | Trust calibration in the surface, not just the data. |

---

## Calibration — what commit0 already gets right

The critique above is sharp. Honesty also requires noting what's already mature:

- **Hexagonal architecture.** Domain / app / adapters layering is enforced and respected. Adapters can be swapped. The MCP surface is bolted on cleanly without polluting the domain.
- **OpenCodeGraph abstraction.** A single port interface for the graph that decouples SurrealDB from semantics is the right shape. Multiple backends become possible without rewriting services.
- **Lazy-init contract.** Server boots even when SurrealDB is unreachable; tools return clear error messages on first call. Professional behaviour for a development-time tool.
- **Per-PR-one-feature discipline.** The captured `feedback_one_feature_per_pr.md` lesson and the way this session delivered five PRs in three slices (#55 / #58 / #59 / #60 / #61) shows the team can ship discipline at scale.
- **Dogfooding rule.** `CLAUDE.md` requires using commit0's own tools as the default code-intelligence path. Mature stance — tools you don't use yourself are tools you ship as trash.
- **Real-BAU verification gate.** This session's user-driven insistence ("not just tool calls, real BAU") forced #56 and #57 to surface and close. That cultural rule is one of commit0's most valuable assets.
- **18-tool MCP surface is well-designed.** Coverage of search, trace, tests, diff, interface, meta, security, API is comprehensive. The shape is right; the framing (MCP-as-product) is what needs adjustment.

These deserve to be preserved as the project moves toward platform shape. Several of them — particularly the dogfooding rule and the per-PR discipline — are exactly what makes the platform pivot survivable.

---

## Anti-priorities — what NOT to do

If the next session is tempted to sprint forward without addressing the three architectural decisions:

- **Do not ship more MCP tools.** 18 is enough. The marginal tool moves zero needles.
- **Do not build a chat UI before the schema can support what it shows.** A chat that can only answer "where is X" because the graph only knows code edges will look impressive in a demo and fail in adoption.
- **Do not build a GitHub App until subscriptions exist.** A bot is a subscription delivery mechanism. Without the substrate, it's a one-off.
- **Do not ingest Slack until ACLs are designed.** Ingesting a private channel without a scoping model is a Security incident waiting to happen.
- **Do not optimize the indexer before deciding on the event-log substrate.** Performance work on a snapshot indexer is wasted if the substrate moves to event-driven.

---

## The three foundational decisions, restated

1. **Tool or platform?** Pick one. Follow the consequences.
2. **DB or event log?** Pick one. The current "DB" answer caps every reactive workflow.
3. **Code graph or knowledge graph?** Pick one. The current "code graph" answer caps the wiki promise.

Three months on these decisions will yield more value than three months on the next 30 issues in the queue.

---

## References

- This session's merged PRs: #55 (`commit0_index_status` + tracker registry), #58 (`list_repos` + `list_files` + `node://` resource), #59 (`scan_security` + `api_surface` + integration tests), #60 (docker bind-mount fix for #57), #61 (HTTP MCP at `/mcp` for #56).
- Closed issues this session: #15 (ROADMAP — 18-tool surface), #28 (meta + security tools), #56 (HTTP↔MCP integration loop), #57 (docker volume mount).
- Plan file (single-shot vision; this doc supersedes its scope): `~/.claude/plans/commit0-integrative-assistant-and-live-wiki.md`.
- Captured cultural rules in `CLAUDE.md`: dogfood-commit0; one feature per PR; full English identifiers; co-author trailer; PR title format.
- New ROADMAP-2 issue (created from this doc): see GitHub.

---

## Disposition

This document is **input to direction-setting**, not direction itself. The maintainers should:

1. Read it.
2. Disagree with the parts that deserve disagreement.
3. Make the three foundational decisions explicitly (yes/no, with rationale).
4. Open the new `[ROADMAP-2]` issue with the chosen direction and link this doc.
5. Revisit this doc after the next major milestone (3 months out) to check what the team's view confirmed and what was overstated.

The point of writing it down is so the disagreements happen on the document, not in someone's head over six months of gradual drift.
