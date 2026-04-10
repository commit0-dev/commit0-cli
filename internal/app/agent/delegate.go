package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/genai"

	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
)

// ModelFactory creates an ADK LLM model. Injected from the wiring layer
// so delegate.go never imports concrete adapters (Gemini, OpenRouter, etc.).
type ModelFactory func() (adkmodel.LLM, error)

// delegateConfig holds services needed to create sub-agents.
type delegateConfig struct {
	querySvc     *app.QueryService
	traceSvc     *app.TraceService
	blastSvc     *app.BlastService
	flowSvc      *app.FieldFlowService
	tempSvc      *app.TemporalService
	rootCauseSvc *app.RootCauseAnalysisService
	store        domain.GraphStore
	gitWalker    domain.GitWalker
	explainer    domain.LLMExplainer
	cfg          *config.Config
	pad          *Scratchpad
	modelFactory ModelFactory
	log          *slog.Logger
}

// BuildDelegateTool creates the delegate tool for the Analyst agent.
func BuildDelegateTool(
	querySvc *app.QueryService,
	traceSvc *app.TraceService,
	blastSvc *app.BlastService,
	flowSvc *app.FieldFlowService,
	tempSvc *app.TemporalService,
	rootCauseSvc *app.RootCauseAnalysisService,
	store domain.GraphStore,
	gitWalker domain.GitWalker,
	explainer domain.LLMExplainer,
	cfg *config.Config,
	pad *Scratchpad,
	modelFactory ModelFactory,
) (tool.Tool, error) {
	dc := &delegateConfig{
		querySvc: querySvc, traceSvc: traceSvc, blastSvc: blastSvc,
		flowSvc: flowSvc, tempSvc: tempSvc, rootCauseSvc: rootCauseSvc,
		store: store, gitWalker: gitWalker, explainer: explainer,
		cfg: cfg, pad: pad, modelFactory: modelFactory,
		log: slog.Default().With("component", "delegate"),
	}

	return functiontool.New(functiontool.Config{
		Name: "delegate",
		Description: "Delegate a focused investigation task to a specialized sub-agent. " +
			"Types: search (discovery), trace (structural), security (risks), deep_dive (code detail). " +
			"IMPORTANT: You MUST call update_scratchpad after receiving results from this tool.",
	}, func(ctx tool.Context, input delegateInput) (delegateOutput, error) {
		return dc.execute(ctx, input)
	})
}

type delegateInput struct {
	AgentType string `json:"agent_type"` // search, trace, security, deep_dive
	Task      string `json:"task"`       // what to investigate
	Context   string `json:"context"`    // prior findings to inform this delegation
}

type delegateOutput struct {
	Status    string       `json:"status"`     // success, timeout, empty, error, budget_exceeded
	Findings  string       `json:"findings"`   // sub-agent output
	Duration  string       `json:"duration"`   // how long it took
	ToolCalls int          `json:"tool_calls"` // how many tools the sub-agent used
	ToolLog   []SubToolLog `json:"tool_log"`   // sub-agent tool call history
}

// SubToolLog records a single tool call made by a sub-agent.
type SubToolLog struct {
	Name       string `json:"name"`
	DurationMs int64  `json:"duration_ms"`
	ResultSize int    `json:"result_size"`
}

func (dc *delegateConfig) execute(ctx tool.Context, input delegateInput) (delegateOutput, error) {
	// Protocol enforcement: refuse if scratchpad wasn't updated since last delegation.
	if dc.pad.DelegationCount > 0 && !dc.pad.UpdatedSinceDelegation {
		return delegateOutput{
			Status:   "error",
			Findings: "You must call update_scratchpad before delegating again. Record what you learned from the previous delegation.",
		}, nil
	}

	// Cost budget check.
	if dc.pad.CostConsumed > dc.pad.CostBudget {
		return delegateOutput{
			Status:   "budget_exceeded",
			Findings: "Cost budget exceeded. Synthesize with available evidence using write_report.",
		}, nil
	}
	if dc.pad.CostConsumed > dc.pad.CostBudget*0.8 {
		dc.log.Warn("cost budget 80% consumed", "consumed", dc.pad.CostConsumed, "budget", dc.pad.CostBudget)
	}

	// Max delegation check.
	if dc.pad.DelegationCount >= 8 {
		return delegateOutput{
			Status:   "error",
			Findings: "Maximum 8 delegations reached. Synthesize with available evidence.",
		}, nil
	}

	// Validate agent type.
	instruction, ok := subAgentInstructions[input.AgentType]
	if !ok {
		return delegateOutput{
			Status:   "error",
			Findings: fmt.Sprintf("Unknown agent type: %s. Use: search, trace, security, deep_dive", input.AgentType),
		}, nil
	}

	// Select tools for sub-agent.
	subTools, err := dc.toolsForType(input.AgentType)
	if err != nil {
		return delegateOutput{Status: "error", Findings: err.Error()}, nil
	}

	// Build sub-agent prompt.
	prompt := input.Task
	if input.Context != "" {
		prompt = fmt.Sprintf("## Task\n%s\n\n## Context from prior analysis\n%s", input.Task, input.Context)
	}

	start := time.Now()
	dc.pad.DelegationCount++
	dc.pad.UpdatedSinceDelegation = false

	// Record action.
	dc.pad.RecordAction("delegate:"+input.AgentType, input.Task, 0)

	// Run sub-agent with timeout.
	findings, toolCalls, toolLog, err := dc.runSubAgent(input.AgentType, instruction, subTools, prompt)
	duration := time.Since(start)

	if err != nil {
		dc.log.Error("sub-agent failed", "type", input.AgentType, "err", err, "duration", duration)
		return delegateOutput{
			Status:   "error",
			Findings: fmt.Sprintf("Sub-agent error: %v. Try a different approach.", err),
			Duration: duration.String(),
			ToolLog:  toolLog,
		}, nil
	}

	// Validate output quality.
	if len(strings.TrimSpace(findings)) < 20 {
		return delegateOutput{
			Status:   "empty",
			Findings: "Sub-agent produced no useful output. Try different search terms or a different agent type.",
			Duration: duration.String(),
			ToolLog:  toolLog,
		}, nil
	}

	// Estimate cost (rough: ~0.001 per 1K input tokens, ~0.004 per 1K output tokens for Flash).
	estimatedCost := float64(len(prompt)+len(findings)) / 4000.0 * 0.002
	dc.pad.CostConsumed += estimatedCost
	dc.pad.TokensConsumed += (len(prompt) + len(findings)) / 4

	dc.log.Info("delegation complete",
		"type", input.AgentType,
		"duration", duration,
		"findings_len", len(findings),
		"tool_calls", toolCalls,
		"cost_consumed", dc.pad.CostConsumed,
	)

	return delegateOutput{
		Status:    "success",
		Findings:  findings,
		Duration:  duration.String(),
		ToolCalls: toolCalls,
		ToolLog:   toolLog,
	}, nil
}

// runSubAgent creates a temporary ADK agent, runs it, and captures output + tool log.
func (dc *delegateConfig) runSubAgent(agentType, instruction string, tools []tool.Tool, prompt string) (string, int, []SubToolLog, error) {
	adkModel, err := dc.modelFactory()
	if err != nil {
		return "", 0, nil, fmt.Errorf("create model: %w", err)
	}

	subAgent, err := llmagent.New(llmagent.Config{
		Name:        "commit0-" + agentType,
		Model:       adkModel,
		Description: "Specialized " + agentType + " agent for commit0 code analysis",
		Instruction: instruction,
		Tools:       tools,
	})
	if err != nil {
		return "", 0, nil, fmt.Errorf("create sub-agent: %w", err)
	}

	subRunner, err := runner.New(runner.Config{
		AppName:           "commit0-sub",
		Agent:             subAgent,
		SessionService:    session.InMemoryService(),
		AutoCreateSession: true,
	})
	if err != nil {
		return "", 0, nil, fmt.Errorf("create sub-runner: %w", err)
	}

	// Run with timeout.
	subCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sessionID := fmt.Sprintf("sub-%s-%d", agentType, time.Now().UnixNano())
	msg := genai.NewContentFromText(prompt, genai.RoleUser)

	var findings strings.Builder
	toolCalls := 0
	var toolLog []SubToolLog
	var currentToolStart time.Time
	var currentToolName string

	for event, err := range subRunner.Run(subCtx, "analyst", sessionID, msg, agent.RunConfig{}) {
		if err != nil {
			if subCtx.Err() != nil {
				partial := findings.String()
				if partial == "" {
					partial = "TIMEOUT: Sub-agent timed out after 60s."
				}
				return partial, toolCalls, toolLog, nil
			}
			return findings.String(), toolCalls, toolLog, err
		}

		if event == nil || event.Content == nil {
			continue
		}

		for _, part := range event.Content.Parts {
			if part.FunctionCall != nil {
				toolCalls++
				currentToolName = part.FunctionCall.Name
				currentToolStart = time.Now()
			}
			if part.FunctionResponse != nil {
				dur := time.Since(currentToolStart)
				resultSize := 0
				if part.FunctionResponse.Response != nil {
					if rc, ok := part.FunctionResponse.Response["result_count"]; ok {
						if n, ok := rc.(float64); ok {
							resultSize = int(n)
						}
					}
				}
				// Use the response's own name (more reliable than tracking currentToolName).
				name := part.FunctionResponse.Name
				if name == "" {
					name = currentToolName
				}
				toolLog = append(toolLog, SubToolLog{
					Name:       name,
					DurationMs: dur.Milliseconds(),
					ResultSize: resultSize,
				})
			}
			if part.Text != "" {
				findings.WriteString(part.Text)
			}
		}
	}

	return findings.String(), toolCalls, toolLog, nil
}

// toolsForType returns the tool subset for a given sub-agent type.
func (dc *delegateConfig) toolsForType(agentType string) ([]tool.Tool, error) {
	// Build all tools first, then select subset.
	allTools, err := BuildTools(
		dc.querySvc, dc.traceSvc, dc.blastSvc,
		dc.flowSvc, dc.tempSvc, dc.rootCauseSvc,
		dc.store, dc.gitWalker, dc.explainer,
	)
	if err != nil {
		return nil, fmt.Errorf("build tools: %w", err)
	}

	// Index by name.
	toolMap := make(map[string]tool.Tool)
	for _, t := range allTools {
		toolMap[t.Name()] = t
	}

	// Select subset.
	var names []string
	switch agentType {
	case "search":
		names = []string{"search_code", "lookup_node"}
	case "trace":
		names = []string{"trace_calls", "get_neighborhood", "flow_trace"}
	case "security":
		names = []string{"search_code", "flow_trace", "blast_radius", "temporal_query"}
	case "deep_dive":
		names = []string{"lookup_node", "get_neighborhood", "temporal_query", "analyze_commit_diff"}
	default:
		return allTools, nil // fallback: all tools
	}

	var subset []tool.Tool
	for _, name := range names {
		if t, ok := toolMap[name]; ok {
			subset = append(subset, t)
		}
	}
	return subset, nil
}
