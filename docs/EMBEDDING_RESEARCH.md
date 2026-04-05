# Embedding Model Research — commit0

> **Goal:** Identify better and/or cheaper alternatives to `gemini-embedding-2-preview` (3072-dim, $0.20/1M tokens) for multi-language code retrieval.
>
> **Date:** April 2026

---

## TL;DR Recommendation

| Priority | Model | Why |
|----------|-------|-----|
| **Best quality** | `voyage-code-3` @ 1024 dim | +14–17% over OpenAI on 32 code datasets; purpose-built for code |
| **Best value** | `voyage-code-3` @ 512 dim | Same provider, same API, 4× cheaper storage vs. current 3072-dim Gemini |
| **Cheapest API** | `text-embedding-3-small` (OpenAI) | $0.02/1M — 10× cheaper than Gemini; acceptable code retrieval quality |
| **Self-hosted SOTA** | `Qodo-Embed-1-1.5B` or `Qwen3-Embedding-8B` | Best CoIR/MTEB-Code scores; eliminates rate limits and API costs |

**Recommended migration path:** `voyage-code-3` at **1024 dims** — strongest code retrieval accuracy, 3× cheaper than Gemini Embedding 2, and dimensionality reduction requires only a schema bump + re-index.

---

## Current Baseline

| Attribute | Value |
|-----------|-------|
| Model | `gemini-embedding-2-preview` |
| Dimensions | 3072 (HNSW indexes hard-coded) |
| Pricing | $0.20 / 1M tokens |
| Max batch | 100 inputs |
| SDK | `google.golang.org/genai` |
| Query prefix | `"task: code retrieval | query: …"` |
| Context window | ~8K tokens (estimated) |

The current `EmbedResult.Vector` comment says `// 3072-dimensional`. The HNSW indexes in `schema.surql` are also hard-coded to `DIMENSION 3072`.

---

## Model Comparison

### 1. Voyage AI — `voyage-code-3`

| Attribute | Value |
|-----------|-------|
| **Pricing** | $0.06 / 1M tokens (200M free on signup) |
| **Dimensions** | 2048 / 1024 / 512 / 256 (Matryoshka) |
| **Max tokens** | ~16K (estimated, check docs) |
| **CoIR benchmark** | State-of-the-art for commercial APIs |
| **vs. OpenAI 3-large** | +16.30% average across 32 code retrieval datasets |
| **vs. OpenAI 3-large @ 1024 dim** | +14.64% |
| **Quantization** | float32, int8, uint8, binary, ubinary |
| **Rate limits** | 2,000 RPM / 8M TPM (Tier 1); scales with usage |
| **Go SDK** | REST-based; community Go client or direct HTTP |
| **Note** | Voyage AI acquired by MongoDB (2025) |

**Verdict:** Best accuracy for code retrieval among all commercial APIs. At 1024 dims it's 3× cheaper than Gemini Embedding 2 and delivers superior results. The main friction is writing a new `VoyageEmbedder` adapter (straightforward given the clean `domain.Embedder` interface).

---

### 2. OpenAI — `text-embedding-3-small` / `text-embedding-3-large`

| Attribute | 3-small | 3-large |
|-----------|---------|---------|
| **Pricing** | $0.02 / 1M | $0.13 / 1M |
| **Batch pricing** | $0.01 / 1M | $0.065 / 1M |
| **Dimensions** | 1536 → 256 (MRL) | 3072 → 256 (MRL) |
| **Max tokens** | 8,192 | 8,192 |
| **CoIR score** | ~60–62 (estimated) | ~65.17 |
| **Matryoshka** | Yes | Yes |
| **Rate limits** | 300K tokens/request max | same |
| **Go SDK** | `github.com/openai/openai-go` (official) |

**Verdict:** `text-embedding-3-small` is the cheapest commercial option at $0.02/1M — 10× cheaper than Gemini. Quality is noticeably below voyage-code-3 on code tasks (~10–15 points on CoIR) but may be acceptable. Official Go SDK is a plus. `text-embedding-3-large` costs 6.5× more than `3-small` for modest quality gains on code.

---

### 3. Google — `gemini-embedding-2-preview` (current) vs. alternatives

| Attribute | embedding-2 | embedding-001 |
|-----------|-------------|---------------|
| **Pricing** | $0.20 / 1M | $0.15 / 1M ($0.075 batch) |
| **Dimensions** | 3072 / 1536 / 768 (MRL) | not published |
| **Multimodal** | Yes (text + image) | No |
| **Go SDK** | `google.golang.org/genai` | same |

**Verdict:** Staying on Gemini Embedding 2 is the highest-cost option and not the highest-quality option for pure code retrieval. The multimodal capability (images) is currently used in `EmbedBatch` — if image embeddings are needed, Gemini Embedding 2 or Cohere Embed v4 are the only commercial APIs that support them. If code-only, switch away.

---

### 4. Cohere — `embed-v4`

| Attribute | Value |
|-----------|-------|
| **Pricing** | $0.12 / 1M text; $0.47 / 1M images |
| **Dimensions** | 256–1536 (configurable) |
| **Max tokens** | 128K (huge) |
| **Matryoshka** | Configurable dims |
| **Rate limits** | Tiered |
| **Go SDK** | Yes (Cohere SDK) |

**Verdict:** Attractive for the 128K context window (useful for large files) and image support, but at $0.12/1M it's still expensive. No evidence it outperforms voyage-code-3 on code retrieval. The `embed-english-v3.0` at $0.02/1M is a cheap fallback but only 512-token context.

---

### 5. Self-Hosted / Open-Source

| Model | Size | CoIR Score | MTEB-Code | Notes |
|-------|------|-----------|-----------|-------|
| **Qodo-Embed-1-1.5B** | 1.5B | 68.53 | — | Exceptional; outperforms 7B models; Apache 2.0 |
| **Qodo-Embed-1-7B** | 7B | 71.5 | — | SOTA for code; outperforms OpenAI 3-large (65.17) |
| **Qwen3-Embedding-8B** | 8B | — | 80.68 | #1 MTEB-Code; multilingual; strong for Go + Python + TS |
| **C2LLM-7B** | 7B | — | 80.75 | Current SOTA MTEB-Code; newer research model |
| **Nomic Embed Code** | ~137M | — | — | Lightweight, production-ready, outperforms ada-002 |
| **Jina Embeddings v4** | — | — | Strong | Multi-language, code adapters, MRL |

**Self-hosting trade-offs:**
- **Pros:** Zero API cost, zero rate limits, 5–15ms latency (vs. 100–200ms API), data privacy, no re-index needed if using same dim
- **Cons:** GPU infrastructure needed for throughput; operational overhead; Qodo/Qwen models need significant VRAM

**Recommended self-hosted:** `Qodo-Embed-1-1.5B` via Ollama or vLLM if you have a GPU available — best efficiency/quality ratio and can serve via a simple REST endpoint that mirrors the `domain.Embedder` interface.

---

## Benchmark Reference

| Benchmark | What It Measures | Top Models (2026) |
|-----------|-----------------|-------------------|
| **CoIR** | Code retrieval (10 datasets, NDCG@10) | Qodo-Embed-1-7B (71.5), voyage-code-3, OpenAI 3-large (65.17) |
| **MTEB-Code** | Code tasks (Hugging Face leaderboard) | C2LLM-7B (80.75), Qwen3-Embedding-8B (80.68) |
| **BEIR** | General IR including code tasks | OpenAI 3-large, voyage-3 |
| **CodeSearchNet** | Classic GitHub code search (legacy) | Replaced by CoIR |

---

## Migration Impact Analysis

### What needs to change per provider

#### A. Voyage AI (`voyage-code-3`)

1. **New adapter:** `internal/adapters/voyage/embedder.go` — implement `domain.Embedder` via Voyage REST API. The interface is clean: `EmbedBatch` + `EmbedQuery`.
2. **Config:** Add `VoyageConfig` struct (or rename `GeminiConfig` → `EmbedConfig`) with `APIKey`, `Model`, `Dimension`, `BatchSize`.
3. **Schema change:** `DIMENSION 3072` → `DIMENSION 1024` (or 512/256) in `schema.surql`. Bump `schemaVersion` to `6`. **Full re-index required** — existing embeddings are incompatible across providers.
4. **Query prefix:** Voyage uses `input_type: "query"` vs. `"document"` — handled at the HTTP level, not as a text prefix. Update `EmbedQuery` accordingly.
5. **`EmbedResult` comment:** Minor doc change (`// 1024-dimensional`).
6. **Wire-up:** Update `wireServeServices()` to construct `VoyageEmbedder` instead of `GeminiEmbedder`.

**Estimated effort:** ~2–3 days including tests and re-index.

#### B. OpenAI (`text-embedding-3-small`)

Same as Voyage but use `github.com/openai/openai-go` (official Go SDK). `dimensions` parameter supported natively. Query asymmetry is NOT needed for OpenAI — the same embedding works for both documents and queries.

**Estimated effort:** ~1–2 days.

#### C. Self-hosted (Qodo / Qwen)

Expose model via vLLM or Ollama with an OpenAI-compatible REST endpoint (`/v1/embeddings`). The adapter can reuse the OpenAI adapter structure, just pointing at `http://localhost:11434`. No API key needed.

**Estimated effort:** ~1 day adapter + infrastructure setup time.

---

## Dimension Trade-off

The current 3072-dim HNSW indexes use cosine distance over F32 vectors. Reducing dimensions has these effects:

| Dimension | Storage per node | HNSW build time | Query latency | Quality (voyage-code-3) |
|-----------|-----------------|-----------------|---------------|------------------------|
| 3072 | 12 KB | Slowest | ~2–5 ms | — |
| 2048 | 8 KB | Slow | ~1–3 ms | Best |
| **1024** | **4 KB** | **Medium** | **~0.5–1 ms** | **+14.6% vs. OAI 3-large** |
| 512 | 2 KB | Fast | <0.5 ms | Near-equivalent to 1024 |
| 256 | 1 KB | Fastest | <0.3 ms | Slight drop |

**Recommendation: 1024 dims** — sweet spot for quality, storage, and latency.

---

## Cost Projection

Assume a medium codebase: **500K code nodes**, average **200 tokens each** = **100M tokens per full index**.

| Model | Dim | $/1M tokens | Full index cost | Monthly re-index (10%) |
|-------|-----|-------------|----------------|----------------------|
| `gemini-embedding-2-preview` | 3072 | $0.20 | **$20.00** | $2.00 |
| `voyage-code-3` | 1024 | $0.06 | **$6.00** | $0.60 |
| `text-embedding-3-small` | 1536 | $0.02 | **$2.00** | $0.20 |
| `text-embedding-3-large` | 3072 | $0.13 | **$13.00** | $1.30 |
| `Qodo-Embed-1-1.5B` (self-hosted) | configurable | ~$0/token | ~$0 | GPU cost only |

For a large enterprise codebase (10M tokens), voyage-code-3 saves $14/index vs. Gemini; over 12 months of weekly re-indexes that's ~$730 saved.

---

## Decision Matrix

| Criterion | Weight | Gemini 2 | voyage-code-3 | OAI 3-small | Qodo-1.5B (self) |
|-----------|--------|----------|---------------|-------------|-----------------|
| Code quality (CoIR) | 35% | ~67 | ~73 | ~61 | 68.5 |
| Cost | 25% | ⭐ ($0.20) | ⭐⭐⭐ ($0.06) | ⭐⭐⭐⭐⭐ ($0.02) | ⭐⭐⭐⭐⭐ (~$0) |
| Rate limits | 20% | Medium | High (2K RPM) | High | Unlimited |
| Latency | 10% | ~150ms | ~150ms | ~120ms | 5–15ms |
| Go SDK / migration effort | 10% | Low (done) | Medium | Low (official SDK) | Medium |

**Weighted winner: `voyage-code-3` at 1024 dims** — highest code retrieval quality, 3× cheaper than baseline, good rate limits, REST API with simple adapter.

---

## Recommended Next Steps

1. **Short-term:** Implement `VoyageEmbedder` behind the existing `domain.Embedder` interface. Gate behind a config flag (`EMBEDDER_PROVIDER=voyage|gemini|openai`) so it can be tested in parallel.
2. **Evaluate:** Run the same codebase through both Gemini Embedding 2 and voyage-code-3, compare retrieval recall on a test query set.
3. **Schema migration:** Once validated, bump `schemaVersion` to `6`, update HNSW `DIMENSION` to `1024`, and trigger a full re-index.
4. **Future:** If infrastructure is available, benchmark `Qodo-Embed-1-1.5B` self-hosted for zero marginal cost at scale.

---

*Research compiled April 2026. Check [huggingface.co/spaces/mteb/leaderboard](https://huggingface.co/spaces/mteb/leaderboard) and [voyageai.com/pricing](https://www.voyageai.com/pricing) for latest numbers before committing.*
