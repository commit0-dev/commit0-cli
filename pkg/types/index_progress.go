package types

import "time"

// IndexStage identifies a pipeline stage.
type IndexStage string

const (
	StageWalk      IndexStage = "walk"
	StageParse     IndexStage = "parse"
	StageSummarize IndexStage = "summarize"
	StageEmbed     IndexStage = "embed"
	StageStore     IndexStage = "store"
	StageReembed   IndexStage = "reembed"
	StageTemporal  IndexStage = "temporal"
	StageCleanup   IndexStage = "cleanup"
)

// StageStatus is the lifecycle state of a pipeline stage.
type StageStatus string

const (
	StatusPending   StageStatus = "pending"
	StatusRunning   StageStatus = "running"
	StatusCompleted StageStatus = "completed"
	StatusFailed    StageStatus = "failed"
	StatusSkipped   StageStatus = "skipped"
)

// StageError records a single error within a stage.
type StageError struct {
	File    string `json:"file,omitempty"`    // file path that caused the error
	Node    string `json:"node,omitempty"`    // node qualified name
	Message string `json:"message"`           // error description
	Stage   string `json:"stage"`             // which stage
}

// StageProgress tracks the state of one pipeline stage.
type StageProgress struct {
	Status          StageStatus  `json:"status"`
	ItemsDone       int          `json:"items_done"`
	ItemsTotal      int          `json:"items_total,omitempty"` // 0 = unknown
	Errors          []StageError `json:"errors,omitempty"`
	ErrorCount      int          `json:"error_count"`           // total (may exceed len(Errors) if capped)
	StartedAt       *time.Time   `json:"started_at,omitempty"`
	CompletedAt     *time.Time   `json:"completed_at,omitempty"`
	DurationMS      int64        `json:"duration_ms,omitempty"`
}

// IndexConfig captures the configuration used for this index run.
// Useful for debugging "why did it use Gemini instead of Ollama?"
type IndexConfig struct {
	EmbedProvider string `json:"embed_provider"` // gemini, voyage, ollama
	LLMProvider   string `json:"llm_provider"`   // gemini, openrouter, ollama
	EmbedModel    string `json:"embed_model"`    // model name
	LLMModel      string `json:"llm_model"`      // model name
	EmbedDim      int    `json:"embed_dim"`      // HNSW dimension
	BatchSize     int    `json:"batch_size"`
	Fast          bool   `json:"fast"`           // --fast flag (skip summarize + reembed)
	Reparse       bool   `json:"reparse"`        // --reparse flag
	Force         bool   `json:"force"`          // --force flag
}

// PipelineCoverage tracks the gap between what the AST parser extracted
// and what downstream stages actually processed. This is THE critical metric
// for debugging search quality and graph completeness.
type PipelineCoverage struct {
	// AST extraction totals (ground truth from tree-sitter)
	FilesWalked  int `json:"files_walked"`   // files discovered by walker
	FilesParsed  int `json:"files_parsed"`   // files successfully parsed (AST extracted)
	FilesSkipped int `json:"files_skipped"`  // files skipped (unchanged ContentHash)

	NodesExtracted int `json:"nodes_extracted"` // total nodes from AST (functions + classes + files + modules)
	EdgesExtracted int `json:"edges_extracted"` // total edges from AST (calls + imports + defines + data_flow + ...)

	// Downstream processing coverage
	NodesSummarized int `json:"nodes_summarized"` // nodes that got LLM Summary + Concepts
	NodesEmbedded   int `json:"nodes_embedded"`   // nodes that got embedding vectors
	NodesStored     int `json:"nodes_stored"`     // nodes written to DB
	EdgesStored     int `json:"edges_stored"`     // edges written to DB

	// Edge resolution (resolver quality)
	CallEdgesTotal    int `json:"call_edges_total"`    // total EdgeCalls extracted
	CallEdgesResolved int `json:"call_edges_resolved"` // EdgeCalls with ToID matching a known node ID
	CallEdgesUnresolved int `json:"call_edges_unresolved"` // EdgeCalls left as raw callee names (cross-file unresolved)

	// Coverage percentages (computed)
	SummaryCoverage float64 `json:"summary_coverage"` // nodes_summarized / nodes_extracted * 100
	EmbedCoverage   float64 `json:"embed_coverage"`   // nodes_embedded / nodes_extracted * 100
	StoreCoverage   float64 `json:"store_coverage"`   // nodes_stored / nodes_extracted * 100
	EdgeResolution  float64 `json:"edge_resolution"`  // call_edges_resolved / call_edges_total * 100
}

// IndexProgress is the comprehensive snapshot of an index job's state.
// Returned by GET /api/v1/index/:job_id.
type IndexProgress struct {
	// Top-level summary (backward-compatible with old IndexJob fields)
	JobID        string     `json:"job_id"`
	Status       string     `json:"status"`        // indexing, completed, failed
	RepoSlug     string     `json:"repo_slug"`
	Error        string     `json:"error,omitempty"`
	FilesIndexed int        `json:"files_indexed"`
	NodesCreated int        `json:"nodes_created"`
	EdgesCreated int        `json:"edges_created"`
	TotalErrors  int        `json:"total_errors"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	ElapsedMS    int64      `json:"elapsed_ms"`

	// Stage-level detail
	CurrentStage IndexStage                    `json:"current_stage"`
	Stages       map[IndexStage]*StageProgress `json:"stages"`

	// AST ↔ Embedding coverage (the critical gap metric)
	Coverage PipelineCoverage `json:"coverage"`

	// Configuration used for this run (for debugging)
	Config IndexConfig `json:"config"`
}
