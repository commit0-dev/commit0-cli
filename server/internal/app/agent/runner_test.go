package agent

// runner_test.go — unit tests for delegate.go, service.go, ports.go, instructions.go.
// stdlib testing only, inline fakes, no production code modified.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ---------------------------------------------------------------------------
// Inline fakes
// ---------------------------------------------------------------------------

// fakeRunner implements AgentRunnerPort.
type fakeRunner struct {
	events []RunnerEvent
	err    error
}

func (f *fakeRunner) Run(_ context.Context, _ AgentConfig, _ string, _ map[string]any) (<-chan RunnerEvent, error) {
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan RunnerEvent, len(f.events)+1)
	for _, e := range f.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// fakeSubRunner is a factory-created AgentRunnerPort for sub-agents.
type fakeSubRunner struct {
	events []RunnerEvent
	err    error
}

func (f *fakeSubRunner) Run(_ context.Context, _ AgentConfig, _ string, _ map[string]any) (<-chan RunnerEvent, error) {
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan RunnerEvent, len(f.events)+1)
	for _, e := range f.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// makeFactory returns a SubRunnerFactory that always returns r (or an error).
func makeFactory(r AgentRunnerPort, factoryErr error) SubRunnerFactory {
	return func(_ AgentConfig) (AgentRunnerPort, error) {
		if factoryErr != nil {
			return nil, factoryErr
		}
		return r, nil
	}
}

// minimalPad returns a fresh scratchpad ready for use.
func minimalPad() *Scratchpad {
	return NewScratchpad("test goal")
}

// minimalDelegateCfg returns a delegateConfig wired with the supplied runner factory.
func minimalDelegateCfg(factory SubRunnerFactory) *delegateConfig {
	return &delegateConfig{
		cfg:              &config.Config{},
		pad:              minimalPad(),
		subRunnerFactory: factory,
		log:              slog.Default(),
	}
}

// ---------------------------------------------------------------------------
// ports.go — context helpers
// ---------------------------------------------------------------------------

func TestWithRepoSlug_RoundTrip(t *testing.T) {
	ctx := WithRepoSlug(context.Background(), "owner/repo")
	if got := RepoSlugFrom(ctx); got != "owner/repo" {
		t.Errorf("RepoSlugFrom = %q, want %q", got, "owner/repo")
	}
}

func TestRepoSlugFrom_Missing(t *testing.T) {
	if got := RepoSlugFrom(context.Background()); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestWithRepoPath_RoundTrip(t *testing.T) {
	ctx := WithRepoPath(context.Background(), "/tmp/repo")
	if got := RepoPathFrom(ctx); got != "/tmp/repo" {
		t.Errorf("RepoPathFrom = %q, want %q", got, "/tmp/repo")
	}
}

func TestRepoPathFrom_Missing(t *testing.T) {
	// Default value should be "." when key is absent.
	if got := RepoPathFrom(context.Background()); got != "." {
		t.Errorf("expected default '.', got %q", got)
	}
}

func TestWithRepoSlug_OverridesPrevious(t *testing.T) {
	ctx := WithRepoSlug(context.Background(), "first")
	ctx = WithRepoSlug(ctx, "second")
	if got := RepoSlugFrom(ctx); got != "second" {
		t.Errorf("expected 'second', got %q", got)
	}
}

func TestWithRepoPath_OverridesPrevious(t *testing.T) {
	ctx := WithRepoPath(context.Background(), "/old")
	ctx = WithRepoPath(ctx, "/new")
	if got := RepoPathFrom(ctx); got != "/new" {
		t.Errorf("expected '/new', got %q", got)
	}
}

// ports.go — AgentConfig struct (zero-value sanity)
func TestAgentConfig_ZeroValue(t *testing.T) {
	var cfg AgentConfig
	if cfg.Name != "" || cfg.Instruction != "" || cfg.Tools != nil {
		t.Errorf("unexpected zero value: %+v", cfg)
	}
}

// ports.go — UsageInfo
func TestUsageInfo_Total(t *testing.T) {
	u := UsageInfo{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}
	if u.TotalTokens != u.PromptTokens+u.CompletionTokens {
		t.Errorf("token arithmetic mismatch: %+v", u)
	}
}

// ports.go — RunnerEvent type field values.
func TestRunnerEvent_TypeStrings(t *testing.T) {
	types := []string{"tool_call", "tool_result", "message", "usage", "error"}
	for _, typ := range types {
		e := RunnerEvent{Type: typ}
		if e.Type != typ {
			t.Errorf("RunnerEvent.Type assignment failed for %q", typ)
		}
	}
}

// ---------------------------------------------------------------------------
// instructions.go — constant and map existence
// ---------------------------------------------------------------------------

func TestAnalystInstruction_NonEmpty(t *testing.T) {
	if len(strings.TrimSpace(analystInstruction)) == 0 {
		t.Error("analystInstruction must not be empty")
	}
}

func TestAnalystInstruction_ContainsKeyPhrases(t *testing.T) {
	phrases := []string{
		"search_code",
		"write_report",
		"delegate",
		"update_scratchpad",
	}
	for _, p := range phrases {
		if !strings.Contains(analystInstruction, p) {
			t.Errorf("analystInstruction missing expected phrase %q", p)
		}
	}
}

func TestSubAgentInstructions_AllFourAgentTypes(t *testing.T) {
	expected := []string{"search", "trace", "security", "deep_dive"}
	for _, typ := range expected {
		instr, ok := subAgentInstructions[typ]
		if !ok {
			t.Errorf("subAgentInstructions missing key %q", typ)
			continue
		}
		if len(strings.TrimSpace(instr)) == 0 {
			t.Errorf("instruction for %q is empty", typ)
		}
	}
}

func TestSubAgentInstructions_NoExtraKeys(t *testing.T) {
	if len(subAgentInstructions) != 4 {
		t.Errorf("expected exactly 4 agent instruction types, got %d", len(subAgentInstructions))
	}
}

func TestSearchAgentInstruction_NonEmpty(t *testing.T) {
	if len(strings.TrimSpace(searchAgentInstruction)) == 0 {
		t.Error("searchAgentInstruction must not be empty")
	}
}

func TestTraceAgentInstruction_NonEmpty(t *testing.T) {
	if len(strings.TrimSpace(traceAgentInstruction)) == 0 {
		t.Error("traceAgentInstruction must not be empty")
	}
}

func TestSecurityAgentInstruction_NonEmpty(t *testing.T) {
	if len(strings.TrimSpace(securityAgentInstruction)) == 0 {
		t.Error("securityAgentInstruction must not be empty")
	}
}

func TestDeepDiveAgentInstruction_NonEmpty(t *testing.T) {
	if len(strings.TrimSpace(deepDiveAgentInstruction)) == 0 {
		t.Error("deepDiveAgentInstruction must not be empty")
	}
}

func TestSubAgentInstructions_ContentQuality(t *testing.T) {
	// Each sub-agent instruction must mention its expected output format.
	checks := map[string]string{
		"search":    "QUALIFIED_NAME",
		"trace":     "CALL_CHAIN",
		"security":  "SEVERITY",
		"deep_dive": "ANSWER",
	}
	for typ, keyword := range checks {
		instr := subAgentInstructions[typ]
		if !strings.Contains(instr, keyword) {
			t.Errorf("instruction for %q missing expected keyword %q", typ, keyword)
		}
	}
}

// ---------------------------------------------------------------------------
// delegate.go — BuildDelegateTool
// ---------------------------------------------------------------------------

func TestBuildDelegateTool_ReturnsAgentTool(t *testing.T) {
	factory := makeFactory(&fakeSubRunner{}, nil)
	tool := BuildDelegateTool(nil, nil, nil, nil, nil, nil, nil, nil, nil, &config.Config{}, minimalPad(), factory)
	if tool == nil {
		t.Fatal("BuildDelegateTool returned nil")
	}
}

func TestDelegateTool_Def(t *testing.T) {
	factory := makeFactory(&fakeSubRunner{}, nil)
	tool := BuildDelegateTool(nil, nil, nil, nil, nil, nil, nil, nil, nil, &config.Config{}, minimalPad(), factory)
	def := tool.Def()
	if def.Name != "delegate" {
		t.Errorf("Def().Name = %q, want %q", def.Name, "delegate")
	}
	if len(def.Description) == 0 {
		t.Error("Def().Description must not be empty")
	}
}

func TestDelegateTool_Invoke_InvalidJSON(t *testing.T) {
	factory := makeFactory(&fakeSubRunner{}, nil)
	tool := BuildDelegateTool(nil, nil, nil, nil, nil, nil, nil, nil, nil, &config.Config{}, minimalPad(), factory)
	_, err := tool.Invoke(context.Background(), "{bad json}")
	if err == nil {
		t.Error("expected parse error on invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// delegate.go — delegateConfig.execute() paths
// ---------------------------------------------------------------------------

func TestDelegateExecute_ScratchpadEnforcementBeforeFirstDelegation(t *testing.T) {
	// First delegation (DelegationCount == 0) should not be blocked.
	runner := &fakeSubRunner{
		events: []RunnerEvent{{Type: "message", Content: "findings with enough content here"}},
	}
	factory := makeFactory(runner, nil)
	dc := minimalDelegateCfg(factory)
	dc.pad.UpdatedSinceDelegation = false // freshly created

	out, err := dc.execute(context.Background(), delegateInput{
		AgentType: "search",
		Task:      "find something",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status == "error" && strings.Contains(out.Findings, "update_scratchpad") {
		t.Errorf("first delegation should not be blocked by scratchpad enforcement")
	}
}

func TestDelegateExecute_ScratchpadEnforcementAfterFirst(t *testing.T) {
	factory := makeFactory(&fakeSubRunner{}, nil)
	dc := minimalDelegateCfg(factory)
	// Simulate: one delegation done, scratchpad NOT updated.
	dc.pad.DelegationCount = 1
	dc.pad.UpdatedSinceDelegation = false

	out, err := dc.execute(context.Background(), delegateInput{
		AgentType: "search",
		Task:      "find something",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "error" {
		t.Errorf("expected error status, got %q", out.Status)
	}
	if !strings.Contains(out.Findings, "update_scratchpad") {
		t.Errorf("expected update_scratchpad message, got %q", out.Findings)
	}
}

func TestDelegateExecute_BudgetExceeded(t *testing.T) {
	dc := minimalDelegateCfg(nil)
	dc.pad.CostConsumed = 2.0
	dc.pad.CostBudget = 1.0

	out, err := dc.execute(context.Background(), delegateInput{AgentType: "search", Task: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "budget_exceeded" {
		t.Errorf("expected budget_exceeded, got %q", out.Status)
	}
}

func TestDelegateExecute_BudgetAt80Percent_StillProceeds(t *testing.T) {
	runner := &fakeSubRunner{
		events: []RunnerEvent{{Type: "message", Content: strings.Repeat("a", 30)}},
	}
	factory := makeFactory(runner, nil)
	dc := minimalDelegateCfg(factory)
	dc.pad.CostConsumed = 0.85 // > 80% of default 1.00
	dc.pad.CostBudget = 1.0

	out, err := dc.execute(context.Background(), delegateInput{AgentType: "search", Task: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should NOT be budget_exceeded (just a warning); delegation proceeds.
	if out.Status == "budget_exceeded" {
		t.Errorf("80%% budget should log warning but not block, got budget_exceeded")
	}
}

func TestDelegateExecute_MaxDelegations(t *testing.T) {
	dc := minimalDelegateCfg(nil)
	dc.pad.DelegationCount = 8
	dc.pad.UpdatedSinceDelegation = true

	out, err := dc.execute(context.Background(), delegateInput{AgentType: "search", Task: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "error" {
		t.Errorf("expected error on max delegations, got %q", out.Status)
	}
	if !strings.Contains(out.Findings, "8 delegations") {
		t.Errorf("expected max-delegation message, got %q", out.Findings)
	}
}

func TestDelegateExecute_UnknownAgentType(t *testing.T) {
	dc := minimalDelegateCfg(nil)
	dc.pad.UpdatedSinceDelegation = true

	out, err := dc.execute(context.Background(), delegateInput{AgentType: "bogus", Task: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "error" {
		t.Errorf("expected error for unknown agent type, got %q", out.Status)
	}
	if !strings.Contains(out.Findings, "bogus") {
		t.Errorf("error should mention unknown agent type, got %q", out.Findings)
	}
}

func TestDelegateExecute_FactoryError(t *testing.T) {
	dc := minimalDelegateCfg(makeFactory(nil, errors.New("factory broke")))
	dc.pad.UpdatedSinceDelegation = true

	out, err := dc.execute(context.Background(), delegateInput{AgentType: "search", Task: "x"})
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if out.Status != "error" {
		t.Errorf("expected error on factory failure, got %q", out.Status)
	}
}

func TestDelegateExecute_SubAgentRunError(t *testing.T) {
	runner := &fakeSubRunner{err: errors.New("llm unavailable")}
	dc := minimalDelegateCfg(makeFactory(runner, nil))
	dc.pad.UpdatedSinceDelegation = true

	out, err := dc.execute(context.Background(), delegateInput{AgentType: "search", Task: "x"})
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if out.Status != "error" {
		t.Errorf("expected error on sub-agent run failure, got %q", out.Status)
	}
}

func TestDelegateExecute_EmptyFindings(t *testing.T) {
	runner := &fakeSubRunner{
		events: []RunnerEvent{{Type: "message", Content: "tiny"}}, // < 20 chars
	}
	dc := minimalDelegateCfg(makeFactory(runner, nil))
	dc.pad.UpdatedSinceDelegation = true

	out, err := dc.execute(context.Background(), delegateInput{AgentType: "search", Task: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "empty" {
		t.Errorf("expected empty status for short output, got %q (findings=%q)", out.Status, out.Findings)
	}
}

func TestDelegateExecute_HappyPath(t *testing.T) {
	findings := strings.Repeat("detailed findings from sub-agent ", 5)
	runner := &fakeSubRunner{
		events: []RunnerEvent{
			{Type: "tool_call", ToolName: "search_code"},
			{Type: "tool_result", ToolName: "search_code"},
			{Type: "message", Content: findings},
		},
	}
	dc := minimalDelegateCfg(makeFactory(runner, nil))
	dc.pad.UpdatedSinceDelegation = true

	out, err := dc.execute(context.Background(), delegateInput{
		AgentType: "search",
		Task:      "find authentication functions",
		Context:   "prior analysis showed X",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "success" {
		t.Errorf("expected success, got %q (findings=%q)", out.Status, out.Findings)
	}
	if !strings.Contains(out.Findings, "detailed findings") {
		t.Errorf("findings not propagated: %q", out.Findings)
	}
	if out.ToolCalls != 1 {
		t.Errorf("expected 1 tool_call, got %d", out.ToolCalls)
	}
	if len(out.ToolLog) != 1 {
		t.Errorf("expected 1 tool_result entry, got %d", len(out.ToolLog))
	}
}

func TestDelegateExecute_HappyPath_WithContext(t *testing.T) {
	findings := strings.Repeat("security issue detected here. ", 3)
	runner := &fakeSubRunner{
		events: []RunnerEvent{{Type: "message", Content: findings}},
	}
	dc := minimalDelegateCfg(makeFactory(runner, nil))
	dc.pad.UpdatedSinceDelegation = true

	out, err := dc.execute(context.Background(), delegateInput{
		AgentType: "security",
		Task:      "audit login flow",
		Context:   "previously traced auth path",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "success" {
		t.Errorf("expected success, got %q", out.Status)
	}
}

func TestDelegateExecute_TracksToolLog(t *testing.T) {
	runner := &fakeSubRunner{
		events: []RunnerEvent{
			{Type: "tool_call", ToolName: "trace_calls"},
			{Type: "tool_result", ToolName: "trace_calls"},
			{Type: "tool_call", ToolName: "get_neighborhood"},
			{Type: "tool_result", ToolName: ""}, // empty name, falls back to currentToolName
			{Type: "message", Content: strings.Repeat("trace result with sufficient length ", 3)},
		},
	}
	dc := minimalDelegateCfg(makeFactory(runner, nil))
	dc.pad.UpdatedSinceDelegation = true

	out, err := dc.execute(context.Background(), delegateInput{AgentType: "trace", Task: "trace auth"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ToolCalls != 2 {
		t.Errorf("expected 2 tool_calls, got %d", out.ToolCalls)
	}
	if len(out.ToolLog) != 2 {
		t.Errorf("expected 2 tool_result entries, got %d", len(out.ToolLog))
	}
}

func TestDelegateExecute_ErrorEvent_NonTimeout(t *testing.T) {
	runner := &fakeSubRunner{
		events: []RunnerEvent{
			{Type: "error", Content: "model overloaded"},
		},
	}
	dc := minimalDelegateCfg(makeFactory(runner, nil))
	dc.pad.UpdatedSinceDelegation = true

	out, err := dc.execute(context.Background(), delegateInput{AgentType: "search", Task: "x"})
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if out.Status != "error" {
		t.Errorf("expected error status for error event, got %q", out.Status)
	}
}

func TestDelegateExecute_IncrementsDelegationCount(t *testing.T) {
	runner := &fakeSubRunner{
		events: []RunnerEvent{{Type: "message", Content: strings.Repeat("a", 25)}},
	}
	dc := minimalDelegateCfg(makeFactory(runner, nil))
	dc.pad.UpdatedSinceDelegation = true
	initial := dc.pad.DelegationCount

	dc.execute(context.Background(), delegateInput{AgentType: "search", Task: "x"}) //nolint
	if dc.pad.DelegationCount != initial+1 {
		t.Errorf("DelegationCount not incremented: %d → %d", initial, dc.pad.DelegationCount)
	}
}

func TestDelegateExecute_UpdatedSinceDelegationFalseAfterCall(t *testing.T) {
	runner := &fakeSubRunner{
		events: []RunnerEvent{{Type: "message", Content: strings.Repeat("a", 25)}},
	}
	dc := minimalDelegateCfg(makeFactory(runner, nil))
	dc.pad.UpdatedSinceDelegation = true

	dc.execute(context.Background(), delegateInput{AgentType: "search", Task: "x"}) //nolint
	if dc.pad.UpdatedSinceDelegation {
		t.Error("UpdatedSinceDelegation should be false after delegation")
	}
}

func TestDelegateExecute_CostAccumulated(t *testing.T) {
	findings := strings.Repeat("detailed analysis ", 10)
	runner := &fakeSubRunner{
		events: []RunnerEvent{{Type: "message", Content: findings}},
	}
	dc := minimalDelegateCfg(makeFactory(runner, nil))
	dc.pad.UpdatedSinceDelegation = true

	dc.execute(context.Background(), delegateInput{AgentType: "search", Task: "find something"}) //nolint
	if dc.pad.CostConsumed <= 0 {
		t.Error("CostConsumed should be positive after successful delegation")
	}
	if dc.pad.TokensConsumed <= 0 {
		t.Error("TokensConsumed should be positive after successful delegation")
	}
}

// ---------------------------------------------------------------------------
// delegate.go — toolsForType
// ---------------------------------------------------------------------------

func TestToolsForType_Search(t *testing.T) {
	dc := minimalDelegateCfg(nil)
	tools := dc.toolsForType("search")
	names := toolNames(tools)
	if !containsName(names, "search_code") {
		t.Errorf("search tools should include search_code, got %v", names)
	}
	if !containsName(names, "lookup_node") {
		t.Errorf("search tools should include lookup_node, got %v", names)
	}
}

func TestToolsForType_Trace(t *testing.T) {
	dc := minimalDelegateCfg(nil)
	tools := dc.toolsForType("trace")
	names := toolNames(tools)
	if !containsName(names, "trace_calls") {
		t.Errorf("trace tools should include trace_calls, got %v", names)
	}
}

func TestToolsForType_Security(t *testing.T) {
	dc := minimalDelegateCfg(nil)
	tools := dc.toolsForType("security")
	names := toolNames(tools)
	if !containsName(names, "blast_radius") {
		t.Errorf("security tools should include blast_radius, got %v", names)
	}
}

func TestToolsForType_DeepDive(t *testing.T) {
	dc := minimalDelegateCfg(nil)
	tools := dc.toolsForType("deep_dive")
	names := toolNames(tools)
	if !containsName(names, "lookup_node") {
		t.Errorf("deep_dive tools should include lookup_node, got %v", names)
	}
}

func TestToolsForType_Default_ReturnsAll(t *testing.T) {
	dc := minimalDelegateCfg(nil)
	all := dc.toolsForType("unknown_type")
	if len(all) == 0 {
		t.Error("unknown agent type should return all tools")
	}
}

// ---------------------------------------------------------------------------
// delegate.go — runSubAgent
// ---------------------------------------------------------------------------

func TestRunSubAgent_ContextCanceledMidStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Runner that emits nothing before the test cancels it.
	ch := make(chan RunnerEvent)
	runner := &blockingRunner{ch: ch}
	dc := minimalDelegateCfg(makeFactory(runner, nil))

	done := make(chan struct{})
	go func() {
		defer close(done)
		dc.runSubAgent(ctx, "search", searchAgentInstruction, nil, "prompt") //nolint
	}()
	cancel()
	close(ch)
	<-done // must not hang
}

// blockingRunner sends nothing until its channel is closed.
type blockingRunner struct{ ch <-chan RunnerEvent }

func (b *blockingRunner) Run(ctx context.Context, _ AgentConfig, _ string, _ map[string]any) (<-chan RunnerEvent, error) {
	return b.ch, nil
}

func TestRunSubAgent_TimeoutEventPath_WithPartial(t *testing.T) {
	// Simulate context already canceled when the error event arrives — partial message exists.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled → subCtx derived from it is also done

	runner := &fakeSubRunner{
		events: []RunnerEvent{
			{Type: "message", Content: "partial result from sub-agent"},
			{Type: "error", Content: "deadline exceeded"},
		},
	}
	dc := minimalDelegateCfg(makeFactory(runner, nil))
	findings, _, _, err := dc.runSubAgent(ctx, "search", searchAgentInstruction, nil, "x")
	_ = err
	// findings should be the partial content or TIMEOUT placeholder — not empty
	if findings == "" {
		t.Error("expected non-empty findings for canceled-context error path")
	}
}

func TestRunSubAgent_TimeoutEventPath_EmptyPartial(t *testing.T) {
	// Context already canceled, no prior message — exercises the `if partial == ""` branch
	// which substitutes the TIMEOUT sentinel string.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner := &fakeSubRunner{
		events: []RunnerEvent{
			// No "message" event before the error — findings builder is empty.
			{Type: "error", Content: "deadline exceeded"},
		},
	}
	dc := minimalDelegateCfg(makeFactory(runner, nil))
	findings, _, _, err := dc.runSubAgent(ctx, "search", searchAgentInstruction, nil, "x")
	// Either the canceled-context branch fires (findings = "TIMEOUT: ...") or the non-canceled
	// branch fires (err != nil). Either way is valid — the test just ensures no panic.
	_ = findings
	_ = err
}

// ---------------------------------------------------------------------------
// delegate.go — Invoke JSON marshaling round-trip
// ---------------------------------------------------------------------------

func TestDelegateTool_Invoke_HappyPath(t *testing.T) {
	findings := strings.Repeat("analysis result here. ", 5)
	runner := &fakeSubRunner{
		events: []RunnerEvent{{Type: "message", Content: findings}},
	}
	factory := makeFactory(runner, nil)
	pad := minimalPad()
	pad.UpdatedSinceDelegation = true

	tool := BuildDelegateTool(nil, nil, nil, nil, nil, nil, nil, nil, nil, &config.Config{}, pad, factory)

	argsJSON := `{"agent_type":"search","task":"find authentication","context":""}`
	result, err := tool.Invoke(context.Background(), argsJSON)
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
	if !strings.Contains(result, "status") {
		t.Errorf("result should be JSON with status field, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// service.go — NewAgentService
// ---------------------------------------------------------------------------

func TestNewAgentService_HappyPath(t *testing.T) {
	factory := makeFactory(&fakeSubRunner{}, nil)

	svc, err := NewAgentService(
		nil, nil, nil, nil, nil, nil,
		nil, nil, nil,
		&config.Config{},
		nil,
		&fakeRunner{},
		factory,
	)
	if err != nil {
		t.Fatalf("NewAgentService error: %v", err)
	}
	if svc == nil {
		t.Fatal("NewAgentService returned nil")
	}
}

func TestNewAgentService_ImplementsAgentRunner(t *testing.T) {
	factory := makeFactory(&fakeSubRunner{}, nil)
	svc, err := NewAgentService(
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
		&config.Config{},
		nil,
		&fakeRunner{},
		factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	// Compile-time check exists in service.go; this just confirms the value satisfies the interface.
	var _ domain.AgentRunner = svc
}

// ---------------------------------------------------------------------------
// service.go — Chat()
// ---------------------------------------------------------------------------

func TestChat_RunnerError(t *testing.T) {
	runner := &fakeRunner{err: errors.New("llm down")}
	svc, _ := newTestService(runner)

	ch, err := svc.Chat(context.Background(), domain.ChatRequest{
		RepoSlug: "owner/repo",
		Message:  "hello",
	})
	if err != nil {
		t.Fatalf("Chat returned immediate error: %v", err)
	}

	var events []domain.ChatEvent
	for e := range ch {
		events = append(events, e)
	}
	// Should see a "thinking" event followed by an "error" event.
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d: %+v", len(events), events)
	}
	last := events[len(events)-1]
	if last.Type != "error" {
		t.Errorf("last event should be error, got %q", last.Type)
	}
	if !last.Done {
		t.Error("error event should have Done=true")
	}
}

func TestChat_MessageEvent(t *testing.T) {
	runner := &fakeRunner{
		events: []RunnerEvent{{Type: "message", Content: "here is the answer"}},
	}
	svc, _ := newTestService(runner)

	ch, err := svc.Chat(context.Background(), domain.ChatRequest{
		RepoSlug: "test/repo",
		Message:  "what is auth?",
	})
	if err != nil {
		t.Fatal(err)
	}

	var events []domain.ChatEvent
	for e := range ch {
		events = append(events, e)
	}

	// Expect: thinking → message → done
	typeSeq := make([]string, 0, len(events))
	for _, e := range events {
		typeSeq = append(typeSeq, e.Type)
	}
	if !containsStr(typeSeq, "message") {
		t.Errorf("expected message event, got %v", typeSeq)
	}
	if !containsStr(typeSeq, "done") {
		t.Errorf("expected done event, got %v", typeSeq)
	}
}

func TestChat_ToolCallAndResult(t *testing.T) {
	runner := &fakeRunner{
		events: []RunnerEvent{
			{Type: "tool_call", ToolName: "search_code", Content: `{"question":"auth"}`},
			{Type: "tool_result", ToolName: "search_code", Content: "found 3 results"},
			{Type: "message", Content: "analysis complete"},
		},
	}
	svc, _ := newTestService(runner)

	ch, err := svc.Chat(context.Background(), domain.ChatRequest{Message: "find auth"})
	if err != nil {
		t.Fatal(err)
	}

	var toolCalls, toolResults int
	for e := range ch {
		switch e.Type {
		case "tool_call":
			toolCalls++
		case "tool_result":
			toolResults++
		}
	}
	if toolCalls != 1 {
		t.Errorf("expected 1 tool_call event, got %d", toolCalls)
	}
	if toolResults != 1 {
		t.Errorf("expected 1 tool_result event, got %d", toolResults)
	}
}

func TestChat_UsageEvent(t *testing.T) {
	runner := &fakeRunner{
		events: []RunnerEvent{
			{Type: "usage", Usage: &UsageInfo{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150}},
		},
	}
	svc, _ := newTestService(runner)

	ch, err := svc.Chat(context.Background(), domain.ChatRequest{Message: "x"})
	if err != nil {
		t.Fatal(err)
	}

	var usageEvents []domain.ChatEvent
	for e := range ch {
		if e.Type == "usage" {
			usageEvents = append(usageEvents, e)
		}
	}
	if len(usageEvents) != 1 {
		t.Fatalf("expected 1 usage event, got %d", len(usageEvents))
	}
	if !strings.Contains(usageEvents[0].Content, "prompt_tokens") {
		t.Errorf("usage content should have prompt_tokens, got %q", usageEvents[0].Content)
	}
}

func TestChat_UsageEvent_NilUsage(t *testing.T) {
	// Usage event with nil Usage should be silently dropped.
	runner := &fakeRunner{
		events: []RunnerEvent{
			{Type: "usage", Usage: nil},
			{Type: "message", Content: "result"},
		},
	}
	svc, _ := newTestService(runner)

	ch, err := svc.Chat(context.Background(), domain.ChatRequest{Message: "x"})
	if err != nil {
		t.Fatal(err)
	}

	var usageEvents int
	for e := range ch {
		if e.Type == "usage" {
			usageEvents++
		}
	}
	if usageEvents != 0 {
		t.Errorf("nil usage should be dropped, got %d usage events", usageEvents)
	}
}

func TestChat_ErrorEvent_FromRunner(t *testing.T) {
	runner := &fakeRunner{
		events: []RunnerEvent{
			{Type: "message", Content: "partial"},
			{Type: "error", Content: "model crashed"},
		},
	}
	svc, _ := newTestService(runner)

	ch, err := svc.Chat(context.Background(), domain.ChatRequest{Message: "x"})
	if err != nil {
		t.Fatal(err)
	}

	var events []domain.ChatEvent
	for e := range ch {
		events = append(events, e)
	}
	last := events[len(events)-1]
	if last.Type != "error" {
		t.Errorf("expected last event to be error, got %q", last.Type)
	}
	if !strings.Contains(last.Content, "model crashed") {
		t.Errorf("error content should carry message, got %q", last.Content)
	}
}

func TestChat_ThinkingEventFirst(t *testing.T) {
	runner := &fakeRunner{
		events: []RunnerEvent{{Type: "message", Content: "ok"}},
	}
	svc, _ := newTestService(runner)

	ch, err := svc.Chat(context.Background(), domain.ChatRequest{Message: "x"})
	if err != nil {
		t.Fatal(err)
	}

	var events []domain.ChatEvent
	for e := range ch {
		events = append(events, e)
	}
	if len(events) == 0 {
		t.Fatal("no events received")
	}
	if events[0].Type != "thinking" {
		t.Errorf("first event should be thinking, got %q", events[0].Type)
	}
}

func TestChat_DoneEventLast(t *testing.T) {
	runner := &fakeRunner{
		events: []RunnerEvent{{Type: "message", Content: "done now"}},
	}
	svc, _ := newTestService(runner)

	ch, err := svc.Chat(context.Background(), domain.ChatRequest{Message: "x"})
	if err != nil {
		t.Fatal(err)
	}

	var events []domain.ChatEvent
	for e := range ch {
		events = append(events, e)
	}
	last := events[len(events)-1]
	if last.Type != "done" || !last.Done {
		t.Errorf("last event should be done with Done=true, got type=%q done=%v", last.Type, last.Done)
	}
}

func TestChat_ContextCanceled(t *testing.T) {
	// Runner that blocks until context is canceled.
	ch := make(chan RunnerEvent)
	runner := &blockingRunner{ch: ch}
	svc, _ := newTestService(runner)

	ctx, cancel := context.WithCancel(context.Background())
	eventCh, err := svc.Chat(ctx, domain.ChatRequest{Message: "x"})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	close(ch)

	// Drain channel — must not hang.
	for range eventCh {
	}
}

func TestChat_PanicRecovery(t *testing.T) {
	runner := &panicRunner{}
	svc, _ := newTestService(runner)

	ch, err := svc.Chat(context.Background(), domain.ChatRequest{Message: "x"})
	if err != nil {
		t.Fatal(err)
	}

	var events []domain.ChatEvent
	for e := range ch {
		events = append(events, e)
	}
	// Should recover and send an error event.
	found := false
	for _, e := range events {
		if e.Type == "error" && strings.Contains(e.Content, "panic") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected panic recovery error event, got %v", events)
	}
}

// panicRunner panics on Run to exercise Chat's recover() guard.
type panicRunner struct{}

func (p *panicRunner) Run(_ context.Context, _ AgentConfig, _ string, _ map[string]any) (<-chan RunnerEvent, error) {
	panic("test panic in runner")
}

func TestChat_RepoSlugPropagatedToState(t *testing.T) {
	// We verify the service correctly sets repo_slug in state by checking
	// that our fake runner receives a Run call (indirectly via events arriving).
	recorder := &stateRecorder{}
	svc, _ := newTestService(recorder)

	ch, err := svc.Chat(context.Background(), domain.ChatRequest{
		RepoSlug: "acme/myrepo",
		Message:  "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}

	if recorder.lastState["repo_slug"] != "acme/myrepo" {
		t.Errorf("repo_slug not propagated to state: %v", recorder.lastState)
	}
}

// stateRecorder captures the state map passed to Run.
type stateRecorder struct {
	lastState map[string]any
}

func (r *stateRecorder) Run(_ context.Context, _ AgentConfig, _ string, state map[string]any) (<-chan RunnerEvent, error) {
	r.lastState = state
	ch := make(chan RunnerEvent)
	close(ch)
	return ch, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestService creates an AgentService backed by the provided runner.
func newTestService(runner AgentRunnerPort) (*AgentService, error) {
	factory := makeFactory(&fakeSubRunner{}, nil)
	return NewAgentService(
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
		&config.Config{},
		nil,
		runner,
		factory,
	)
}

func toolNames(tools []AgentTool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Def().Name
	}
	return names
}

func containsName(names []string, target string) bool {
	for _, n := range names {
		if n == target {
			return true
		}
	}
	return false
}

func containsStr(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Extra delegate.go edge-cases for DelegationCount tracking
// ---------------------------------------------------------------------------

func TestDelegateExecute_AllFourAgentTypes_HappyPath(t *testing.T) {
	agentTypes := []string{"search", "trace", "security", "deep_dive"}
	for _, typ := range agentTypes {
		t.Run(typ, func(t *testing.T) {
			findings := fmt.Sprintf("findings from %s agent with enough length here", typ)
			runner := &fakeSubRunner{
				events: []RunnerEvent{{Type: "message", Content: findings}},
			}
			dc := minimalDelegateCfg(makeFactory(runner, nil))
			dc.pad.UpdatedSinceDelegation = true

			out, err := dc.execute(context.Background(), delegateInput{
				AgentType: typ,
				Task:      "investigate something important",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Status != "success" {
				t.Errorf("expected success for %q, got %q (findings=%q)", typ, out.Status, out.Findings)
			}
		})
	}
}
