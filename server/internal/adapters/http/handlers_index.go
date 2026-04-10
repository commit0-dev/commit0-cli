package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/commit0-dev/commit0/server/internal/app"
)

// IndexJob represents the state of an asynchronous index operation.
type IndexJob struct {
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	ID           string     `json:"id"`
	Status       string     `json:"status"`
	RepoSlug     string     `json:"repo_slug"`
	Error        string     `json:"error,omitempty"`
	FilesIndexed int        `json:"files_indexed"`
	NodesCreated int        `json:"nodes_created"`
	Errors       int        `json:"errors"`
}

// indexJobStore is an in-memory store for async index jobs.
type indexJobStore struct {
	jobs map[string]*IndexJob
	mu   sync.RWMutex
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
	RepoPath  string   `json:"repo_path"`
	RepoSlug  string   `json:"repo_slug"`
	Languages []string `json:"languages"`
	Exclude   []string `json:"exclude"`
	Force     bool     `json:"force"`
}

// handleStartIndex handles POST /api/v1/index.
func (s *Server) handleStartIndex(c *gin.Context) {
	var req startIndexRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request body"})
		return
	}
	if req.RepoPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "repo_path is required"})
		return
	}
	if req.RepoSlug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "repo_slug is required"})
		return
	}

	jobID, err := newJobID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to generate job ID"})
		return
	}

	job := &IndexJob{
		ID:        jobID,
		Status:    "indexing",
		RepoSlug:  req.RepoSlug,
		StartedAt: time.Now(),
	}
	s.jobs.set(job)

	go func() {
		onProgress := func(filesIndexed, nodesCreated int) {
			s.jobs.update(jobID, func(j *IndexJob) {
				j.FilesIndexed = filesIndexed
				j.NodesCreated = nodesCreated
			})
		}

		result, indexErr := s.indexSvc.IndexWithProgress(context.Background(), app.IndexRequest{
			RepoPath:  req.RepoPath,
			RepoSlug:  req.RepoSlug,
			Languages: req.Languages,
			Force:     req.Force,
		}, onProgress)

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

	c.JSON(http.StatusAccepted, gin.H{"job_id": jobID})
}

// handleIndexStatus handles GET /api/v1/index/:job_id.
func (s *Server) handleIndexStatus(c *gin.Context) {
	jobID := c.Param("job_id")
	job, ok := s.jobs.get(jobID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"message": "job not found"})
		return
	}
	c.JSON(http.StatusOK, job)
}
