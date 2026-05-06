package unsloth

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// Patterns for tool call extraction, tried in priority order.
var (
	// Format 1: <tool_call>{"name": "...", "arguments": {...}}</tool_call>.
	xmlToolCallPattern = regexp.MustCompile(`<tool_call>\s*(\{.*?\})\s*</tool_call>`)

	// Format 2: Gemma native <|tool_call>call:name{args}<tool_call|>.
	gemmaToolCallPattern = regexp.MustCompile(`<\|tool_call>call:(\w+)\{(.*?)\}<tool_call\|>`)

	// thinkPattern matches <think>...</think> blocks.
	thinkPattern = regexp.MustCompile(`(?s)<think>.*?</think>`)
)

// rawToolCall is the JSON structure inside <tool_call> tags.
type rawToolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// parseToolCalls extracts tool calls from model content text.
// Supports multiple formats: XML tags, Gemma native tokens, and Python-style calls.
// Returns parsed tool calls and the remaining content with tool call blocks removed.
func parseToolCalls(content string) ([]schema.ToolCall, string) {
	// Try Format 1: <tool_call>{JSON}</tool_call>
	if calls, cleaned := parseXMLToolCalls(content); len(calls) > 0 {
		return calls, cleaned
	}

	// Try Format 2: Gemma native <|tool_call>call:name{...}<tool_call|>
	if calls, cleaned := parseGemmaToolCalls(content); len(calls) > 0 {
		return calls, cleaned
	}

	// Try Format 3: tool_name(key="value", ...) or tool_name {"key": "value"}
	if calls, cleaned := parseFunctionStyleCalls(content); len(calls) > 0 {
		return calls, cleaned
	}

	// Try Format 4: bare tool name (e.g. "search_code\n") — small-context fallback.
	if calls, cleaned := parseBareToolNames(content); len(calls) > 0 {
		return calls, cleaned
	}

	return nil, content
}

func parseXMLToolCalls(content string) ([]schema.ToolCall, string) {
	matches := xmlToolCallPattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil, content
	}

	var calls []schema.ToolCall
	for i, match := range matches {
		if len(match) < 2 {
			continue
		}
		var raw rawToolCall
		if err := json.Unmarshal([]byte(match[1]), &raw); err != nil {
			continue
		}
		argsBytes, _ := json.Marshal(raw.Arguments)
		calls = append(calls, schema.ToolCall{
			ID:   fmt.Sprintf("call_%d", i),
			Type: "function",
			Function: schema.FunctionCall{
				Name:      raw.Name,
				Arguments: string(argsBytes),
			},
		})
	}

	cleaned := xmlToolCallPattern.ReplaceAllString(content, "")
	cleaned = thinkPattern.ReplaceAllString(cleaned, "")
	return calls, strings.TrimSpace(cleaned)
}

func parseGemmaToolCalls(content string) ([]schema.ToolCall, string) {
	matches := gemmaToolCallPattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil, content
	}

	var calls []schema.ToolCall
	for i, match := range matches {
		if len(match) < 3 {
			continue
		}
		name := match[1]
		argsRaw := "{" + match[2] + "}"
		// Gemma format uses key:value — try JSON parse, else wrap as query.
		var argsMap map[string]any
		if err := json.Unmarshal([]byte(argsRaw), &argsMap); err != nil {
			argsMap = map[string]any{"question": match[2]}
		}
		argsBytes, _ := json.Marshal(argsMap)
		calls = append(calls, schema.ToolCall{
			ID:   fmt.Sprintf("call_%d", i),
			Type: "function",
			Function: schema.FunctionCall{
				Name:      name,
				Arguments: string(argsBytes),
			},
		})
	}

	cleaned := gemmaToolCallPattern.ReplaceAllString(content, "")
	cleaned = thinkPattern.ReplaceAllString(cleaned, "")
	return calls, strings.TrimSpace(cleaned)
}

// parseFunctionStyleCalls handles Python-style calls like:
//
//	search_code(question="TraceService", top_k=5)
//	search_code {"question": "TraceService"}
//
// Only matches known tool names stored in registeredToolNames.
var registeredToolNames []string

// RegisterToolNames stores tool names for function-style parsing.
func RegisterToolNames(names []string) {
	registeredToolNames = names
}

func parseFunctionStyleCalls(content string) ([]schema.ToolCall, string) {
	if len(registeredToolNames) == 0 {
		return nil, content
	}

	var calls []schema.ToolCall
	cleaned := content

	for i, name := range registeredToolNames {
		// Match: tool_name(args) or tool_name {json}
		pat := regexp.MustCompile(regexp.QuoteMeta(name) + `\s*[\(\{](.*?)[\)\}]`)
		matches := pat.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			argsStr := match[1]

			// Try parsing as JSON first.
			argsMap := make(map[string]any)
			if err := json.Unmarshal([]byte("{"+argsStr+"}"), &argsMap); err != nil {
				// Try Python kwargs: key="value", key=value
				argsMap = parsePythonKwargs(argsStr)
			}
			if len(argsMap) == 0 {
				// Last resort: treat entire arg as "question"
				argsMap = map[string]any{"question": strings.Trim(argsStr, "\"'")}
			}

			argsBytes, _ := json.Marshal(argsMap)
			calls = append(calls, schema.ToolCall{
				ID:   fmt.Sprintf("call_%d", i),
				Type: "function",
				Function: schema.FunctionCall{
					Name:      name,
					Arguments: string(argsBytes),
				},
			})
			cleaned = strings.Replace(cleaned, match[0], "", 1)
		}
	}

	if len(calls) == 0 {
		return nil, content
	}
	cleaned = thinkPattern.ReplaceAllString(cleaned, "")
	return calls, strings.TrimSpace(cleaned)
}

// parseBareToolNames detects when the model outputs just a tool name
// (e.g. "search_code\n") without arguments. Common with small-context models
// where the tool calling instructions are truncated.
func parseBareToolNames(content string) ([]schema.ToolCall, string) {
	if len(registeredToolNames) == 0 {
		return nil, content
	}

	// Strip thinking blocks first.
	stripped := thinkPattern.ReplaceAllString(content, "")
	stripped = strings.TrimSpace(stripped)

	var calls []schema.ToolCall
	cleaned := content

	for i, name := range registeredToolNames {
		// Check if the stripped content contains the bare tool name.
		if strings.Contains(stripped, name) {
			// Build default args from lastUserQuery if available.
			args := map[string]any{}
			if lastUserQuery != "" {
				args["question"] = lastUserQuery
			}
			argsBytes, _ := json.Marshal(args)
			calls = append(calls, schema.ToolCall{
				ID:   fmt.Sprintf("call_%d", i),
				Type: "function",
				Function: schema.FunctionCall{
					Name:      name,
					Arguments: string(argsBytes),
				},
			})
			cleaned = strings.Replace(cleaned, name, "", 1)
			break // Only one bare-name call per response.
		}
	}

	if len(calls) == 0 {
		return nil, content
	}
	cleaned = thinkPattern.ReplaceAllString(cleaned, "")
	return calls, strings.TrimSpace(cleaned)
}

// lastUserQuery stores the most recent user question for bare-name fallback.
var lastUserQuery string

// SetLastUserQuery stores the user's query for tool call argument inference.
func SetLastUserQuery(q string) {
	lastUserQuery = q
}

// parsePythonKwargs parses key="value", key=value style arguments.
func parsePythonKwargs(s string) map[string]any {
	result := make(map[string]any)
	// Match key="value" or key=value patterns.
	pat := regexp.MustCompile(`(\w+)\s*=\s*"([^"]*)"`)
	for _, match := range pat.FindAllStringSubmatch(s, -1) {
		if len(match) >= 3 {
			result[match[1]] = match[2]
		}
	}
	// Also try key=number.
	numPat := regexp.MustCompile(`(\w+)\s*=\s*(\d+)`)
	for _, match := range numPat.FindAllStringSubmatch(s, -1) {
		if len(match) >= 3 {
			if _, exists := result[match[1]]; !exists {
				n, _ := fmt.Sscanf(match[2], "%d", new(int))
				if n > 0 {
					result[match[1]] = match[2]
				}
			}
		}
	}
	return result
}

// buildToolPrompt generates a compact tool description for system prompt injection.
// Tools are described concisely (name + one-line description only) to stay within
// small context windows (4K-8K). Full parameter schemas are omitted — the model
// can infer parameters from tool names and descriptions.
func buildToolPrompt(tools []*schema.ToolInfo) string {
	if len(tools) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n## TOOL USE INSTRUCTIONS (CRITICAL)\n")
	b.WriteString("To call a tool, output EXACTLY: <tool_call>{\"name\": \"TOOL\", \"arguments\": {\"key\": \"value\"}}</tool_call>\n")
	b.WriteString("Available tools:\n")

	for _, t := range tools {
		b.WriteString("- ")
		b.WriteString(t.Name)
		// Add first sentence of description only.
		desc := t.Desc
		if idx := strings.Index(desc, "."); idx > 0 && idx < 80 {
			desc = desc[:idx+1]
		}
		if len(desc) > 80 {
			desc = desc[:80]
		}
		b.WriteString(": ")
		b.WriteString(desc)
		b.WriteString("\n")
	}
	return b.String()
}
