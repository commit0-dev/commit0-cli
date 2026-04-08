# commit0 — AppSec Feature Roadmap

> Product analysis for the next generation of security features.
> Each feature builds on commit0's unique foundation: code graph + temporal history + data flow + LLM reasoning.
> Focus: capabilities that are structurally impossible for pattern-matching tools like Semgrep.

**Last updated:** 2026-04-08

---

## Current State: Honest Assessment

commit0 has **strong security infrastructure** but **shallow detection logic**.

The `AnalysisService` today runs 4 hardcoded taint rules using `strings.Contains()` matching on qualified names. It finds a source pattern like `"req.Body"`, traces field flow forward 5 hops, and checks if any hop's name contains a sink pattern like `"db.Query"`. Sanitizers are checked the same way — if any hop in the chain contains `"Prepare"`, the flow is marked safe regardless of whether `Prepare` was actually applied to the tainted value.

This produces two failure modes:
1. **False positives**: `db.QueryBuilder.Build()` matches the `"db.Query"` sink pattern but isn't a SQL sink.
2. **False negatives**: A custom database wrapper `repository.Execute(query)` doesn't contain `"db.Query"` and is invisible.

The mutation detector only fires on `X = func(X)` assignment patterns. Return value flows (`result := process(userInput)` then `db.Query(result)`) are invisible. The `MutationKind` enum defines 5 types but only `MutationTransform` is ever emitted.

**The deeper gap**: commit0 has no concept of HTTP API surfaces. The tree-sitter extractors parse functions, classes, and calls — but they don't extract route registrations (`e.GET("/path", handler)`), request bindings (`c.Param("id")`), response shapes (`c.JSON(200, user)`), or middleware chains. The code graph knows that `Handler.GetUser` calls `Service.FindUser`, but doesn't know that `Handler.GetUser` is bound to `GET /api/v1/users/:id` and receives user-controlled input through the `:id` path parameter.

Without API surface awareness, security analysis is blind to the most important question: **what data enters the system from the outside, what path does it take, and what does the system expose back?**

**What works well**: The field flow infrastructure (`FieldFlowService` + `FieldFlowStore`), temporal tracking (`TemporalService`), graph traversal (`TraceService` + `BlastService`), and embedding-based semantic search are solid. The plumbing is right. What sits on top needs depth.

---

## Feature 1: Semantic Source-Sink Classification Engine

### The Problem

A fintech startup uses commit0 to scan their Go backend. The security service reports 47 findings. The security engineer spends 4 hours triaging:

- 18 are false positives: `db.QueryRow` matched the `"db.Query"` sink pattern, but the argument was a parameterized query — safe by construction.
- 12 are true positives but low-quality: the taint path says `Register.createUser → db.Save` but doesn't explain *which parameter* carries the taint or *why* `db.Save` is dangerous.
- 6 real vulnerabilities were missed entirely: the team uses a custom ORM method `repo.FindByEmail(email)` that internally calls `db.Query` — but because `FindByEmail` doesn't contain `"db.Query"` in its name, the sink is invisible.
- 11 are noise: functions that happen to have "Query" in their name (`QueryBuilder.Build`, `QueryParams.Validate`) but aren't execution sinks.

The engineer's conclusion: "This tool wastes more time than it saves."

### Why Semgrep Cannot Solve This

Semgrep matches syntactic patterns. It can write a rule for `db.Query($ARG)` and flag every call site. But:

1. **It cannot follow `$ARG` through function boundaries.** If user input passes through `service.Process(input)` and `Process` returns a value that reaches `db.Query`, Semgrep sees two disconnected call sites. It doesn't know they're linked.

2. **It cannot discover custom sinks.** A team wraps `db.Query` inside `repo.FindByEmail`. Semgrep needs a new rule for every wrapper. In a codebase with 200 repository methods, that's 200 rules — manually written, manually maintained, guaranteed to drift.

3. **It cannot reason about context.** `db.Query("SELECT 1")` with a hardcoded string is safe. `db.Query(userInput)` is dangerous. Semgrep can check if the argument is a literal, but can't follow a variable through 4 function boundaries to determine whether it originated from user input.

These aren't bugs in Semgrep. They're architectural limits of pattern matching. Solving them requires a code graph.

### The Insight

commit0 already embeds every function into a 3072-dimensional vector space where semantic similarity is measurable. A function like `repo.FindByEmail` that internally calls `db.Query(SELECT * FROM users WHERE email = ?)` has an embedding **close to** other database query functions — because the summarizer and context builder captured its behavior.

Instead of maintaining a static list of sink patterns, commit0 can **discover** sinks by semantic similarity. Seed the system with known sinks (`db.Query`, `exec.Command`, `os.Open`), then find all functions within cosine distance N that behave similarly. The LLM verifies each candidate.

No other SAST tool has embeddings of every function. No other tool can search for "functions that behave like database queries." This is what makes graph-based classification fundamentally different from pattern matching.

### Solution Design

**Core abstraction: SecurityCatalog**

```go
// internal/app/security_catalog.go

type SecurityRole string
const (
    RoleSource    SecurityRole = "source"     // Where external input enters
    RoleSink      SecurityRole = "sink"       // Where dangerous operations happen
    RoleSanitizer SecurityRole = "sanitizer"  // What neutralizes taint
    RoleValidator SecurityRole = "validator"  // What validates but doesn't transform
    RoleAuthGate  SecurityRole = "auth_gate"  // What enforces authorization
)

type SecurityClassification struct {
    Role           SecurityRole
    Category       string        // "sql", "command", "xss", "path", "crypto", "auth"
    Confidence     float64       // 0.0-1.0 — how certain is the classification
    Method         string        // "seed", "embedding", "llm", "manual"
    DangerousParam int           // Which parameter index carries the risk (0-indexed, -1 for return)
    CWE            string        // "CWE-89", "CWE-78", etc.
    MitigatedBy    []string      // Categories of sanitizers that neutralize this sink
}
```

**Discovery pipeline (3 stages)**:

**Stage 1 — Seed catalog.** Ship a curated set of ~200 source/sink/sanitizer classifications covering Go stdlib, common frameworks (Echo, Gin, net/http), and database packages. These are the anchors.

```go
var seedCatalog = []SeedEntry{
    {Qualified: "database/sql.DB.Query",     Role: RoleSink, Category: "sql", DangerousParam: 0, CWE: "CWE-89"},
    {Qualified: "database/sql.DB.Exec",      Role: RoleSink, Category: "sql", DangerousParam: 0, CWE: "CWE-89"},
    {Qualified: "os/exec.Command",           Role: RoleSink, Category: "command", DangerousParam: 0, CWE: "CWE-78"},
    {Qualified: "net/http.Request.FormValue", Role: RoleSource, Category: "user-input"},
    {Qualified: "html.EscapeString",         Role: RoleSanitizer, Category: "xss"},
    // ...200 more
}
```

**Stage 2 — Embedding expansion.** For each seed sink, find the top-K functions in the codebase whose embeddings are most similar. These are *candidate* sinks — functions that behave like known sinks but have custom names.

```go
// Seed: "database/sql.DB.Query" → embed its description
// Find: "internal/repo.UserRepo.FindByEmail" → embedding similarity 0.91
// Why: its body contains SQL query construction, summarizer captured "queries users table"
candidates := vectorIndex.Search(ctx, seedEmbedding, SearchParams{TopK: 20, MinScore: 0.75})
```

**Stage 3 — LLM verification.** For each candidate, ask the LLM: "Is this function a SQL query sink? Here is its body, its callers, and its callees." The LLM returns a structured classification with confidence score.

**Result**: The catalog knows `repo.FindByEmail(email)` is a SQL sink where parameter 0 is dangerous — even though it was never in any hardcoded list.

**Storage**: Classifications stored as metadata on nodes in SurrealDB:

```sql
DEFINE FIELD security_role     ON function TYPE option<string>;
DEFINE FIELD security_category ON function TYPE option<string>;
DEFINE FIELD security_cwe      ON function TYPE option<string>;
DEFINE FIELD security_param    ON function TYPE option<int>;
```

### What Semgrep Cannot Replicate

| Capability | Semgrep | commit0 |
|---|---|---|
| Match known sink by name | Yes (pattern rule) | Yes (seed catalog) |
| Discover wrapper around known sink | No | Yes (graph: wrapper calls db.Query) |
| Discover semantically similar sink | No | Yes (embedding similarity + LLM verify) |
| Know which parameter is dangerous | Partial (metavar position) | Yes (DangerousParam per classification) |
| Know what sanitizers are effective | No (just "is there a sanitizer?") | Yes (MitigatedBy per sink category) |
| Auto-update when code changes | No (rules are static) | Yes (re-classify on re-index) |

### Success Criteria

- **Precision**: >85% of catalog-discovered sinks are true sinks (manual review of top 50)
- **Recall**: >70% of actual sinks discovered (vs. manual audit of 3 open-source Go projects)
- **Noise reduction**: Taint scan false positive rate drops from ~40% to <15%
- **Discovery rate**: At least 30% of sinks found via embedding expansion (not in seed list)

### Engineering Complexity

**Core work**: SecurityCatalog struct, seed data file (~200 entries), embedding expansion pipeline, LLM verification prompt, SurrealDB schema addition.

**Hard part**: Tuning embedding similarity threshold. Too low (0.6) = noisy. Too high (0.9) = misses wrappers. LLM verification acts as precision filter, so embedding stage can be permissive (0.7).

**Dependency**: Requires Embedder and LLMExplainer ports. Both exist.

---

## Feature 2: Return-Value Taint Propagation

### The Problem

A healthcare SaaS company runs commit0 security scan on their patient portal. Zero SQL injection findings. The security team is relieved.

Three weeks later, a penetration tester finds a critical SQL injection:

```go
func (h *Handler) GetPatient(c echo.Context) error {
    id := c.Param("id")                          // SOURCE: user input
    patient := h.service.FindPatient(id)          // passes tainted id
    return c.JSON(200, patient)
}

func (s *Service) FindPatient(id string) *Patient {
    query := s.buildQuery(id)                     // returns tainted query string
    return s.repo.Execute(query)                  // SINK: executes tainted query
}

func (s *Service) buildQuery(id string) string {
    return fmt.Sprintf("SELECT * FROM patients WHERE id = '%s'", id)
}
```

commit0 missed this because:
1. The mutation detector only fires on `x = func(x)` patterns — `query := s.buildQuery(id)` is `x = func(y)`, a different variable.
2. The taint from `id` flows THROUGH `buildQuery`'s return value into `query`, but return-value flows aren't tracked.
3. Even if `query` were tracked, `s.repo.Execute` isn't in the hardcoded sink list (Feature 1 fixes this).

This is the most common pattern in real-world injection vulnerabilities: tainted data passes through helper functions via return values before reaching a sink.

### Why Semgrep Cannot Solve This

Semgrep sees each function in isolation. It can match `fmt.Sprintf("SELECT...%s", $ARG)` inside `buildQuery`. But it cannot:

1. **Know that `$ARG` came from `c.Param("id")`.** That information is 2 function calls away. Semgrep has no call graph.

2. **Know that the return value of `buildQuery` reaches `repo.Execute`.** The return value is assigned to a local variable `query` in `FindPatient`, then passed as an argument to `Execute`. Semgrep can't follow variables through function calls.

3. **Chain the taint**: `c.Param("id")` → `FindPatient(id)` → `buildQuery(id)` → `return fmt.Sprintf(...)` → `query` → `repo.Execute(query)`. This 5-link chain crosses 3 function boundaries. Semgrep operates within one function. Even Semgrep Pro's inter-file analysis handles limited patterns (imports, method calls) — not arbitrary return-value flows across call boundaries.

CodeQL handles this with function summaries and library models. But each function summary must be manually written or inferred by expensive whole-program analysis. commit0's approach is different: the code graph already connects these functions. We just need to add return-value edges.

### The Insight

commit0's tree-sitter extractor already visits every `call_expression` and every `assignment_statement`. The missing piece is connecting them: when a call's return value is assigned to a variable, and that variable is later passed to another call, those two calls are linked through the variable.

```
id := c.Param("id")           → edge: Handler.GetPatient -[data_flow]→ Service.FindPatient (arg: id)
query := s.buildQuery(id)     → MISSING: no edge for return value assignment
s.repo.Execute(query)         → edge: Service.FindPatient -[data_flow]→ Repository.Execute (arg: query)
```

The fix: within each function body, track which local variables hold return values from calls. When those variables are passed as arguments to subsequent calls, emit a `data_flow` edge connecting the producing call to the consuming call. This is **intra-procedural** analysis — scan each function once, no fixed-point iteration.

### Solution Design

**New edge metadata:**

```go
meta["flow_type"] = "return_value"   // vs current "argument"
meta["via_var"]   = "query"          // the local variable that carries the taint
```

**Tree-sitter extraction** (`extractGoReturnFlows`):

```go
// Within a function body:
// Pass 1: Find assignments where RHS is a call expression
//   "query := s.buildQuery(id)" → varMap["query"] = {callee: "buildQuery", args: ["id"]}
//
// Pass 2: Find call expressions where an argument references a tracked variable
//   "s.repo.Execute(query)" → "query" is in varMap
//
// Emit: edge from "buildQuery" to "Execute" with flow_type: "return_value"
```

**Patterns detected:**

```go
// Pattern 1: Direct return-value → argument
result := process(userInput)
db.Query(result)                     // edge: process → db.Query via "result"

// Pattern 2: Multi-step chain
raw := c.Param("id")
trimmed := strings.TrimSpace(raw)
query := fmt.Sprintf("SELECT...%s", trimmed)
db.Exec(query)                       // chain: c.Param → TrimSpace → Sprintf → db.Exec

// Pattern 3: Struct field from return value
user := service.GetUser(id)
response := map[string]string{"email": user.Email}
c.JSON(200, response)               // edge: GetUser → c.JSON via "user"
```

**Key constraint**: Single-pass, O(n) in function body size. Only track variables that are subsequently used as arguments to other calls. Dead variables create no edges.

### What Semgrep Cannot Replicate

| Taint Pattern | Semgrep | commit0 (with F2) |
|---|---|---|
| `sink(source())` — direct | Yes | Yes |
| `x := source(); sink(x)` — same function | Yes (dataflow mode) | Yes |
| `x := helper(source()); sink(x)` — return value | No | **Yes** |
| `x := a(source()); y := b(x); sink(y)` — chained | No | **Yes** |
| `x := source(); ... 50 lines later ... sink(x)` | Partial | **Yes** (graph-based) |
| Cross-function: `A(input) → B(input) → C → sink` | No | **Yes** (data_flow edges chain) |

The fundamental difference: Semgrep analyzes one function (or one file) at a time. commit0 analyzes the **graph** — taint chains that cross any number of function boundaries are just paths in the graph.

### Success Criteria

- **Edge coverage**: Return-value edges account for 30-50% of all `data_flow` edges
- **True positive lift**: 2-3x more injection paths found on OWASP benchmarks
- **Performance**: Indexing time increases <10%
- **Chain length**: Average taint path increases from ~2 hops to ~4 hops

### Engineering Complexity

**Core work**: `extractGoReturnFlows()` in `golang.go`, equivalents for Python/TypeScript/JavaScript.

**Hard part**: Go's multiple return values (`result, err := func()`), variable shadowing in nested scopes, destructuring in JS/TS. Python is simplest (single return).

**Risk**: Over-tainting. Mitigation: only track variables subsequently used as call arguments.

---

## Feature 3: API Surface Discovery, Exposure Mapping & Spec Rendering

### The Problem

A B2B SaaS company has 140 API endpoints spread across 30 Go files. Their API documentation is a Notion page last updated 6 months ago. It lists 95 endpoints — 45 are missing, 12 are wrong, 8 describe endpoints that no longer exist.

The security team asks three questions nobody can answer quickly:

**Question 1: "What data can an unauthenticated user access?"**

Nobody knows which endpoints lack auth middleware. The route registrations are spread across `server.go`, `routes.go`, and 5 module-specific route files. Middleware is applied at different levels — global, group, per-route. A developer added a new endpoint last sprint and forgot the auth middleware. It's serving production traffic without authentication.

**Question 2: "If a user sends a crafted `id` parameter, what database tables can they reach?"**

The `id` parameter enters through `c.Param("id")` in a handler. The handler calls `service.GetItem(id)`. The service calls `repo.FindByID(id)`. The repo builds a query. But `id` also gets passed to `auditLog.Record(id)` which writes to a different table. And `cache.Invalidate(id)` which hits Redis. The blast radius of a single input parameter spans 3 data stores — but you'd need to read 8 files to trace this manually.

**Question 3: "What PII fields does our API return in responses?"**

The `GET /api/v1/users/:id` endpoint returns a `User` struct. That struct has 15 fields. The handler does `c.JSON(200, user)` — serializing everything, including `SSN`, `DateOfBirth`, and `InternalScore`. The team thought they had a DTO layer filtering sensitive fields, but three endpoints bypass it and return raw domain objects. Nobody knows which three.

These are not exotic questions. They are **table-stakes for any SOC 2 / HIPAA / PCI audit**. And the only way to answer them today is manual code review.

### Why Semgrep Cannot Solve This

Semgrep can write rules. It can match `e.GET($PATH, $HANDLER)` and extract route registrations. It can match `c.Param($NAME)` and find parameter bindings. It can match `c.JSON($STATUS, $DATA)` and find response sites.

But Semgrep **cannot connect them**:

1. **Route → handler → service → database is a 4-hop graph traversal.** Semgrep sees individual nodes. It can tell you "there is a route at `/users/:id`" and separately "there is a `db.Query` call in `repo.go`". It cannot tell you that the route's handler eventually reaches that specific `db.Query` through 3 intermediate function calls. That requires a call graph.

2. **Response type inference requires cross-file type resolution.** `c.JSON(200, user)` — what is `user`? It's a variable of type `*User`, defined in `models.go`, with fields tagged `json:"email"`, `json:"ssn"`. Semgrep can match the `c.JSON` call, but it can't resolve the type of `user`, find the struct definition, and enumerate its JSON-serialized fields. That requires type analysis + struct field extraction.

3. **Middleware chain analysis requires control flow through higher-order functions.** `group.Use(authMiddleware)` applies middleware to all routes in the group. Which routes are in the group? That depends on which `group.GET(...)` calls come after. Semgrep can match the `Use` call but can't determine its scope. And route-level middleware (`e.GET(path, handler, mw1, mw2)`) passes functions as arguments — Semgrep can extract the names but can't verify they actually perform authentication.

4. **Generating an API spec requires synthesizing information from 4 locations**: route registration (method + path), handler function (parameter bindings), request struct definition (field names + types + validation), and response struct definition (field names + JSON tags). These are in different files, connected by function calls and type references. Semgrep operates within a single pattern context.

**The structural limitation**: Semgrep is a search tool. It finds things. It doesn't connect them. API surface mapping is fundamentally a graph problem — connecting routes to handlers to services to databases to response types. commit0 has the graph. Semgrep doesn't.

### The Insight

commit0 already knows that `Handler.GetUser` calls `Service.FindUser` calls `Repo.Get` calls `db.Query`. The call graph is there. What's missing is the **entry point**: commit0 doesn't know that `Handler.GetUser` is bound to `GET /api/v1/users/:id` and receives input through `c.Param("id")`.

Adding API surface awareness to the tree-sitter extractors transforms commit0 from a code analysis tool into an **application analysis tool**. The difference:

- **Code analysis**: "function A calls function B" (what Semgrep does, what commit0 does today)
- **Application analysis**: "HTTP GET /users/:id receives user-controlled `id` parameter → handler validates it → service looks up user → repo queries `users` table with `id` → handler returns User struct with fields [name, email, ssn, phone] → `ssn` is PII that should not be exposed"

The second statement requires connecting HTTP semantics (routes, parameters, response types) to code semantics (function calls, data flow, struct fields). commit0's graph is the natural place to store and query this.

### Solution Design

#### 3.1 New Domain Types

```go
// pkg/types/api.go

type APIEndpoint struct {
    Method      string            // "GET", "POST", "DELETE"
    Path        string            // "/api/v1/users/:id"
    HandlerFunc string            // qualified name: "internal/api.UserHandler.GetUser"
    Middleware  []string          // ["authMiddleware", "rateLimitMiddleware"]
    Group       string            // route group prefix: "/api/v1"
    
    // Extracted from handler body analysis
    RequestParams  []APIParam     // path params, query params, headers
    RequestBody    *APISchema     // bound struct type (from c.Bind)
    ResponseBodies []APIResponse  // status code → response type mapping
    
    // Security annotations (populated by Feature 1 catalog)
    AuthRequired   bool           // has auth middleware in chain
    AuthMethod     string         // "jwt", "api-key", "session", "none"
    RateLimit      bool           // has rate limiting middleware
}

type APIParam struct {
    Name     string   // "id"
    In       string   // "path", "query", "header", "cookie"
    Type     string   // "string", "int" (inferred from usage)
    Required bool     // path params always required; query params based on validation
    TaintedSinks []string // qualified names of sinks this param reaches (from taint analysis)
}

type APISchema struct {
    StructName string         // "CreateUserRequest"
    Fields     []APIField
}

type APIField struct {
    Name       string   // Go field name: "Email"
    JSONName   string   // from json tag: "email"
    Type       string   // "string", "int", "[]string"
    Required   bool     // from validate tag or manual check
    Sensitive  bool     // PII/secret classification
    Category   string   // "pii", "credential", "internal", "public"
}

type APIResponse struct {
    StatusCode int
    Schema     *APISchema  // resolved struct type
    IsStream   bool        // SSE or chunked response
}
```

#### 3.2 Tree-Sitter API Extraction

**New extractor functions per framework:**

**Go / Echo:**
```go
// extractEchoRoutes detects: e.GET("/path", handler, middleware...)
//                            group.POST("/path", handler)
//                            group := e.Group("/prefix", middleware...)
func extractEchoRoutes(root *sitter.Node, src []byte, filePath string) []APIEndpoint

// extractEchoBindings detects: c.Param("name"), c.QueryParam("name"),
//                               c.Bind(&req), c.JSON(status, data)
func extractEchoBindings(fnNode *sitter.Node, src []byte) ([]APIParam, *APISchema, []APIResponse)
```

**Python / Flask & FastAPI:**
```python
# extractFlaskRoutes detects: @app.route("/path", methods=["GET"])
#                              @app.get("/path")
#                              @blueprint.post("/path")
func extractFlaskRoutes(root *sitter.Node, src []byte, filePath string) []APIEndpoint

# extractFastAPIRoutes detects: @app.get("/path", response_model=User)
#                                @router.post("/path")
#                                Depends() for dependency injection / auth
func extractFastAPIRoutes(root *sitter.Node, src []byte, filePath string) []APIEndpoint
```

**TypeScript / Express & NestJS:**
```typescript
// extractExpressRoutes detects: app.get("/path", handler)
//                                router.post("/path", middleware, handler)
//                                app.use("/prefix", router)
func extractExpressRoutes(root *sitter.Node, src []byte, filePath string) []APIEndpoint

// extractNestJSRoutes detects: @Get("/path"), @Post("/path")
//                               @Controller("/prefix")
//                               @UseGuards(AuthGuard)
//                               @Body() dto: CreateUserDto
func extractNestJSRoutes(root *sitter.Node, src []byte, filePath string) []APIEndpoint
```

**Route detection strategy**: Match call expressions where the function name is an HTTP method (`GET`, `POST`, `PUT`, `DELETE`, `PATCH`) on a known router object. For decorator-based frameworks (Flask, FastAPI, NestJS), match decorator expressions with HTTP method names. Extract the path string and handler function reference.

#### 3.3 Request/Response Type Resolution

After extracting routes and handler references, resolve the data shapes:

**Request resolution:**
1. Find `c.Bind(&req)` calls in the handler body → extract type of `req`
2. Look up struct definition in the code graph (existing `NodeClass` for structs)
3. Extract struct fields with JSON tags, validation tags, type info
4. For `c.Param("name")` / `c.QueryParam("name")` → create APIParam entries

**Response resolution:**
1. Find `c.JSON(status, data)` calls → determine type of `data`
2. If `data` is a local variable, trace its assignment back to the producing call
3. Resolve the struct type → extract fields with JSON tags
4. Flag fields that are PII (common patterns: `email`, `phone`, `ssn`, `dob`, `password`, `token`, `secret`, `credit_card`) or carry security classifications from Feature 1 catalog

**Struct field sensitivity heuristic + LLM verification:**

```go
// Heuristic: field names matching PII patterns
piiPatterns := []string{
    "ssn", "social_security", "tax_id",
    "password", "passwd", "secret", "token", "api_key",
    "credit_card", "card_number", "cvv",
    "date_of_birth", "dob", "birth_date",
    "phone", "mobile", "cell",
    "address", "street", "zip_code", "postal",
}

// LLM verification for ambiguous fields:
// "Is the field 'score' in UserProfile a sensitive internal metric
//  or a public-facing rating? Here is the struct definition and
//  the functions that read/write this field."
```

#### 3.4 Exposure Map Construction

The **API Exposure Map** is the union of three analyses:

**Layer 1 — API surface graph:**
```
Route GET /api/v1/users/:id
  ├── Handler: UserHandler.GetUser
  │   ├── Middleware: [authMiddleware] → auth: JWT
  │   ├── Params: [id: path, string, required]
  │   └── Response: 200 → User{name, email, phone, ssn, internal_score}
  │
  ├── Service: UserService.FindByID
  │   └── Calls: UserRepo.Get, AuditLog.Record, Cache.Get
  │
  └── Data stores: [users (postgres), audit_log (postgres), cache (redis)]
```

**Layer 2 — Input taint flow (from Feature 2):**
```
Param "id" (user-controlled)
  → UserHandler.GetUser(c) extracts c.Param("id")
  → UserService.FindByID(id)
  → UserRepo.Get(id) → db.Query("SELECT * FROM users WHERE id = $1", id)
                        ✓ parameterized — safe
  → AuditLog.Record(id) → db.Exec("INSERT INTO audit_log (entity_id) VALUES ($1)", id)
                           ✓ parameterized — safe
  → Cache.Get(id) → redis.Get("user:" + id)
                     ⚠ string concatenation — potential cache key injection
```

**Layer 3 — Output exposure analysis:**
```
Response User struct:
  ✓ name        (string, public)
  ✓ email       (string, PII — but expected in user profile)
  ⚠ phone       (string, PII — should this be in the default response?)
  ✗ ssn         (string, CRITICAL PII — should NEVER be in API response)
  ✗ internal_score (float64, internal metric — data leak)
```

#### 3.5 API Spec Rendering

From the exposure map, generate an OpenAPI 3.0 spec:

```yaml
openapi: "3.0.0"
info:
  title: "my-service API"
  description: "Auto-generated by commit0 from source code analysis"
  version: "1.0.0"
  x-commit0-generated: true
  x-commit0-commit: "abc123"

paths:
  /api/v1/users/{id}:
    get:
      operationId: "UserHandler.GetUser"
      summary: "Retrieve user by ID"
      description: |
        Auto-generated from internal/api/handlers.go:42
        Service: UserService.FindByID → UserRepo.Get
        Data sources: users (postgres), audit_log (postgres), cache (redis)
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
          x-commit0-taint:
            reaches-sinks:
              - "UserRepo.Get → db.Query (parameterized, safe)"
              - "Cache.Get → redis.Get (string concat, warning)"
      responses:
        "200":
          description: "User found"
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/User"
          x-commit0-exposure:
            safe-fields: ["name", "email"]
            pii-fields: ["phone"]
            critical-fields: ["ssn", "internal_score"]
            recommendation: "Create a UserResponse DTO excluding ssn and internal_score"
      security:
        - jwt: []
      x-commit0-middleware: ["authMiddleware"]
      x-commit0-handler: "internal/api.UserHandler.GetUser"
      x-commit0-source: "internal/adapters/http/server.go:67"

  /api/v1/admin/search:
    get:
      operationId: "AdminHandler.Search"
      parameters:
        - name: q
          in: query
          schema:
            type: string
          x-commit0-taint:
            reaches-sinks:
              - "AdminRepo.RawSearch → db.Query (NOT parameterized, CRITICAL)"
      security: []
      x-commit0-middleware: []
      x-commit0-security-issues:
        - severity: critical
          issue: "No authentication middleware"
          fix: "Add authMiddleware and requireAdmin"
        - severity: critical
          issue: "SQL injection via 'q' parameter"
          fix: "Use parameterized query in AdminRepo.RawSearch"

components:
  schemas:
    User:
      type: object
      properties:
        name:
          type: string
        email:
          type: string
          x-commit0-sensitivity: pii
        phone:
          type: string
          x-commit0-sensitivity: pii
        ssn:
          type: string
          x-commit0-sensitivity: critical-pii
        internal_score:
          type: number
          x-commit0-sensitivity: internal
      x-commit0-source: "internal/models/user.go:12"
```

### User Experience

```bash
# Discover API surface and generate spec
commit0 api discover

# Output:
# API Surface Discovery for my-service
# ═══════════════════════════════════════════════════════════════
#
# Discovered 47 endpoints across 12 files
#
#  Method  Path                        Auth    Params              Response Fields    Issues
#  ──────  ──────────────────────────  ──────  ──────────────────  ─────────────────  ──────
#  GET     /api/v1/users/:id           JWT     id (path)           User (15 fields)   PII exposed: ssn, phone
#  POST    /api/v1/users               JWT     body: CreateUser    User (15 fields)   PII exposed: ssn
#  GET     /api/v1/admin/search        NONE    q (query)           []User             NO AUTH, SQLi via q
#  DELETE  /api/v1/users/:id           JWT     id (path)           User               -
#  POST    /api/v1/payments            JWT     body: PaymentReq    Payment            PII: card_number in response
#  ...
#
# Security Summary:
#   3 endpoints without authentication
#   2 endpoints with SQL injection paths
#   7 endpoints exposing PII in responses
#   1 endpoint with unparameterized cache key

# Generate OpenAPI spec
commit0 api spec --format yaml > openapi.yaml
commit0 api spec --format json > openapi.json

# Show exposure map for a specific endpoint
commit0 api expose "GET /api/v1/users/:id"

# Output:
# Exposure Map: GET /api/v1/users/:id
# ═══════════════════════════════════════════════════════════════
#
# Entry: internal/api.UserHandler.GetUser (handlers.go:42)
# Auth: JWT via authMiddleware
#
# Input Flow (param "id"):
#   c.Param("id")
#   → UserService.FindByID(id)
#   → UserRepo.Get(id) → db.Query($1, id)          ✓ parameterized
#   → AuditLog.Record(id) → db.Exec($1, id)        ✓ parameterized
#   → Cache.Get("user:" + id)                       ⚠ string concat
#
# Output Exposure:
#   200 → User struct (internal/models/user.go:12)
#   ├── name           string    public
#   ├── email          string    PII
#   ├── phone          string    PII         ⚠ should this be in default response?
#   ├── ssn            string    CRITICAL    ✗ NEVER expose in API
#   ├── internal_score float64   internal    ✗ data leak
#   └── ... 10 more fields (all public)
#
# Data Stores Reached:
#   • users table (postgres) via UserRepo.Get — READ
#   • audit_log table (postgres) via AuditLog.Record — WRITE
#   • user:{id} key (redis) via Cache.Get — READ

# Diff API surface between commits / branches
commit0 api diff main..feature-branch

# Output:
#  + POST /api/v1/admin/search     ← NEW, no auth middleware
#  ~ GET  /api/v1/users/:id        ← response now includes "internal_score"
#  - GET  /api/v1/legacy/users     ← endpoint removed
```

### What Semgrep Cannot Replicate

This is the definitive comparison. Every row represents a capability that requires connecting multiple code locations through the graph:

| Analysis | Semgrep | commit0 | Why |
|---|---|---|---|
| List all API endpoints | Partial (pattern match route registrations) | **Complete** (route extraction + handler resolution) | Semgrep finds `e.GET` calls but can't resolve handler references or group prefixes |
| Map endpoint → database tables | **No** | **Yes** | Requires 3-4 hop call graph traversal: handler → service → repo → db |
| Map input param → all sinks it reaches | **No** | **Yes** | Requires cross-function taint flow (Feature 2) |
| Enumerate response fields with types | **No** | **Yes** | Requires type resolution: `c.JSON(200, user)` → User struct → field list |
| Detect PII in API responses | **No** | **Yes** | Requires response type resolution + field sensitivity classification |
| Detect missing auth middleware | Partial (match `Use(auth)`) | **Yes** | Requires route → middleware chain → auth verification |
| Generate accurate OpenAPI spec | **No** | **Yes** | Requires synthesizing route + params + request type + response type + auth |
| Diff API surface between commits | **No** | **Yes** | Requires temporal graph + API surface extraction at two points |
| Show blast radius of handler change | **No** | **Yes** | Requires reverse graph traversal from the handler |

**The structural gap**: Semgrep can find *individual facts* ("there is a route here", "there is a JSON response there", "there is a db.Query there"). commit0 can *connect them* into a coherent API exposure map because it has the code graph. An API surface is not a collection of patterns — it's a graph of routes → handlers → services → data stores → response types. Only a graph tool can analyze it as such.

### Success Criteria

- **Endpoint discovery**: >95% of actual endpoints discovered (vs. manual audit)
- **Response field accuracy**: >90% of response struct fields correctly enumerated
- **PII detection**: >80% of actual PII fields flagged (measured against manual annotation)
- **Spec accuracy**: Generated OpenAPI spec can be loaded by Swagger UI and correctly describes all endpoints, params, and response shapes
- **Auth gap detection**: 100% of endpoints without auth middleware are flagged
- **Taint coverage**: For each input parameter, the complete set of sinks it reaches is identified (with Feature 2 enabled)

### Engineering Complexity

**Core work**: New `APIEndpoint` domain types, framework-specific tree-sitter extractors (Echo, Flask, Express), response type resolver, PII heuristic + LLM classification, OpenAPI spec generator, CLI commands.

**Hard parts**:

1. **Framework detection**: Which framework is this codebase using? Infer from imports (`labstack/echo`, `gin-gonic/gin`, `flask`, `express`). Route extraction patterns differ per framework.

2. **Type resolution for responses**: `c.JSON(200, user)` — resolving `user` to its struct type requires tracking the variable to its declaration and following the type. For simple cases (local variable with explicit type), tree-sitter can handle it. For complex cases (returned from a function, type assertion, interface), the resolver needs the existing type resolution infrastructure.

3. **Middleware scope**: `group := e.Group("/api", authMW)` — all routes added to `group` inherit `authMW`. Tracking which routes belong to which group requires matching variable references across the file.

**Dependency**: Feature 2 (Return-Value Taint) for accurate input→sink tracing. Feature 1 (Security Catalog) for sink classification and PII detection. Without these, the exposure map shows routes and response types but can't assess taint flows or classify sensitivity.

---

## Feature 4: Graph-Powered Dependency Risk Assessment

### The Problem

Monday morning. A critical CVE drops for `golang.org/x/text` — a Unicode normalization bug enabling homograph attacks in domain validation. Affects versions before 0.14.0.

The CTO asks: "Are we affected?"

The team checks `go.mod`: yes, `golang.org/x/text` v0.13.0. Dependabot fires. Snyk fires. Renovate opens a PR. Three tools, same message: "You have a vulnerable version. Upgrade."

But that's the **wrong answer to the wrong question**. The right questions:

1. **Do we use the vulnerable function?** The CVE is in `unicode/norm.NFC.String()`. The team imports `golang.org/x/text/language` for locale detection — a different subpackage that isn't affected.

2. **If we use it, does user-controlled data reach it?** If only hardcoded strings pass through normalization, there's no exploitable path.

3. **If it's exploitable, which API endpoints are affected?** (Feature 3's exposure map answers this.)

The team spends a day investigating. They're not affected. They upgrade anyway because the tooling can't tell them it's safe.

Across 200 microservices, this happens every week. Dependabot opens PRs. Snyk opens tickets. Each one requires a human to answer "are we actually affected?" That's 10-20 hours per week of security engineering time spent on triage that a graph tool could automate.

### Why Semgrep Cannot Solve This

Semgrep doesn't know about dependencies at all. It matches patterns in source code. `go.mod` is not Go source code — it's a dependency manifest. Semgrep can't parse it, can't query CVE databases, can't trace import paths to vulnerable functions.

**Dependabot/Snyk/Renovate** know about dependencies but don't know about code:

1. **They can't determine reachability.** "You import `golang.org/x/text`" is true but insufficient. Which subpackage? Which function? The import graph from `go.mod` to the specific vulnerable function is 2-3 levels deep (module → package → function). These tools stop at the module level.

2. **They can't determine exposure.** Even if the vulnerable function is called, is the argument user-controlled? These tools have no taint analysis. They can't follow data from an HTTP parameter through 5 function calls to the vulnerable API.

3. **They can't show blast radius.** If exploited, what data is at risk? What endpoints are affected? How many users are impacted? Dependency scanners report "vulnerability exists." They don't report "this vulnerability can be triggered through GET /api/v1/validate and affects the authentication flow for all users."

### The Insight

commit0 already indexes `go.mod` via the gomod tree-sitter extractor, creating `NodeModule` entries. It already has `imports` edges connecting files to modules. It already has `calls` edges connecting functions to functions. It has `BlastService` for reverse traversal. With Feature 2, it has return-value taint tracking. With Feature 3, it has the API exposure map.

The chain: `CVE affects function F in module M` → `which files import M?` (imports edges) → `which functions call F?` (calls edges) → `does user input reach those calls?` (taint flow) → `which API endpoints are affected?` (exposure map) → `what's the blast radius?` (reverse traversal)

Every link is a query commit0 can already execute. The feature is connecting CVE databases to graph queries.

### Solution Design

**CVE data source**: OSV (osv.dev) — free, open, covers Go/Python/npm. API: `GET https://api.osv.dev/v1/query` with package + version.

**Risk assessment pipeline:**

```
Step 1: IDENTIFY
  Parse go.mod / package.json / requirements.txt
  Query OSV for each dependency + version
  → List of CVEs with affected version ranges and (when available) affected functions

Step 2: REACHABILITY
  For each CVE with known affected function:
    Search code graph for call edges to that function
  For CVE without affected function:
    Find all imports of the affected module
    Find all functions that transitively call into the module
  → Reachable (with call chain) or Unreachable

Step 3: EXPOSURE
  For each reachable CVE:
    Trace backward from the vulnerable function call
    Check: does any taint source reach it? (Feature 2 taint analysis)
    Check: which API endpoints expose it? (Feature 3 exposure map)
  → Exposed (with taint path + endpoint list) or Internal-only

Step 4: BLAST RADIUS
  For each exposed CVE:
    Run BlastService from the vulnerable call site
    Cross-reference with API exposure map
  → Affected endpoints, data stores, and downstream services

Step 5: PRIORITIZE
  Score = reachability × exposure × blast_radius × CVSS_base
  → Prioritized risk report
```

### User Experience

```bash
commit0 security deps

# Output:
# Dependency Risk Assessment for my-service
# ═══════════════════════════════════════════════════════════════
#
# CRITICAL — EXPOSED VIA API
#   CVE-2024-XXXXX: golang.org/x/text v0.13.0 (Unicode normalization bypass)
#   CVSS: 9.1 | Affected function: unicode/norm.NFC.String
#   Reachable: YES — internal/auth/validator.go:42 calls norm.NFC.String()
#   Exposed: YES — user input from LoginHandler reaches norm.NFC.String()
#   Affected endpoints:
#     POST /api/v1/auth/login    ← user-controlled "username" parameter
#     POST /api/v1/auth/register ← user-controlled "email" parameter
#   Blast radius: authentication flow for all users
#   Risk score: 0.95
#
# LOW — NOT REACHABLE
#   CVE-2024-ZZZZZ: golang.org/x/crypto v0.17.0 (RSA padding oracle)
#   CVSS: 5.9 | Affected function: rsa.DecryptPKCS1v15
#   Reachable: NO — only x/crypto/ssh imported, RSA functions not called
#   Risk score: 0.05
#
# Summary: 1 critical (exposed via API), 1 low (unreachable)
# Actual risk: 1 of 2 CVEs requires action
# Noise eliminated: 50%

# CI mode
commit0 security deps --ci --only-exposed --min-severity high
```

### What Semgrep Cannot Replicate

| Analysis | Dependabot/Snyk | Semgrep | commit0 |
|---|---|---|---|
| "You have vulnerable version" | Yes | No | Yes |
| "You call the vulnerable function" | No | No | **Yes** (call graph) |
| "User input reaches the vulnerable function" | No | No | **Yes** (taint flow) |
| "These API endpoints are affected" | No | No | **Yes** (exposure map) |
| "This is the blast radius if exploited" | No | No | **Yes** (reverse traversal) |
| Noise reduction (unreachable CVEs) | No | No | **Yes** (50-70% noise eliminated) |

The difference is categorical. Dependabot says "upgrade." commit0 says "this CVE can be triggered through your login endpoint by a crafted username, affecting authentication for all users — upgrade immediately" or "this CVE is in a function you don't call — upgrade when convenient."

### Success Criteria

- **Noise reduction**: >50% of flagged CVEs downgraded to low/info because vulnerable code path is unreachable
- **Zero missed exposures**: If user input reaches a vulnerable function, it MUST be flagged
- **Triage time**: Average time-to-decision per CVE drops from ~30 minutes to ~2 minutes
- **API endpoint mapping**: Every exposed CVE includes the specific endpoints affected (from Feature 3)

### Engineering Complexity

**Core work**: OSV API client, reachability analysis (graph traversal from vulnerable function), exposure check (reverse taint from vulnerable call site), risk scoring, CLI output.

**Hard part**: Mapping CVE-affected functions to the code graph. OSV sometimes provides function names, sometimes only the package. When only the package is known, fall back to "any import of this package = reachable" — noisier but safe.

**Dependency**: Features 1+2+3. The full pipeline (catalog + taint + API surface) makes the risk assessment precise. Without them, it degrades to reachability-only (still better than Dependabot, but less actionable).

---

## Build Order & Dependency Graph

```
Feature 1: Security Catalog ─────────────────┐
  (seed + embedding + LLM classification)    │
                                              │
Feature 2: Return-Value Taint ──────┐        │
  (intra-procedural flow edges)     │        │
                                    ▼        ▼
                              Feature 3: API Surface Discovery
                                (routes + exposure map + spec)
                                              │
                                              ▼
                              Feature 4: Dependency Risk
                                (CVE + reachability + exposure)
```

| Order | Feature | Depends On | What It Unlocks |
|-------|---------|-----------|-----------------|
| 1a | Security Catalog | — | Precise sink/source/sanitizer classification for all downstream features |
| 1b | Return-Value Taint | — | Cross-function taint chains for exposure analysis |
| 2 | API Surface Discovery | F1 + F2 | Endpoint-level exposure maps, OpenAPI spec, PII detection |
| 3 | Dependency Risk | F1 + F2 + F3 | CVE → reachability → exposure → blast radius per endpoint |

Features 1 and 2 can be built in parallel — they have no mutual dependency. Feature 3 requires both. Feature 4 requires the full stack.

### The Compound Effect

Each feature is useful alone. Together they create something no other tool offers:

```
commit0 api discover + commit0 security deps

= "Your API has 47 endpoints. 3 lack authentication.
   2 have SQL injection paths. 7 expose PII in responses.
   1 endpoint (POST /api/v1/auth/login) is affected by CVE-2024-XXXXX
   because user-controlled 'username' reaches the vulnerable
   unicode/norm.NFC.String() function through 4 intermediate calls.
   Here is the OpenAPI spec with all security annotations.
   Here is the precise diff from last week."
```

Semgrep can find individual patterns. Dependabot can flag versions. Neither can tell this story. commit0 can, because it has the graph.
