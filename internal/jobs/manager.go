package jobs

import (
	"context"
	"fmt"
	"sync"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
)

// Processor is implemented by the scientific-paper pipeline.
type Processor interface {
	Process(
		ctx context.Context,
		pdfPath, outputDir string,
		options domain.ProcessingOptions,
		progress func(float64, string),
	) (domain.JobResult, error)
}

// Manager executes jobs with bounded concurrency and records all state
// transitions in Store.
type Manager struct {
	Store     *Store
	Processor Processor
	workers   chan struct{}
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	mu      sync.Mutex
	started map[string]struct{}
	closed  bool
}

func NewManager(store *Store, processor Processor, maxWorkers int) *Manager {
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		Store: store, Processor: processor, workers: make(chan struct{}, maxWorkers),
		ctx: ctx, cancel: cancel, started: make(map[string]struct{}),
	}
}

func (m *Manager) Start(jobID, pdfPath, outputDir string, options domain.ProcessingOptions) error {
	if m == nil || m.Store == nil || m.Processor == nil {
		return fmt.Errorf("job manager is not configured")
	}
	job, exists := m.Store.Get(jobID)
	if !exists {
		return ErrNotFound
	}
	if job.Status != domain.JobPending {
		return fmt.Errorf("job %s is not pending", jobID)
	}

	// Start and Shutdown coordinate under the same mutex so WaitGroup.Add can
	// never race with Shutdown's Wait. A job ID is accepted only once.
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("job manager is shutting down")
	}
	if _, alreadyStarted := m.started[jobID]; alreadyStarted {
		m.mu.Unlock()
		return fmt.Errorf("job %s was already started", jobID)
	}
	m.started[jobID] = struct{}{}
	m.wg.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.wg.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				m.fail(jobID, fmt.Errorf("processing panic: %v", recovered))
			}
		}()

		select {
		case m.workers <- struct{}{}:
			defer func() { <-m.workers }()
		case <-m.ctx.Done():
			m.fail(jobID, m.ctx.Err())
			return
		}

		_, _ = m.Store.Update(jobID, func(job *domain.JobState) {
			job.Status = domain.JobRunning
			job.Step = "Starting"
			job.Progress = 0.04
			job.Error = ""
		})
		result, err := m.Processor.Process(m.ctx, pdfPath, outputDir, options, func(ratio float64, step string) {
			_, _ = m.Store.Update(jobID, func(job *domain.JobState) {
				job.Status = domain.JobRunning
				job.Step = step
				job.Progress = ratio
			})
		})
		if err != nil {
			m.fail(jobID, err)
			return
		}
		_, _ = m.Store.Update(jobID, func(job *domain.JobState) {
			job.Status = domain.JobCompleted
			job.Step = "Completed"
			job.Progress = 1
			job.Error = ""
			job.EngineUsed = result.EngineUsed
			job.Result = result
		})
	}()
	return nil
}

func (m *Manager) fail(jobID string, err error) {
	if err == nil {
		err = fmt.Errorf("processing failed")
	}
	_, _ = m.Store.Update(jobID, func(job *domain.JobState) {
		job.Status = domain.JobFailed
		job.Step = "Failed"
		job.Error = err.Error()
		if job.Progress < 0.05 {
			job.Progress = 0.05
		}
	})
}

func (m *Manager) Shutdown() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		m.cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
}
