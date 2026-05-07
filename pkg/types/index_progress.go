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
	File    string `json:"file,omitempty"` // file path that caused the error
	Node    string `json:"node,omitempty"` // node qualified name
	Message string `json:"message"`        // error description
	Stage   string `json:"stage"`          // which stage
}

// StageProgress tracks the state of one pipeline stage.
type StageProgress struct {
	Status      StageStatus  `json:"status"`
	ItemsDone   int          `json:"items_done"`
	ItemsTotal  int          `json:"items_total,omitempty"` // 0 = unknown
	Errors      []StageError `json:"errors,omitempty"`
	ErrorCount  int          `json:"error_count"` // total (may exceed len(Errors) if capped)
	StartedAt   *time.Time   `json:"started_at,omitempty"`
	CompletedAt *time.Time   `json:"completed_at,omitempty"`
	DurationMS  int64        `json:"duration_ms,omitempty"`
}

// IndexConfig captures the configuration used for this index run.
// Useful for debugging questions like which provider was selected.
type IndexConfig struct {
	EmbedProvider string `json:"embed_provider"` // gemini, voyage, ollama
	LLMProvider   string `json:"llm_provider"`   // gemini, openrouter, ollama
	EmbedModel    string `json:"embed_model"`    // model name
	LLMModel      string `json:"llm_model"`      // model name
	EmbedDim      int    `json:"embed_dim"`      // HNSW dimension
	BatchSize     int    `json:"batch_size"`
	Fast          bool   `json:"fast"`    // --fast flag (skip summarize + reembed)
	Reparse       bool   `json:"reparse"` // --reparse flag
	Force         bool   `json:"force"`   // --force flag
}

// PipelineCoverage tracks the gap between what the AST parser extracted
// and what downstream stages actually processed. This is THE critical metric
// for debugging search quality and graph completeness.
type PipelineCoverage struct {
	// AST extraction totals (ground truth from tree-sitter)
	FilesWalked  int `json:"files_walked"`  // files discovered by walker
	FilesParsed  int `json:"files_parsed"`  // files successfully parsed (AST extracted)
	FilesSkipped int `json:"files_skipped"` // files skipped (unchanged ContentHash)

	NodesExtracted int `json:"nodes_extracted"` // total nodes from AST (functions + classes + files + modules)
	EdgesExtracted int `json:"edges_extracted"` // total edges from AST (calls + imports + defines + data_flow + ...)

	// Downstream processing coverage
	NodesSummarized int `json:"nodes_summarized"` // nodes that got LLM Summary + Concepts
	NodesEmbedded   int `json:"nodes_embedded"`   // nodes that got embedding vectors
	NodesStored     int `json:"nodes_stored"`     // nodes written to DB
	EdgesStored     int `json:"edges_stored"`     // edges written to DB

	// Edge resolution (resolver quality)
	CallEdgesTotal      int `json:"call_edges_total"`      // total EdgeCalls extracted
	CallEdgesResolved   int `json:"call_edges_resolved"`   // EdgeCalls with ToID matching a known node ID
	CallEdgesUnresolved int `json:"call_edges_unresolved"` // EdgeCalls left as raw callee names (cross-file unresolved)

	// Coverage percentages (computed)
	SummaryCoverage float64 `json:"summary_coverage"` // nodes_summarized / nodes_extracted * 100
	EmbedCoverage   float64 `json:"embed_coverage"`   // nodes_embedded / nodes_extracted * 100
	StoreCoverage   float64 `json:"store_coverage"`   // nodes_stored / nodes_extracted * 100
	EdgeResolution  float64 `json:"edge_resolution"`  // call_edges_resolved / call_edges_total * 100
}

// ── Streaming events (SSE) ────────────────────────────────────────────────

// IndexEventType identifies an SSE event variant on the indexing stream.
type IndexEventType string

const (
	// IndexEventStageStart is emitted when a pipeline stage transitions to running.
	IndexEventStageStart IndexEventType = "stage_start"
	// IndexEventStageDone is emitted when a stage completes (or skips/fails).
	IndexEventStageDone IndexEventType = "stage_done"
	// IndexEventGraphDelta carries a batch of newly-stored nodes and edges.
	// Nodes are guaranteed to precede edges that reference them within the
	// same delta (and within the run, modulo the per-batch contract of
	// graph.PutBatch). Emitted from the store stage.
	IndexEventGraphDelta IndexEventType = "graph_delta"
	// IndexEventProgress is a periodic counter snapshot.
	IndexEventProgress IndexEventType = "progress"
	// IndexEventDone is the terminal event with a full IndexProgress snapshot.
	IndexEventDone IndexEventType = "done"
	// IndexEventError reports a non-fatal stream-level issue (e.g. consumer lag).
	// The producer keeps running; the client may reconnect to resync.
	IndexEventError IndexEventType = "error"
)

// GraphNodeDelta is the wire shape for one streamed node. Intentionally a
// trimmed subset of CodeNode — heavy fields (Body, Embedding, Concepts) are
// dropped so the SSE wire stays cheap. Frontend can re-fetch full bodies via
// GET /api/v1/nodes/:id when a user drills in.
type GraphNodeDelta struct {
	ID        string   `json:"id"`
	Qualified string   `json:"qualified"`
	Name      string   `json:"name"`
	Kind      NodeKind `json:"kind"`
	FilePath  string   `json:"file_path"`
	Language  string   `json:"language,omitempty"`
	RepoSlug  string   `json:"repo_slug"`
	StartLine int      `json:"start_line,omitempty"`
	EndLine   int      `json:"end_line,omitempty"`
	Signature string   `json:"signature,omitempty"`
}

// GraphEdgeDelta is the wire shape for one streamed edge.
type GraphEdgeDelta struct {
	FromID   string   `json:"from_id"`
	ToID     string   `json:"to_id"`
	Kind     EdgeKind `json:"kind"`
	CallSite string   `json:"call_site,omitempty"`
}

// ProgressSnapshot is a lightweight tick payload emitted between stages.
type ProgressSnapshot struct {
	FilesIndexed int        `json:"files_indexed"`
	NodesCreated int        `json:"nodes_created"`
	EdgesCreated int        `json:"edges_created"`
	ElapsedMS    int64      `json:"elapsed_ms"`
	CurrentStage IndexStage `json:"current_stage,omitempty"`
}

// IndexEvent is one frame on the indexing SSE stream. The Type discriminator
// selects which optional payload field is populated; all unused fields are
// omitted from the JSON wire by `omitempty`.
type IndexEvent struct {
	Type      IndexEventType `json:"type"`
	EmittedAt time.Time      `json:"emitted_at"`

	// stage_start / stage_done
	Stage       IndexStage  `json:"stage,omitempty"`
	ItemsTotal  int         `json:"items_total,omitempty"`
	ItemsDone   int         `json:"items_done,omitempty"`
	ErrorCount  int         `json:"stage_error_count,omitempty"`
	DurationMS  int64       `json:"duration_ms,omitempty"`
	StageStatus StageStatus `json:"stage_status,omitempty"`

	// graph_delta
	Nodes []GraphNodeDelta `json:"nodes,omitempty"`
	Edges []GraphEdgeDelta `json:"edges,omitempty"`

	// progress
	Progress *ProgressSnapshot `json:"progress,omitempty"`

	// done
	Done *IndexProgress `json:"done,omitempty"`

	// error
	Message string `json:"message,omitempty"`
}

// IndexProgress is the comprehensive snapshot of an index job's state.
// Returned by GET /api/v1/index/:job_id.
type IndexProgress struct {
	// Top-level summary (backward-compatible with old IndexJob fields)
	JobID        string     `json:"job_id"`
	Status       string     `json:"status"` // indexing, completed, failed
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
