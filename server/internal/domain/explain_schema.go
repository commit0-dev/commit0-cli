package domain

// SchemaForQueryType returns the JSON Schema for structured LLM output.
// Used by both Gemini (ResponseJsonSchema) and Ollama (format field)
// to enable constrained decoding.
func SchemaForQueryType(queryType string) map[string]any {
	switch queryType {
	case "search":
		return searchSchema()
	case "trace":
		return traceSchema()
	case "blast":
		return blastSchema()
	case "summarize":
		return summarizeBatchSchema()
	case "summarize-single":
		return summarizeSingleSchema()
	default:
		return genericSchema()
	}
}

func searchSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"overview": map[string]any{
				"type":        "string",
				"description": "Direct 2-3 sentence answer to the question",
			},
			"evidence": map[string]any{
				"type":        "array",
				"description": "Code locations that support the answer",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"function":    map[string]any{"type": "string", "description": "Qualified function or type name"},
						"file":        map[string]any{"type": "string", "description": "Source file path"},
						"lines":       map[string]any{"type": "string", "description": "Line range e.g. 42-78"},
						"description": map[string]any{"type": "string", "description": "What this code does and why it is relevant"},
						"relevance":   map[string]any{"type": "string", "description": "How this evidence supports the answer"},
					},
					"required": []string{"function", "description"},
				},
			},
			"insights": map[string]any{
				"type":        "array",
				"description": "Actionable observations about architecture, patterns, or risks",
				"items":       map[string]any{"type": "string"},
			},
		},
		"required": []string{"overview", "evidence"},
	}
}

func traceSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"overview": map[string]any{
				"type":        "string",
				"description": "2-3 sentence summary of the call chain",
			},
			"flow_steps": map[string]any{
				"type":        "array",
				"description": "Step-by-step execution flow",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"hop":          map[string]any{"type": "integer", "description": "Step number in the call chain"},
						"function":     map[string]any{"type": "string", "description": "Qualified function name"},
						"action":       map[string]any{"type": "string", "description": "What this function does in the flow"},
						"data_changes": map[string]any{"type": "string", "description": "Data transformations or side effects at this step"},
					},
					"required": []string{"hop", "function", "action"},
				},
			},
			"key_insights": map[string]any{
				"type":        "array",
				"description": "Important observations about branching, error handling, or design",
				"items":       map[string]any{"type": "string"},
			},
		},
		"required": []string{"overview", "flow_steps"},
	}
}

func blastSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"overview": map[string]any{
				"type":        "string",
				"description": "2-3 sentence impact assessment",
			},
			"severity": map[string]any{
				"type":        "string",
				"enum":        []string{"low", "medium", "high", "critical"},
				"description": "Overall severity of the change impact",
			},
			"risk_areas": map[string]any{
				"type":        "array",
				"description": "Components at risk from this change",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"function":   map[string]any{"type": "string", "description": "Affected function or type"},
						"file":       map[string]any{"type": "string", "description": "Source file path"},
						"risk":       map[string]any{"type": "string", "description": "What could break and how"},
						"mitigation": map[string]any{"type": "string", "description": "How to mitigate the risk"},
					},
					"required": []string{"function", "risk"},
				},
			},
			"migration_steps": map[string]any{
				"type":        "array",
				"description": "Suggested order of changes for safe migration",
				"items":       map[string]any{"type": "string"},
			},
		},
		"required": []string{"overview", "severity", "risk_areas"},
	}
}

func summarizeBatchSchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"summary":  map[string]any{"type": "string", "description": "One-paragraph description of what this code does and why"},
				"concepts": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "3-5 semantic tags like caching, auth, middleware"},
			},
			"required": []string{"summary", "concepts"},
		},
	}
}

func summarizeSingleSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary":  map[string]any{"type": "string", "description": "One-paragraph description of what this code does and why"},
			"concepts": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "3-5 semantic tags like caching, auth, middleware"},
		},
		"required": []string{"summary", "concepts"},
	}
}

func genericSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"overview": map[string]any{"type": "string"},
			"insights": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"overview"},
	}
}
