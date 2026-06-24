package jobs

import (
	"testing"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
)

func TestStoreCapacityAndCloning(t *testing.T) {
	store := NewStore()
	first, err := store.Create("a.pdf", domain.DefaultProcessingOptions(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create("b.pdf", domain.DefaultProcessingOptions(), 1); err == nil {
		t.Fatal("expected capacity error")
	}
	_, err = store.Update(first.JobID, func(job *domain.JobState) {
		job.Status = domain.JobCompleted
		stats := domain.NewCleaningStats(1)
		stats.DroppedByRule["x"] = 1
		job.Result.Stats = &stats
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create("b.pdf", domain.DefaultProcessingOptions(), 1)
	if err != nil || second.JobID == first.JobID {
		t.Fatalf("second create: %#v %v", second, err)
	}
	copy, _ := store.Get(first.JobID)
	copy.Result.Stats.DroppedByRule["x"] = 99
	again, _ := store.Get(first.JobID)
	if again.Result.Stats.DroppedByRule["x"] != 1 {
		t.Fatal("Get returned aliased map")
	}
}
