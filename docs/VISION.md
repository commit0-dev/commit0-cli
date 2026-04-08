# commit0 — Find Commit Zero

> Trace the root cause. Every bug has a commit zero.

---

## Core Mission

A bug is found in production. 47 services are affected. The blast radius spans 3 teams. Everyone's asking: **which commit caused this?**

commit0 answers that question by combining:
1. **Continuous data flow tracing** across the entire codebase
2. **Temporal code graph** that tracks how the dependency graph evolves across commits
3. **Causal reasoning** that follows data flow backward from a failure to its origin commit
4. **Deep memory** that retains and compresses analysis context across long investigations

---

## 1. Continuous Data Flow Tracing

### Beyond Call Graphs

Current tools (including commit0 today) track `A calls B`. That's a call graph. But bugs don't follow call paths — they follow **data paths**:

```
User input → JSON decode → validate() → transform() → db.Save()
                                              ↓
                              field "email" mutated to lowercase
                                              ↓
                         downstream: notification service reads email
                                              ↓
                              email comparison fails → bug
```

The call graph shows `transform → db.Save`. But the bug is in the **data flow**: the `email` field was lowercased in `transform()`, and the notification service expected original case.

### Data Flow Graph

commit0 already stores `data_flow`, `reads`, `writes` edges. The enhancement is to make these **precise and continuous**:

```
┌─────────────────────────────────────────────────────────────────┐
│  CONTINUOUS DATA FLOW GRAPH                                      │
│                                                                  │
│  Variable: user.Email                                            │
│                                                                  │
│  ┌──────────┐  writes   ┌──────────┐  reads    ┌──────────┐   │
│  │ Register │──────────▸│ DB.Save  │──────────▸│ Notify   │   │
│  │ :42      │           │ :78      │           │ :15      │   │
│  └──────────┘           └──────────┘           └──────────┘   │
│       │                      │                      │          │
│       ▼                      ▼                      ▼          │
│  email = input.Email    stored as-is           reads email     │
│                                                compares with   │
│  ┌──────────┐                                  stored copy     │
│  │Transform │  MUTATION: email.ToLower()                       │
│  │ :55      │  ← THIS IS THE TAINT POINT                      │
│  └──────────┘                                                  │
│                                                                  │
│  Taint chain: Register:42 → Transform:55 (MUTATES) → DB:78     │
│               → Notify:15 (READS MUTATED VALUE) → BUG           │
└─────────────────────────────────────────────────────────────────┘
```

### Implementation: Enhanced AST Extraction

Current tree-sitter extractors capture function-level data flow. Enhance to capture **field-level data flow**:

```go
// Current: "function A data_flows to function B"
// Enhanced: "function A.param[email] data_flows to function B.field[user.Email]"
//           with mutation tag: "lowercased at line 55"
```

This requires deeper AST analysis:
- Track variable assignments within function bodies
- Follow parameter passing across call sites
- Detect mutations (string operations, type conversions, field modifications)
- Store mutation metadata on data_flow edges

### Data Flow Query Language

```bash
# Where does user input end up?
commit0 flow "req.Body" --direction forward

# What writes to this database field?
commit0 flow "users.email" --direction reverse

# Full taint chain from input to output
commit0 flow "req.Body" --to "db.Query" --show-mutations
```

---

## 2. Temporal Code Graph: Git-Aware Evolution

### The Missing Dimension: Time

The current code graph is a snapshot — it shows relationships NOW. But to find commit zero, we need to know how relationships **changed over time**:

```
Commit abc123 (3 days ago):
  + Added transform() function
  + Added data_flow edge: Register → Transform → DB.Save
  + Transform mutates email field (new behavior)
  
Commit def456 (2 days ago):
  ~ Modified Notify to compare emails case-sensitively
  
Commit ghi789 (1 day ago):
  Bug reported: notification emails not matching

COMMIT ZERO: abc123 — introduced the email mutation
```

### Temporal Graph Storage

Each node and edge carries a **commit range**:

```sql
-- SurrealDB schema addition
DEFINE FIELD OVERWRITE introduced_commit ON `function` TYPE option<string>;
DEFINE FIELD OVERWRITE introduced_at     ON `function` TYPE option<datetime>;
DEFINE FIELD OVERWRITE last_modified_commit ON `function` TYPE option<string>;
DEFINE FIELD OVERWRITE last_modified_at  ON `function` TYPE option<datetime>;

-- Edge temporal metadata
DEFINE FIELD OVERWRITE introduced_commit ON calls TYPE option<string>;
DEFINE FIELD OVERWRITE removed_commit    ON calls TYPE option<string>;
```

### Diff-Aware Indexing

When indexing, compare the current graph against the previous graph:

```
For each commit in range:
  1. Checkout commit
  2. Parse → extract nodes + edges
  3. Diff against previous graph:
     - New nodes → mark introduced_commit
     - Removed nodes → mark removed_commit on old record
     - Changed edges → track when relationships changed
  4. Store temporal metadata
```

This creates a **time-series code graph** where we can query:
- "When was this function introduced?"
- "When did A start calling B?"
- "What changed in the data flow between commit X and Y?"

### Commit Zero Detection Algorithm

```
INPUT: A bug description or failing test
OUTPUT: The commit that most likely introduced the bug

1. LOCATE: Find the functions involved in the bug
   → Agent uses search_code to find relevant functions

2. TRACE: Follow data flow backward from the failure point
   → Agent uses flow tracing to find taint chains

3. TIMELINE: For each function in the taint chain, query temporal graph
   → When was each function last modified?
   → When were data_flow edges introduced/changed?

4. CORRELATE: Find the commit that introduced or modified
   the taint-producing function or edge
   → Sort by temporal proximity to bug report
   → Weight by data flow position (closer to taint point = higher)

5. VERIFY: Ask the agent to analyze the suspect commit's diff
   → Does the change explain the observed behavior?
   → What was the developer's intent?

6. REPORT: Present commit zero with full causal chain
```

```bash
# Find the root cause
commit0 find-root "notification emails not matching for new users"

# Output:
# Commit Zero: abc123 (3 days ago, by @alice)
# "Normalize email addresses to lowercase"
# 
# Causal Chain:
#   1. Register(email) → Transform.normalize() [INTRODUCED in abc123]
#   2. Transform lowercases email → DB.Save(lowered_email)
#   3. Notify reads email from DB (lowercased)
#   4. Notify compares with original email (mixed case) → MISMATCH
#
# The normalize() function was added with good intent (email dedup)
# but downstream Notify service assumes original case is preserved.
#
# Fix: Either make Notify case-insensitive, or store both forms.
```

---

## 3. Memory Management: Hierarchical Compressed Context

### The Problem

A complex investigation spans hours. The developer asks 20 questions, the agent calls 50 tools, generates 200 code snippets. The context window fills up and the agent loses track.

### Solution: Three-Tier Memory

```
┌────────────────────────────────────────────────────────┐
│  TIER 1: WORKING MEMORY (in context window)            │
│  • Current conversation turn (user message + response) │
│  • Last 3-5 tool results (full detail)                 │
│  • Active hypotheses and investigation state            │
│  • ~8K tokens                                           │
├────────────────────────────────────────────────────────┤
│  TIER 2: SESSION MEMORY (compressed summaries)         │
│  • Previous turns summarized to 1-2 sentences each     │
│  • Key findings with file:line references               │
│  • Tool results compressed to conclusions only          │
│  • Maintained across the investigation session          │
│  • ~4K tokens                                           │
├────────────────────────────────────────────────────────┤
│  TIER 3: PERSISTENT MEMORY (cross-session knowledge)   │
│  • Codebase architecture summary (auto-generated)       │
│  • Known patterns: "auth uses JWT middleware chain"     │
│  • Previous investigations: "email bug was in transform"│
│  • Developer preferences: "prefers Go, uses errgroup"   │
│  • Stored in SurrealDB, retrieved by relevance          │
│  • ~2K tokens (top-K relevant memories per query)       │
└────────────────────────────────────────────────────────┘
```

### Context Compression Pipeline

After each agent turn, compress the context:

```
FULL CONTEXT (128K limit)
  │
  ▼ After each turn:
COMPRESS older turns:
  Turn 5 (full): "User asked about auth. Agent searched, found 5 results..."
  → Compressed: "Investigated auth flow: JWT middleware in auth.go:42, 
     calls validateToken. 5 functions in chain."
  
  Turn 4 (full): "User asked to trace validateToken..."
  → Compressed: "Traced validateToken: 3 callers (authMiddleware, 
     TestAuth, benchAuth). No data mutations."
  
  Turns 1-3 (ultra-compressed):
  → "Session started investigating notification email mismatch.
     Found Register→Transform→DB→Notify data flow chain."
```

### Implementation

```go
// internal/app/memory/
type MemoryManager struct {
    working    WorkingMemory    // current turn, last N tool results
    session    SessionMemory    // compressed history of this investigation
    persistent PersistentMemory // cross-session knowledge in SurrealDB
    compressor Compressor       // LLM-based context compression
}

// Compressor uses Gemma 4 to compress older turns
type Compressor struct {
    model ModelProvider
}

func (c *Compressor) CompressTurn(turn Turn) (string, error) {
    // Prompt: "Summarize this investigation turn in 2 sentences,
    // preserving file names, line numbers, and key findings."
}

func (c *Compressor) CompressSession(turns []CompressedTurn) (string, error) {
    // Prompt: "Create a 3-sentence summary of this investigation so far,
    // preserving the causal chain and key evidence."
}
```

### Context Budget System

Each agent call has a context budget:

```
Total context window: 128K tokens (Gemma 4 E4B)

Budget allocation:
  System prompt:          ~1K tokens (fixed)
  Working memory:         ~8K tokens (current turn + recent tools)
  Session memory:         ~4K tokens (compressed history)
  Persistent memory:      ~2K tokens (relevant knowledge)
  Tool definitions:       ~1K tokens (fixed)
  Code context:           ~16K tokens (relevant code from graph)
  ─────────────────────────────────────
  Available for response: ~96K tokens (plenty)
```

The system **never exceeds the budget** — when working memory grows beyond 8K, older entries are compressed into session memory. When session memory exceeds 4K, oldest entries are ultra-compressed.

---

## 4. Putting It Together: The commit0 Investigation Flow

```
Developer: "Users are reporting that notification emails don't match"

Agent [THINKING]:
  I need to understand the notification email system.
  Let me search for email-related functions first.

Agent [TOOL: search_code]:
  → Found: Notify.sendEmail(), Register.createUser(), Transform.normalize()

Agent [THINKING]:
  The Transform.normalize() function looks suspicious.
  Let me trace the data flow from user registration to notification.

Agent [TOOL: flow_trace]:
  → Taint chain: req.Body.email → Register:42 → Transform:55 (MUTATES: toLower) 
    → DB.Save:78 → Notify:15 (reads email, compares case-sensitively)

Agent [THINKING]:
  Found it. Transform.normalize() lowercases the email, but Notify
  compares it case-sensitively. When was normalize() introduced?

Agent [TOOL: temporal_query]:
  → Transform.normalize() introduced in commit abc123, 3 days ago
  → Author: @alice, message: "Normalize email addresses for dedup"

Agent [TOOL: analyze_diff abc123]:
  → Added: func normalize(email string) string { return strings.ToLower(email) }
  → Called from: Register.createUser() line 42

Agent [RESPONSE]:
  ## Commit Zero: abc123

  **Author**: @alice (3 days ago)
  **Message**: "Normalize email addresses for dedup"

  ### Causal Chain
  1. `Register.createUser()` (register.go:42) passes email to `Transform.normalize()`
  2. `normalize()` (transform.go:55) converts email to lowercase — **introduced in abc123**
  3. Lowercased email stored in DB via `DB.Save()` (db.go:78)
  4. `Notify.sendEmail()` (notify.go:15) reads email from DB and compares
     with original case-sensitive email from the request → **MISMATCH**

  ### Why It Broke
  The normalize() function was added for email deduplication (reasonable intent),
  but the Notify service assumes the stored email preserves original case.

  ### Recommended Fix
  Either:
  - Make Notify.sendEmail() case-insensitive: `strings.EqualFold(stored, original)`
  - Or store both `email` (original) and `email_normalized` (for dedup)

[SESSION MEMORY compressed]:
  "Investigation: email mismatch in notifications. Root cause: commit abc123
   introduced Transform.normalize() which lowercases emails. Notify service
   reads lowercased email but compares case-sensitively. Fix: case-insensitive
   comparison or dual storage."
```

---

## 5. New CLI Commands

```bash
# Core mission: find commit zero
commit0 find-root "notification emails don't match"
commit0 find-root --test "TestNotifyEmailMatch" --since "3 days ago"

# Data flow tracing
commit0 flow "req.Body.email" --direction forward --show-mutations
commit0 flow "db.users.email" --direction reverse --to-commit abc123

# Temporal queries
commit0 history "Transform.normalize" --show-graph-changes
commit0 diff abc123..HEAD --graph  # show how code graph changed across commits

# Investigation sessions
commit0 investigate "notification bug"  # starts interactive agent session
commit0 investigate --resume session-id  # resume previous investigation
```

---

## 6. Implementation Priority

| Priority | Feature | Why First |
|----------|---------|-----------|
| **P0** | Field-level data flow extraction | Foundation for everything — without precise data flow, we can't trace bugs |
| **P0** | Temporal graph (commit metadata on nodes/edges) | Enables "when was this introduced" queries |
| **P1** | Data flow query engine | `commit0 flow` command — the core value proposition |
| **P1** | Context compression + memory tiers | Enables long investigations without losing context |
| **P1** | Commit zero detection algorithm | The headline feature — combines flow + temporal + reasoning |
| **P2** | Background watcher + incremental temporal updates | Real-time graph evolution tracking |
| **P2** | Code review with data flow awareness | Review diffs for data flow mutations |
| **P3** | Security scanner (taint analysis) | Natural extension of data flow tracing |
| **P3** | Auto documentation | Nice-to-have, lower priority than core mission |

---

## 7. Competitive Moat

No existing tool does what commit0 does:

| Capability | git bisect | Sentry | Datadog | Sourcegraph | **commit0** |
|------------|-----------|--------|---------|-------------|-------------|
| Finds failing commit | Yes (manual) | No | No | No | **Yes (automated)** |
| Data flow tracing | No | No | Runtime only | Static only | **Static + semantic** |
| Temporal code graph | No | No | No | No | **Yes** |
| Causal reasoning | No | No | No | No | **Yes (LLM agent)** |
| Works offline | Yes | No | No | No | **Yes** |
| Zero cost | Yes | No | No | No | **Yes** |
| Understands WHY | No | No | No | No | **Yes** |

`git bisect` finds the commit but doesn't explain WHY. Sentry/Datadog show the error but not the cause. Sourcegraph searches code but doesn't reason about data flow. **commit0 does all of this: finds the commit, traces the data flow, explains the causality, and suggests the fix.**
