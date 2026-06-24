package jobs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
)

type processorFunc func(context.Context, string, string, domain.ProcessingOptions, func(float64, string)) (domain.JobResult, error)

func (fn processorFunc) Process(
	ctx context.Context,
	pdfPath, outputDir string,
	options domain.ProcessingOptions,
	progress func(float64, string),
) (domain.JobResult, error) {
	return fn(ctx, pdfPath, outputDir, options, progress)
}

func waitForTerminalJob(t *testing.T, store *Store, jobID string) domain.JobState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, exists := store.Get(jobID)
		if exists && (job.Status == domain.JobCompleted || job.Status == domain.JobFailed) {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach a terminal state", jobID)
	return domain.JobState{}
}

func TestManagerCompletesAndRejectsDuplicateStart(t *testing.T) {
	store := NewStore()
	job, err := store.Create("paper.pdf", domain.DefaultProcessingOptions(), 2)
	if err != nil {
		t.Fatal(err)
	}
	processor := processorFunc(func(_ context.Context, _, _ string, _ domain.ProcessingOptions, progress func(float64, string)) (domain.JobResult, error) {
		progress(0.6, "Working")
		return domain.JobResult{EngineUsed: "piper"}, nil
	})
	manager := NewManager(store, processor, 1)
	defer manager.Shutdown()

	if err := manager.Start(job.JobID, "paper.pdf", t.TempDir(), job.Options); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(job.JobID, "paper.pdf", t.TempDir(), job.Options); err == nil {
		t.Fatal("duplicate start was accepted")
	}
	completed := waitForTerminalJob(t, store, job.JobID)
	if completed.Status != domain.JobCompleted || completed.EngineUsed != "piper" {
		t.Fatalf("unexpected completed state: %#v", completed)
	}
}

func TestManagerConvertsProcessorPanicToFailedJob(t *testing.T) {
	store := NewStore()
	job, err := store.Create("paper.pdf", domain.DefaultProcessingOptions(), 1)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store, processorFunc(func(context.Context, string, string, domain.ProcessingOptions, func(float64, string)) (domain.JobResult, error) {
		panic("broken extractor")
	}), 1)
	defer manager.Shutdown()

	if err := manager.Start(job.JobID, "paper.pdf", t.TempDir(), job.Options); err != nil {
		t.Fatal(err)
	}
	failed := waitForTerminalJob(t, store, job.JobID)
	if failed.Status != domain.JobFailed || !strings.Contains(failed.Error, "broken extractor") {
		t.Fatalf("panic was not recorded: %#v", failed)
	}
}

func TestManagerShutdownCancelsAndRejectsNewWork(t *testing.T) {
	store := NewStore()
	job, err := store.Create("paper.pdf", domain.DefaultProcessingOptions(), 2)
	if err != nil {
		t.Fatal(err)
	}
	processor := processorFunc(func(ctx context.Context, _, _ string, _ domain.ProcessingOptions, _ func(float64, string)) (domain.JobResult, error) {
		<-ctx.Done()
		return domain.JobResult{}, ctx.Err()
	})
	manager := NewManager(store, processor, 1)
	if err := manager.Start(job.JobID, "paper.pdf", t.TempDir(), job.Options); err != nil {
		t.Fatal(err)
	}
	manager.Shutdown()
	failed := waitForTerminalJob(t, store, job.JobID)
	if failed.Status != domain.JobFailed || !strings.Contains(failed.Error, "context canceled") {
		t.Fatalf("unexpected cancelled state: %#v", failed)
	}

	second, err := store.Create("second.pdf", domain.DefaultProcessingOptions(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(second.JobID, "second.pdf", t.TempDir(), second.Options); err == nil {
		t.Fatal("manager accepted work after shutdown")
	}
}
