# commit0 — Multi-Agent Orchestration Architecture

> Analyst Agent with memory, evidence ranking, and feedback loops.

**Last updated:** 2026-04-09

## Overview

A single user prompt triggers autonomous comprehensive code analysis. The **Analyst Agent** (orchestrator) plans, delegates to specialized sub-agents, reads results, evaluates quality, follows up on gaps, and converges on a comprehensive report — without human intervention.

## Core: The Analysis Scratchpad

The Scratchpad is the memory, the ranking system, and the feedback loop.

```
Scratchpad {
    Goal            — original user question
    Strategy        — chosen investigation strategy
    Plan            — ordered steps to execute
    Evidence[]      — scored findings with provenance (the ranking system)
    ActionLog[]     — every tool call + result (prevents repeats)
    OpenQuestions[] — unanswered questions, ranked by priority (drives follow-up)
    Hypotheses[]    — theories with confidence tracking (drives convergence)
    NovelFindings[] — novel findings per delegation (convergence detection)
    CostBudget      — max cost in dollars
    TokenBudget     — max tokens for scratchpad reads
}
```

### Evidence Scoring

Each finding scored on 4 dimensions (0.0-1.0):
- **Relevance** — relates to the Goal?
- **Confidence** — reliable source? (capped by source type: search=0.7, trace=0.9, deep_dive=0.95)
- **Novelty** — new information? (server-side dedup check)
- **Actionability** — can we act on this?

**Priority** = 0.3×Relevance + 0.3×Confidence + 0.2×Novelty + 0.2×Actionability

Scores are **server-side validated** — the agent proposes, the server adjusts.

### Convergence Gates (5 required)

1. Minimum 3 delegations
2. Minimum 5 high-priority evidence items
3. No open questions with priority > 0.7
4. Last 2 delegations produced < 2 novel findings each
5. At least one hypothesis confirmed or rejected

## Sub-Agent Types

| Type | Tools | Purpose |
|------|-------|---------|
| `search` | search_code, lookup_node | Discovery — find entities |
| `trace` | trace_calls, get_neighborhood, flow_trace | Structure — map connections |
| `security` | search_code, flow_trace, blast_radius, temporal_query | Analysis — find risks |
| `deep_dive` | lookup_node, get_neighborhood, analyze_commit_diff, temporal_query | Detail — read code and history |

## Feedback Loop

```
DELEGATE → RECEIVE → EXTRACT EVIDENCE → UPDATE HYPOTHESES → GENERATE QUESTIONS → CHECK CONVERGENCE → DECIDE NEXT
```

After every delegation:
1. Parse results into discrete Evidence items with scores
2. Update hypothesis confidence (support/contradict)
3. Generate new questions, close answered ones
4. Check convergence gates
5. If not converging → next highest-priority action

## Guardrails

- **Token budget**: Scratchpad reads return budgeted views (top-10 evidence, open items only)
- **Score validation**: Server caps confidence by source reliability, dedup-checks novelty
- **Failure handling**: Circuit breaker returns TIMEOUT/EMPTY/UNSTRUCTURED signals
- **Contradiction detection**: Auto-reduces confidence, generates resolution questions, blocks convergence
- **Cost control**: 80% warning, hard stop at budget limit
- **Protocol enforcement**: delegate tool REFUSES if scratchpad not updated; write_report REFUSES if sections lack evidence
- **Handoff contracts**: Every delegation includes task, constraints, budget, timeout, expected format
- **Quality gate**: Report validated for evidence references before rendering

## Files

```
internal/app/agent/scratchpad.go       — Scratchpad struct, scoring, convergence, validation
internal/app/agent/scratchpad_tools.go — update/read/check_redundancy/plan_analysis tools
internal/app/agent/delegate.go         — delegate tool, sub-agent runner, contracts, enforcement
internal/app/agent/instructions.go     — analyst + 4 sub-agent instructions
internal/app/agent/service.go          — registration and initialization
```
