package jobs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/config"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
)

func testSettings(t *testing.T) config.Settings {
	t.Helper()
	root := t.TempDir()
	settings := config.Settings{DataDir: filepath.Join(root, "data"), CacheDir: filepath.Join(root, "cache"), MaxPDFPages: 200, SpeechRateMin: 0.5, SpeechRateMax: 2, MaxWorkers: 1, MaxPendingJobs: 10}
	if err := settings.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestRestoreAndDeleteArtifacts(t *testing.T) {
	settings := testSettings(t)
	jobID := "job-123"
	output := filepath.Join(settings.OutputsDir(), jobID)
	upload := filepath.Join(settings.UploadsDir(), jobID)
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(upload, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(output, "reading.wav"), append([]byte("RIFF"), make([]byte, 80)...), 0o644)
	_ = os.WriteFile(filepath.Join(output, "cleaned_text.txt"), []byte("text"), 0o644)
	_ = os.WriteFile(filepath.Join(upload, "source.pdf"), []byte("%PDF-1.4"), 0o644)
	created := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	meta := persistedMetadata{JobID: jobID, Filename: "paper.pdf", CreatedAt: created, SourcePDF: filepath.Join(upload, "source.pdf"), EffectiveLanguage: "en", EngineUsed: "piper", Options: domain.DefaultProcessingOptions()}
	data, _ := json.Marshal(meta)
	_ = os.WriteFile(filepath.Join(output, "metadata.json"), data, 0o644)

	store := NewStore()
	warnings := RestoreFromDisk(store, settings)
	if len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	job, ok := store.Get(jobID)
	if !ok || job.Status != domain.JobCompleted || job.Result.AudioPath == "" {
		t.Fatalf("not restored: %#v", job)
	}
	if err := DeleteArtifacts(store, settings, jobID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("output remains: %v", err)
	}
	if _, ok := store.Get(jobID); ok {
		t.Fatal("job remains in store")
	}
}

func TestRestoreRejectsMismatchedOutputDirectory(t *testing.T) {
	settings := testSettings(t)
	output := filepath.Join(settings.OutputsDir(), "different-directory")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "reading.wav"), append([]byte("RIFF"), make([]byte, 80)...), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata := persistedMetadata{
		JobID: "job-123", Filename: "paper.pdf",
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewStore()
	warnings := RestoreFromDisk(store, settings)
	if len(warnings) != 1 {
		t.Fatalf("expected one integrity warning, got %v", warnings)
	}
	if len(store.List()) != 0 {
		t.Fatalf("mismatched metadata was restored: %#v", store.List())
	}
}

func TestCleanupExpiredJobsAndOrphanDirectories(t *testing.T) {
	settings := testSettings(t)
	settings.JobRetentionHours = 1
	now := time.Now().UTC()

	store := NewStore()
	expiredID := "expired-job"
	store.RestoreIfAbsent(domain.JobState{
		JobID: expiredID, Filename: "old.pdf",
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now,
		Status: domain.JobCompleted, Step: "Completed", Progress: 1,
	})
	for _, root := range []string{settings.UploadsDir(), settings.OutputsDir()} {
		if err := os.MkdirAll(filepath.Join(root, expiredID), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	activeID := "active-job"
	store.RestoreIfAbsent(domain.JobState{
		JobID: activeID, Filename: "active.pdf",
		CreatedAt: now, UpdatedAt: now,
		Status: domain.JobCompleted, Step: "Completed", Progress: 1,
	})
	activeDir := filepath.Join(settings.OutputsDir(), activeID)
	if err := os.MkdirAll(activeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldOrphan := filepath.Join(settings.OutputsDir(), "orphan-old")
	recentOrphan := filepath.Join(settings.UploadsDir(), "orphan-recent")
	if err := os.MkdirAll(oldOrphan, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(recentOrphan, 0o755); err != nil {
		t.Fatal(err)
	}
	oldTime := now.Add(-3 * time.Hour)
	if err := os.Chtimes(oldOrphan, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	warnings := CleanupExpired(store, settings, now)
	if len(warnings) != 0 {
		t.Fatalf("cleanup warnings: %v", warnings)
	}
	if _, exists := store.Get(expiredID); exists {
		t.Fatal("expired job remains in store")
	}
	if _, err := os.Stat(oldOrphan); !os.IsNotExist(err) {
		t.Fatalf("old orphan remains: %v", err)
	}
	if _, err := os.Stat(recentOrphan); err != nil {
		t.Fatalf("recent orphan was removed: %v", err)
	}
	if _, err := os.Stat(activeDir); err != nil {
		t.Fatalf("active job directory was removed: %v", err)
	}
}
