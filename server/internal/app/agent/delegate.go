package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// delegateConfig holds services needed to create sub-agents.
type delegateConfig struct {
	querySvc        *app.QueryService
	traceSvc        *app.TraceService
	blastSvc        *app.BlastService
	flowSvc         *app.FieldFlowService
	tempSvc         *app.TemporalService
	rootCauseSvc    *app.RootCauseAnalysisService
	graph           domain.OpenCodeGraph
	gitWalker       domain.GitWalker
	explainer       domain.LLMExplainer
	cfg             *config.Config
	pad             *Scratchpad
	subRunnerFactory SubRunnerFactory
	log             *slog.Logger
}

// BuildDelegateTool creates the delegate tool for the Analyst agent.
func BuildDelegateTool(
	querySvc *app.QueryService,
	traceSvc *app.TraceService,
	blastSvc *app.BlastService,
	flowSvc *app.FieldFlowService,
	tempSvc *app.TemporalService,
	rootCauseSvc *app.RootCauseAnalysisService,
	graph domain.OpenCodeGraph,
	gitWalker domain.GitWalker,
	explainer domain.LLMExplainer,
	cfg *config.Config,
	pad *Scratchpad,
	subRunnerFactory SubRunnerFactory,
) AgentTool {
	return &delegateTool{
		dc: &delegateConfig{
			querySvc: querySvc, traceSvc: traceSvc, blastSvc: blastSvc,
			flowSvc: flowSvc, tempSvc: tempSvc, rootCauseSvc: rootCauseSvc,
			graph: graph, gitWalker: gitWalker, explainer: explainer,
			cfg: cfg, pad: pad, subRunnerFactory: subRunnerFactory,
			log: slog.Default().With("component", "delegate"),
		},
	}
}

type delegateTool struct{ dc *delegateConfig }

func (t *delegateTool) Def() ToolDef {
	return ToolDef{
		Name: "delegate",
		Description: "Delegate a focused investigation task to a specialized sub-agent. " +
			"Types: search (discovery), trace (structural), security (risks), deep_dive (code detail). " +
			"IMPORTANT: You MUST call update_scratchpad after receiving results from this tool.",
		InputExample: delegateInput{},
	}
}

func (t *delegateTool) Invoke(ctx context.Context, argsJSON string) (string, error) {
	var input delegateInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	out, err := t.dc.execute(ctx, input)
	if err != nil {
		return "", err
	}
	return marshalJSON(out)
}

type delegateInput struct {
	AgentType string `json:"agent_type"` // search, trace, security, deep_dive
	Task      string `json:"task"`       // what to investigate
	Context   string `json:"context"`    // prior findings to inform this delegation
}

type delegateOutput struct {
	Status    string       `json:"status"`
	Findings  string       `json:"findings"`
	Duration  string       `json:"duration"`
	ToolCalls int          `json:"tool_calls"`
	ToolLog   []SubToolLog `json:"tool_log"`
}

// SubToolLog records a single tool call made by a sub-agent.
type SubToolLog struct {
	Name       string `json:"name"`
	DurationMs int64  `json:"duration_ms"`
	ResultSize int    `json:"result_size"`
}

func (dc *delegateConfig) execute(ctx context.Context, input delegateInput) (delegateOutput, error) {
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
	subTools := dc.toolsForType(input.AgentType)

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
	findings, toolCalls, toolLog, err := dc.runSubAgent(ctx, input.AgentType, instruction, subTools, prompt)
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

	// Estimate cost.
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

// runSubAgent creates a temporary agent, runs it, and captures output + tool log.
func (dc *delegateConfig) runSubAgent(ctx context.Context, agentType, instruction string, tools []AgentTool, prompt string) (string, int, []SubToolLog, error) {
	config := AgentConfig{
		Name:        "commit0-" + agentType,
		Description: "Specialized " + agentType + " agent for commit0 code analysis",
		Instruction: instruction,
		Tools:       tools,
	}

	subRunner, err := dc.subRunnerFactory(config)
	if err != nil {
		return "", 0, nil, fmt.Errorf("create sub-runner: %w", err)
	}

	// Run with timeout.
	subCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Propagate repo context from parent.
	state := map[string]any{
		"repo_slug": RepoSlugFrom(ctx),
		"repo_path": RepoPathFrom(ctx),
	}

	events, err := subRunner.Run(subCtx, config, prompt, state)
	if err != nil {
		return "", 0, nil, fmt.Errorf("run sub-agent: %w", err)
	}

	var findings strings.Builder
	toolCalls := 0
	var toolLog []SubToolLog
	var currentToolStart time.Time
	var currentToolName string

	for event := range events {
		switch event.Type {
		case "tool_call":
			toolCalls++
			currentToolName = event.ToolName
			currentToolStart = time.Now()
		case "tool_result":
			dur := time.Since(currentToolStart)
			name := event.ToolName
			if name == "" {
				name = currentToolName
			}
			toolLog = append(toolLog, SubToolLog{
				Name:       name,
				DurationMs: dur.Milliseconds(),
			})
		case "message":
			findings.WriteString(event.Content)
		case "error":
			if subCtx.Err() != nil {
				partial := findings.String()
				if partial == "" {
					partial = "TIMEOUT: Sub-agent timed out after 30s."
				}
				return partial, toolCalls, toolLog, nil
			}
			return findings.String(), toolCalls, toolLog, fmt.Errorf("sub-agent: %s", event.Content)
		}
	}

	return findings.String(), toolCalls, toolLog, nil
}

// toolsForType returns the tool subset for a given sub-agent type.
func (dc *delegateConfig) toolsForType(agentType string) []AgentTool {
	allTools := BuildTools(
		dc.querySvc, dc.traceSvc, dc.blastSvc,
		dc.flowSvc, dc.tempSvc, dc.rootCauseSvc,
		dc.graph, dc.gitWalker, dc.explainer,
	)

	// Index by name.
	toolMap := make(map[string]AgentTool)
	for _, t := range allTools {
		toolMap[t.Def().Name] = t
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
		return allTools
	}

	var subset []AgentTool
	for _, name := range names {
		if t, ok := toolMap[name]; ok {
			subset = append(subset, t)
		}
	}
	return subset
}
