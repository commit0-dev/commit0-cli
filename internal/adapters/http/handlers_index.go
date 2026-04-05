package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/commit0-dev/commit0/internal/app"
)

// IndexJob represents the state of an asynchronous index operation.
type IndexJob struct {
	ID           string     `json:"id"`
	Status       string     `json:"status"` // "indexing" | "completed" | "failed"
	RepoSlug     string     `json:"repo_slug"`
	FilesIndexed int        `json:"files_indexed"`
	NodesCreated int        `json:"nodes_created"`
	Errors       int        `json:"errors"`
	Error        string     `json:"error,omitempty"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

// indexJobStore is an in-memory store for async index jobs.
type indexJobStore struct {
	mu   sync.RWMutex
	jobs map[string]*IndexJob
}

func newIndexJobStore() *indexJobStore {
	return &indexJobStore{jobs: make(map[string]*IndexJob)}
}

func (s *indexJobStore) set(job *IndexJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
}

func (s *indexJobStore) get(id string) (*IndexJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	// Return a copy so callers can read fields without holding the lock.
	copy := *job
	return &copy, true
}

func (s *indexJobStore) update(id string, fn func(*IndexJob)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return false
	}
	fn(job)
	return true
}

// newJobID generates a random hex job ID using crypto/rand.
func newJobID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type startIndexRequest struct {
	RepoPath string   `json:"repo_path"`
	RepoSlug string   `json:"repo_slug"`
	Languages []string `json:"languages"`
	Exclude   []string `json:"exclude"`
}

// handleStartIndex handles POST /api/v1/index.
// It starts the index operation asynchronously and returns the job ID.
func (s *Server) handleStartIndex(c echo.Context) error {
	var req startIndexRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.RepoPath == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "repo_path is required")
	}
	if req.RepoSlug == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "repo_slug is required")
	}

	jobID, err := newJobID()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate job ID")
	}

	job := &IndexJob{
		ID:        jobID,
		Status:    "indexing",
		RepoSlug:  req.RepoSlug,
		StartedAt: time.Now(),
	}
	s.jobs.set(job)

	// Run indexing in background; use a detached context so cancelling the
	// HTTP request does not abort the index job.
	go func() {
		result, indexErr := s.indexSvc.Index(context.Background(), app.IndexRequest{
			RepoPath:  req.RepoPath,
			RepoSlug:  req.RepoSlug,
			Languages: req.Languages,
		})

		s.jobs.update(jobID, func(j *IndexJob) {
			now := time.Now()
			j.FinishedAt = &now
			if indexErr != nil {
				j.Status = "failed"
				j.Error = indexErr.Error()
			} else {
				j.Status = "completed"
				j.FilesIndexed = result.FilesIndexed
				j.NodesCreated = result.NodesCreated
				j.Errors = 0
			}
		})
	}()

	return c.JSON(http.StatusAccepted, map[string]string{"job_id": jobID})
}

// handleIndexStatus handles GET /api/v1/index/:job_id.
func (s *Server) handleIndexStatus(c echo.Context) error {
	jobID := c.Param("job_id")
	job, ok := s.jobs.get(jobID)
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "job not found")
	}
	return c.JSON(http.StatusOK, job)
}
