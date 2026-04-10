# commit0 Documentation

## When to Read What

| You need to... | Read | Key sections |
|---|---|---|
| Understand why commit0 exists | [VISION.md](VISION.md) | Core mission, data flow tracing, temporal graph, memory tiers, competitive moat |
| Design or review architecture | [ARCHITECTURE.md](ARCHITECTURE.md) | Client-server model, hexagonal layers, port interfaces, tech stack |
| Implement a feature | [BACKEND.md](BACKEND.md) | Service descriptions, HTTP API routes (21 endpoints), agent orchestration, error handling |
| Write or modify SurrealQL | [DATABASE.md](DATABASE.md) | Node/edge tables, HNSW tuning, hybrid search queries, traversal patterns |
| Change indexing or search | [PIPELINE.md](PIPELINE.md) | 6-stage index pipeline, 4-stage retrieval, embedding strategy, cost/performance |
| Work on security analysis | [SECURITY_ROADMAP.md](SECURITY_ROADMAP.md) | 13 edge types, taint rules, API surface discovery, next features |
| Navigate unfamiliar code | [LAYOUT.md](LAYOUT.md) | Annotated file tree with purpose of every file |
| Evaluate embedding models | [EMBEDDING_RESEARCH.md](EMBEDDING_RESEARCH.md) | Voyage vs OpenAI vs Gemini vs self-hosted, benchmarks, decision matrix |
| Plan local/offline features | [LOCAL_MODEL.md](LOCAL_MODEL.md) | Embedded Gemma 4, hardware requirements, competitive positioning |
| Implement commit zero features | [DESIGN.md](DESIGN.md) | Types, ports, services, implementation phases, file map |

## External Reference

| Resource | Location |
|----------|----------|
| Gemini Embedding API | [reference/gemini-api/embeddings.md](reference/gemini-api/embeddings.md) |
| Gemini Structured Output | [reference/gemini-api/structured-outputs.md](reference/gemini-api/structured-outputs.md) |
