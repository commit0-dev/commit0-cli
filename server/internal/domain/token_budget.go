package domain

// TokenBudget defines context window allocation for an LLM operation.
// Each section has a max rune budget. The total must not exceed the
// model's context window (converted to runes at ~3 chars/token for code).
//
// Callers create a budget for their operation, pass it to the ContextBuilder,
// and the builder fills sections in priority order, stopping when budget is exhausted.
type TokenBudget struct {
	// Total is the maximum runes for the entire output text.
	// Derived from the model's context window: tokens * charsPerToken.
	Total int

	// Per-section budgets (priority order: higher sections filled first).
	Prefix    int // task prefix + kind + qualified name
	Summary   int // semantic summary or docstring
	Concepts  int // concept tags
	Signature int // function signature
	Neighbors int // graph context (callers, callees, data flow)
	Body      int // code body (lowest priority — gets remainder)
}

// DefaultEmbedBudget returns a budget for embedding operations.
// Sized for models with the given context window in tokens.
func DefaultEmbedBudget(contextTokens int) TokenBudget {
	// Conservative: 3 chars per token for code
	total := contextTokens * 3
	if total <= 0 {
		total = 6000 // fallback: ~2000 tokens
	}

	return TokenBudget{
		Total:     total,
		Prefix:    200,
		Summary:   500,
		Concepts:  100,
		Signature: 300,
		Neighbors: 800,
		Body:      total - 1900, // remainder after fixed sections
	}
}

// DefaultSummarizeBudget returns a budget for LLM summarization input.
// The summarizer sends code to the LLM and gets back summary + concepts.
// Sized for the LLM's context window (typically larger than embedding models).
func DefaultSummarizeBudget(contextTokens int) TokenBudget {
	total := contextTokens * 3
	if total <= 0 {
		total = 24000 // fallback: ~8000 tokens
	}

	return TokenBudget{
		Total:     total,
		Prefix:    200,
		Summary:   0, // no existing summary in input (that's the output)
		Concepts:  0, // no existing concepts in input
		Signature: 500,
		Neighbors: 0,           // not used for summarization
		Body:      total - 700, // code body is the main input
	}
}

// DefaultExplainBudget returns a budget for LLM explanation input.
// The explainer receives code excerpts + graph context + user question.
func DefaultExplainBudget(contextTokens int) TokenBudget {
	total := contextTokens * 3
	if total <= 0 {
		total = 24000
	}

	return TokenBudget{
		Total:     total,
		Prefix:    300,
		Summary:   500,
		Concepts:  200,
		Signature: 500,
		Neighbors: 2000,
		Body:      total - 3500,
	}
}
