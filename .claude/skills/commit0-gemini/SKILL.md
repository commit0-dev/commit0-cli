# commit0 Gemini Integration Skill

Use this skill when writing Gemini-related code for commit0: the Embedder adapter, the LLMExplainer adapter, context building, embedding batching, and the Go SDK (`google.golang.org/genai`).

---

## Overview

commit0 uses two Gemini models:

| Model | Purpose | Adapter |
|---|---|---|
| `gemini-embedding-2-preview` | Text/code/image → 3072-dim vector | `internal/adapters/gemini/embedder.go` |
| `gemini-2.0-flash` | Code context → streaming NL explanation | `internal/adapters/gemini/explainer.go` |

Both use the unified Go SDK: `google.golang.org/genai`

---

## Gemini Embedding 2 Specifics

### Task Instruction Format

Gemini Embedding 2 uses **natural language instruction prefixes** (not enum-based `task_type`):

| When | Prefix | Example |
|---|---|---|
| Index time (document) | `task: search result \| query:` | `task: search result \| query: [FUNCTION] pkg.Handler.ServeHTTP ...` |
| Query time (query) | `task: search query \| query:` | `task: search query \| query: where is JWT validation?` |
| Clustering/classification | `task: classification \| query:` | `task: classification \| query: {content}` |

This asymmetry between `search result` and `search query` is critical for retrieval precision.

### Model Specs

- Model ID: `gemini-embedding-2-preview`
- Output dimensions: 128–3,072 (Matryoshka)
- commit0 uses: **3072** (max fidelity)
- Parameter: `OutputDimensionality` controls dim at call time
- Max batch: 100 inputs per request
- Multimodal: text + code + images + PDFs in one vector space

### Go SDK Call

```go
import "google.golang.org/genai"

result, err := client.Models.EmbedContent(ctx, "gemini-embedding-2-preview",
    &genai.EmbedContentRequest{
        Contents: []*genai.Content{{
            Parts: []genai.Part{
                genai.Text("task: search result | query: func ValidateJWT(token string) error {...}"),
            },
        }},
        OutputDimensionality: ptr(3072),
    },
)

// result.Embeddings[0].Values → []float32 (3072-dim)
```

### Multimodal Embedding (Images)

```go
// Up to 6 images per request
result, err := client.Models.EmbedContent(ctx, "gemini-embedding-2-preview",
    &genai.EmbedContentRequest{
        Contents: []*genai.Content{{
            Parts: []genai.Part{
                genai.Text("task: search result | query: " + surroundingText),
                genai.Blob{MIMEType: "image/png", Data: diagramBytes},
            },
        }},
        OutputDimensionality: ptr(3072),
    },
)
```

---

## Context Builder (internal/app/context_builder.go)

The context builder constructs the embedding input text for each node, prepending the Gemini task instruction prefix.

### Function Context Template (index time)

```
task: search result | query: [FUNCTION] {qualified_name}
Language: {language}  File: {file_path}:{start_line}-{end_line}
Signature: {signature}
Calls: {callee_1}, {callee_2}, ...
Called by: {caller_1}, {caller_2}, ...
Doc: {docstring}
---
{code_body}
```

### Class Context Template (index time)

```
task: search result | query: [CLASS] {qualified_name}
Language: {language}  File: {file_path}
Inherits: {base_1}, {base_2}
Methods: {method_1}, {method_2}, ...
Doc: {docstring}
---
{class_body_first_512_tokens}
```

### File Context Template (index time)

```
task: search result | query: [FILE] {relative_path}
Language: {language}
Exports: {top_level_symbols}
Imports: {imported_modules}
---
{file_first_1024_tokens}
```

### Query Context Template (query time)

```
task: search query | query: {user_natural_language_question}
```

### Implementation

```go
type ContextBuilder struct {
    maxBodyTokens int // Default: 8192 (Gemini limit)
}

func (b *ContextBuilder) BuildContext(node *types.CodeNode) string {
    var sb strings.Builder

    switch node.Kind {
    case types.NodeFunction:
        sb.WriteString(fmt.Sprintf("task: search result | query: [FUNCTION] %s\n", node.Qualified))
        sb.WriteString(fmt.Sprintf("Language: %s  File: %s:%d-%d\n", node.Language, node.FilePath, node.StartLine, node.EndLine))
        sb.WriteString(fmt.Sprintf("Signature: %s\n", node.Signature))
        if node.Docstring != "" {
            sb.WriteString(fmt.Sprintf("Doc: %s\n", node.Docstring))
        }
        sb.WriteString("---\n")
        sb.WriteString(truncateTokens(node.Body, b.maxBodyTokens))

    case types.NodeClass:
        sb.WriteString(fmt.Sprintf("task: search result | query: [CLASS] %s\n", node.Qualified))
        // ... similar pattern

    case types.NodeFile:
        sb.WriteString(fmt.Sprintf("task: search result | query: [FILE] %s\n", node.FilePath))
        // ... similar pattern
    }

    return sb.String()
}

func (b *ContextBuilder) BuildQueryContext(question string) string {
    return fmt.Sprintf("task: search query | query: %s", question)
}
```

---

## Embed Batcher (internal/app/embed_batcher.go)

Accumulates embedding inputs and flushes in batches of 100 (Gemini API limit).

```go
type EmbedBatcher struct {
    embedder  domain.Embedder
    builder   *ContextBuilder
    batchSize int           // 100
    mu        sync.Mutex
    pending   []domain.EmbedInput
}

func (b *EmbedBatcher) Process(ctx context.Context, pf *ParsedFile) (*EmbeddedFile, error) {
    inputs := make([]domain.EmbedInput, 0, len(pf.Nodes))

    for _, node := range pf.Nodes {
        text := b.builder.BuildContext(&node)
        hash := sha256Hex(text)

        // Cache check: skip if content unchanged
        if node.ContentHash == hash && node.Embedding != nil {
            continue
        }

        inputs = append(inputs, domain.EmbedInput{
            ID:          node.ID,
            Text:        text,
            ContentHash: hash,
        })
    }

    if len(inputs) == 0 {
        return &EmbeddedFile{ParsedFile: pf, AllCached: true}, nil
    }

    // Batch in chunks of batchSize
    results := make(map[string][]float32)
    for i := 0; i < len(inputs); i += b.batchSize {
        end := min(i+b.batchSize, len(inputs))
        batch := inputs[i:end]

        res, err := b.embedder.EmbedBatch(ctx, batch)
        if err != nil {
            return nil, fmt.Errorf("embed batch: %w", err)
        }
        for _, r := range res {
            results[r.ID] = r.Vector
        }
    }

    // Apply embeddings to nodes
    for i := range pf.Nodes {
        if vec, ok := results[pf.Nodes[i].ID]; ok {
            pf.Nodes[i].Embedding = vec
            pf.Nodes[i].ContentHash = sha256Hex(b.builder.BuildContext(&pf.Nodes[i]))
        }
    }

    return &EmbeddedFile{ParsedFile: pf}, nil
}
```

---

## Embedder Adapter (internal/adapters/gemini/embedder.go)

Implements `domain.Embedder`:

```go
type GeminiEmbedder struct {
    client *genai.Client
    model  string           // "gemini-embedding-2-preview"
    dim    int              // 3072
    log    *slog.Logger
}

var _ domain.Embedder = (*GeminiEmbedder)(nil)

func NewGeminiEmbedder(cfg *config.GeminiConfig) (*GeminiEmbedder, error) {
    client, err := genai.NewClient(ctx, &genai.ClientConfig{
        APIKey: cfg.APIKey,
    })
    if err != nil {
        return nil, fmt.Errorf("create gemini client: %w", err)
    }

    return &GeminiEmbedder{
        client: client,
        model:  cfg.EmbedModel,
        dim:    cfg.EmbedDimension,
    }, nil
}

func (e *GeminiEmbedder) EmbedBatch(ctx context.Context, inputs []domain.EmbedInput) ([]domain.EmbedResult, error) {
    contents := make([]*genai.Content, len(inputs))
    for i, input := range inputs {
        parts := []genai.Part{genai.Text(input.Text)}
        // Add images if present
        for j, img := range input.Images {
            parts = append(parts, genai.Blob{
                MIMEType: input.ImageMIMEs[j],
                Data:     img,
            })
        }
        contents[i] = &genai.Content{Parts: parts}
    }

    result, err := e.client.Models.EmbedContent(ctx, e.model, &genai.EmbedContentRequest{
        Contents:             contents,
        OutputDimensionality: ptr(e.dim),
    })
    if err != nil {
        return nil, fmt.Errorf("gemini embed: %w", err)
    }

    results := make([]domain.EmbedResult, len(inputs))
    for i, emb := range result.Embeddings {
        results[i] = domain.EmbedResult{
            ID:     inputs[i].ID,
            Vector: emb.Values,
        }
    }

    return results, nil
}

func (e *GeminiEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
    text := fmt.Sprintf("task: search query | query: %s", query)

    result, err := e.client.Models.EmbedContent(ctx, e.model, &genai.EmbedContentRequest{
        Contents: []*genai.Content{{
            Parts: []genai.Part{genai.Text(text)},
        }},
        OutputDimensionality: ptr(e.dim),
    })
    if err != nil {
        return nil, fmt.Errorf("gemini embed query: %w", err)
    }

    return result.Embeddings[0].Values, nil
}
```

---

## LLM Explainer Adapter (internal/adapters/gemini/explainer.go)

Implements `domain.LLMExplainer` with streaming:

```go
type GeminiExplainer struct {
    client *genai.Client
    model  string          // "gemini-2.0-flash"
    log    *slog.Logger
}

var _ domain.LLMExplainer = (*GeminiExplainer)(nil)

func (e *GeminiExplainer) Explain(ctx context.Context, req domain.ExplainRequest) (<-chan domain.ExplainChunk, error) {
    prompt := buildExplainPrompt(req)

    ch := make(chan domain.ExplainChunk, 16)

    go func() {
        defer close(ch)

        iter := e.client.Models.GenerateContentStream(ctx, e.model, genai.Text(prompt), nil)
        for {
            resp, err := iter.Next()
            if err == iterator.Done {
                ch <- domain.ExplainChunk{Done: true}
                return
            }
            if err != nil {
                ch <- domain.ExplainChunk{Error: err, Done: true}
                return
            }

            for _, candidate := range resp.Candidates {
                for _, part := range candidate.Content.Parts {
                    if text, ok := part.(genai.Text); ok {
                        ch <- domain.ExplainChunk{Text: string(text)}
                    }
                }
            }
        }
    }()

    return ch, nil
}

func buildExplainPrompt(req domain.ExplainRequest) string {
    var sb strings.Builder

    switch req.QueryType {
    case "search":
        sb.WriteString("You are a code search assistant. The user asked: ")
        sb.WriteString(req.UserQuery)
        sb.WriteString("\n\nHere are the most relevant code snippets found:\n\n")
    case "trace":
        sb.WriteString("You are a code trace assistant. Explain the following call chain:\n\n")
    case "blast":
        sb.WriteString("You are a blast radius analyst. Explain the impact of changing the target function:\n\n")
    }

    for _, excerpt := range req.CodeContext {
        sb.WriteString(fmt.Sprintf("### %s (%s:%s)\n", excerpt.Qualified, excerpt.FilePath, excerpt.Lines))
        sb.WriteString("```\n")
        sb.WriteString(excerpt.Snippet)
        sb.WriteString("\n```\n\n")
    }

    if req.GraphContext != "" {
        sb.WriteString("Graph neighborhood:\n")
        sb.WriteString(req.GraphContext)
        sb.WriteString("\n\n")
    }

    sb.WriteString("Provide a clear, concise explanation.")
    return sb.String()
}
```

---

## Rate Limiting & Retry

Gemini API has rate limits. The embedder adapter should handle:

```go
func (e *GeminiEmbedder) EmbedBatchWithRetry(ctx context.Context, inputs []domain.EmbedInput) ([]domain.EmbedResult, error) {
    return retry.WithRetry(ctx, 3, func() ([]domain.EmbedResult, error) {
        return e.EmbedBatch(ctx, inputs)
    })
}
```

Common error codes:
- `429 RESOURCE_EXHAUSTED` → retry with exponential backoff
- `400 INVALID_ARGUMENT` → don't retry (input too large, etc.)
- `503 UNAVAILABLE` → retry

---

## Configuration

```go
type GeminiConfig struct {
    APIKey          string // GEMINI_API_KEY
    EmbedModel      string // gemini-embedding-2-preview
    ExplainModel    string // gemini-2.0-flash
    EmbedDimension  int    // 3072
    MaxBatchSize    int    // 100
}
```

Environment variables:
- `GEMINI_API_KEY` — required
- `GEMINI_EMBED_MODEL` — default: `gemini-embedding-2-preview`
- `GEMINI_EXPLAIN_MODEL` — default: `gemini-2.0-flash`
- `GEMINI_EMBED_DIM` — default: `3072`
- `GEMINI_BATCH_SIZE` — default: `100`

---

## Checklist for Gemini Code

1. [ ] Always prepend task instruction prefix (`task: search result | query:` or `task: search query | query:`)
2. [ ] Use `OutputDimensionality: ptr(3072)` for all embedding calls
3. [ ] Batch up to 100 inputs per `EmbedContent` call
4. [ ] SHA-256 cache: skip re-embedding unchanged content
5. [ ] Handle rate limits with exponential backoff + jitter
6. [ ] Stream LLM responses via `GenerateContentStream`
7. [ ] Close channels from the sender goroutine
8. [ ] Use `context.Context` for cancellation propagation
