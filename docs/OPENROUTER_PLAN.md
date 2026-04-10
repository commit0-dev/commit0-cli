# OpenRouter Integration Plan

> Multi-model LLM support via OpenRouter + real-time cost tracking.
> See `.claude/plans/joyful-drifting-nova.md` for full implementation details.

## Summary

Replace direct Gemini coupling with OpenRouter — unified API gateway for 200+ models. The ADK `model.LLM` interface (2 methods: `Name()`, `GenerateContent()`) gets a custom adapter that translates between ADK's `genai` types and OpenAI message format.

## Status: PLANNED — not yet implemented

## Key Files to Create
```
internal/adapters/openrouter/types.go      — OpenAI request/response structs
internal/adapters/openrouter/translate.go  — genai ↔ OpenAI content translation
internal/adapters/openrouter/client.go     — HTTP client, SSE streaming, cost tracking
internal/adapters/openrouter/model.go      — ADK model.LLM adapter
internal/adapters/openrouter/explainer.go  — domain.LLMExplainer adapter
```

## Key Files to Modify
```
internal/config/config.go              — add LLMProvider, OpenRouterConfig
internal/app/agent/service.go          — accept model.LLM instead of creating Gemini
internal/app/agent/delegate.go         — use model factory
cmd/wire.go                            — provider switch
internal/tui/app.go                    — cost tracking in status bar
```

## Config
```bash
export LLM_PROVIDER=openrouter
export OPENROUTER_API_KEY=sk-or-...
export OPENROUTER_MODEL=google/gemini-2.5-flash-preview  # or anthropic/claude-sonnet-4, etc.
```
