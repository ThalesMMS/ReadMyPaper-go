// Package jobs owns the in-memory job registry and persistence restoration.
package jobs

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/util"
)

var ErrNotFound = errors.New("job not found")

// Store is safe for concurrent use. All read methods return deep copies so UI
// code cannot mutate pipeline state without synchronization.
type Store struct {
	mu      sync.RWMutex
	jobs    map[string]domain.JobState
	version uint64
}

func NewStore() *Store { return &Store{jobs: make(map[string]domain.JobState)} }

func (s *Store) Create(filename string, options domain.ProcessingOptions, maxActive int) (domain.JobState, error) {
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "" || filename == "." {
		return domain.JobState{}, errors.New("filename is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxActive > 0 && s.activeLocked() >= maxActive {
		return domain.JobState{}, errors.New("too many queued or running jobs")
	}
	jobID, err := util.NewJobID()
	if err != nil {
		return domain.JobState{}, err
	}
	now := time.Now().UTC()
	options.JobID = jobID
	options.Filename = filename
	options.CreatedAt = now.Format(time.RFC3339Nano)
	job := domain.JobState{
		JobID: jobID, Filename: filename, CreatedAt: now, UpdatedAt: now,
		Status: domain.JobPending, Step: "Queued", Progress: 0.02, Options: options,
	}
	s.jobs[jobID] = job
	s.version++
	return job.Clone(), nil
}

func (s *Store) Get(jobID string) (domain.JobState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, exists := s.jobs[jobID]
	return job.Clone(), exists
}

func (s *Store) List() []domain.JobState {
	s.mu.RLock()
	result := make([]domain.JobState, 0, len(s.jobs))
	for _, job := range s.jobs {
		result = append(result, job.Clone())
	}
	s.mu.RUnlock()
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].JobID > result[j].JobID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

func (s *Store) Version() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

func (s *Store) ActiveCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeLocked()
}

func (s *Store) activeLocked() int {
	count := 0
	for _, job := range s.jobs {
		if job.Status == domain.JobPending || job.Status == domain.JobRunning {
			count++
		}
	}
	return count
}

// Update applies a mutation while holding the store lock and refreshes
// UpdatedAt. The returned state is detached from the store.
func (s *Store) Update(jobID string, mutate func(*domain.JobState)) (domain.JobState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, exists := s.jobs[jobID]
	if !exists {
		return domain.JobState{}, ErrNotFound
	}
	mutate(&job)
	if job.Progress < 0 {
		job.Progress = 0
	}
	if job.Progress > 1 {
		job.Progress = 1
	}
	job.UpdatedAt = time.Now().UTC()
	s.jobs[jobID] = job
	s.version++
	return job.Clone(), nil
}

func (s *Store) RestoreIfAbsent(job domain.JobState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[job.JobID]; exists {
		return false
	}
	s.jobs[job.JobID] = job.Clone()
	s.version++
	return true
}

func (s *Store) Delete(jobID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[jobID]; !exists {
		return false
	}
	delete(s.jobs, jobID)
	s.version++
	return true
}
