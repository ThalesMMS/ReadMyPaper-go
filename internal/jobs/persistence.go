package jobs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/config"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/util"
)

var safeJobID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

const orphanDirectoryGracePeriod = 10 * time.Minute

type persistedMetadata struct {
	JobID             string                   `json:"job_id"`
	Filename          string                   `json:"filename"`
	CreatedAt         string                   `json:"created_at"`
	SourcePDF         string                   `json:"source_pdf"`
	DetectedLanguage  string                   `json:"detected_language"`
	EffectiveLanguage string                   `json:"effective_language"`
	EngineUsed        string                   `json:"engine_used"`
	Options           domain.ProcessingOptions `json:"options"`
	Stats             *domain.CleaningStats    `json:"stats"`
}

// RestoreFromDisk recovers only fully synthesized jobs (reading.wav present).
// Corrupt directories are skipped and reported together rather than aborting
// application startup.
func RestoreFromDisk(store *Store, settings config.Settings) []error {
	entries, err := os.ReadDir(settings.OutputsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []error{err}
	}
	var warnings []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		job, err := restoreOne(filepath.Join(settings.OutputsDir(), entry.Name()), settings)
		if err != nil {
			warnings = append(warnings, err)
			continue
		}
		if job.JobID != "" {
			store.RestoreIfAbsent(job)
		}
	}
	return warnings
}

func restoreOne(outputDir string, settings config.Settings) (domain.JobState, error) {
	audioPath := filepath.Join(outputDir, "reading.wav")
	if info, err := os.Stat(audioPath); err != nil || info.Size() < 44 {
		return domain.JobState{}, nil
	}
	metadataPath := filepath.Join(outputDir, "metadata.json")
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return domain.JobState{}, fmt.Errorf("restore %s: %w", outputDir, err)
	}
	var metadata persistedMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return domain.JobState{}, fmt.Errorf("restore %s: invalid metadata: %w", outputDir, err)
	}
	if !safeJobID.MatchString(metadata.JobID) || metadata.Filename == "" || metadata.CreatedAt == "" {
		return domain.JobState{}, fmt.Errorf("restore %s: incomplete or unsafe metadata", outputDir)
	}
	if filepath.Base(metadata.Filename) != metadata.Filename || metadata.Filename == "." {
		return domain.JobState{}, fmt.Errorf("restore %s: unsafe filename", outputDir)
	}
	if filepath.Base(filepath.Clean(outputDir)) != metadata.JobID {
		return domain.JobState{}, fmt.Errorf("restore %s: job_id does not match output directory", outputDir)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, metadata.CreatedAt)
	if err != nil {
		createdAt, err = time.Parse(time.RFC3339, metadata.CreatedAt)
		if err != nil {
			return domain.JobState{}, fmt.Errorf("restore %s: invalid created_at", outputDir)
		}
	}
	textPath := filepath.Join(outputDir, "cleaned_text.txt")
	if _, err := os.Stat(textPath); err != nil {
		textPath = ""
	}
	sourcePath := validatedSourcePath(metadata.SourcePDF, metadata.JobID, settings)
	language := metadata.EffectiveLanguage
	if language == "" {
		language = metadata.DetectedLanguage
	}
	return domain.JobState{
		JobID: metadata.JobID, Filename: metadata.Filename,
		CreatedAt: createdAt.UTC(), UpdatedAt: createdAt.UTC(),
		Status: domain.JobCompleted, Step: "Completed", Progress: 1,
		EngineUsed: metadata.EngineUsed, Options: metadata.Options,
		Result: domain.JobResult{
			CleanedTextPath: textPath, AudioPath: audioPath, OriginalPDFPath: sourcePath,
			DetectedLanguage: language, EngineUsed: metadata.EngineUsed, Stats: metadata.Stats,
		},
	}, nil
}

func validatedSourcePath(value, jobID string, settings config.Settings) string {
	candidates := []string{value, filepath.Join(settings.UploadsDir(), jobID, "source.pdf")}
	for _, candidate := range candidates {
		if candidate == "" || !util.WithinRoot(settings.UploadsDir(), candidate) {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// DeleteArtifacts removes a terminal job and only paths derived from its safe
// identifier beneath the configured data roots.
func DeleteArtifacts(store *Store, settings config.Settings, jobID string) error {
	job, exists := store.Get(jobID)
	if !exists {
		return ErrNotFound
	}
	if job.Status == domain.JobPending || job.Status == domain.JobRunning {
		return fmt.Errorf("cannot delete a job that is still processing")
	}
	if !safeJobID.MatchString(jobID) {
		return fmt.Errorf("invalid job identifier")
	}
	paths := []struct{ root, path string }{
		{settings.UploadsDir(), filepath.Join(settings.UploadsDir(), jobID)},
		{settings.OutputsDir(), filepath.Join(settings.OutputsDir(), jobID)},
	}
	for _, item := range paths {
		if !util.WithinRoot(item.root, item.path) {
			return fmt.Errorf("refusing to remove path outside data root")
		}
		if err := os.RemoveAll(item.path); err != nil {
			return err
		}
	}
	store.Delete(jobID)
	return nil
}

// CleanupExpired removes terminal jobs older than the configured TTL and then
// removes old orphan directories left by interrupted or corrupt jobs.
func CleanupExpired(store *Store, settings config.Settings, now time.Time) []error {
	if settings.JobRetentionHours <= 0 {
		return nil
	}
	cutoff := now.UTC().Add(-time.Duration(settings.JobRetentionHours) * time.Hour)
	storedJobs := store.List()
	sort.Slice(storedJobs, func(i, j int) bool {
		return storedJobs[i].CreatedAt.Before(storedJobs[j].CreatedAt)
	})
	var errorsFound []error
	for _, job := range storedJobs {
		if (job.Status != domain.JobCompleted && job.Status != domain.JobFailed) || !job.CreatedAt.Before(cutoff) {
			continue
		}
		if err := DeleteArtifacts(store, settings, job.JobID); err != nil {
			errorsFound = append(errorsFound, err)
		}
	}

	activeIDs := make(map[string]struct{})
	for _, job := range store.List() {
		activeIDs[job.JobID] = struct{}{}
	}
	for _, root := range []string{settings.UploadsDir(), settings.OutputsDir()} {
		errorsFound = append(errorsFound, cleanupOrphanDirectories(root, activeIDs, cutoff)...)
	}
	return errorsFound
}

func cleanupOrphanDirectories(root string, activeIDs map[string]struct{}, cutoff time.Time) []error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []error{fmt.Errorf("scan retention root %s: %w", root, err)}
	}
	graceCutoff := cutoff.Add(-orphanDirectoryGracePeriod)
	var errorsFound []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, exists := activeIDs[entry.Name()]; exists {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if !util.WithinRoot(root, path) {
			errorsFound = append(errorsFound, fmt.Errorf("refusing to inspect orphan outside retention root: %s", path))
			continue
		}
		info, err := entry.Info()
		if err != nil {
			errorsFound = append(errorsFound, fmt.Errorf("inspect orphan %s: %w", path, err))
			continue
		}
		if !info.ModTime().Before(graceCutoff) {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			errorsFound = append(errorsFound, fmt.Errorf("remove orphan %s: %w", path, err))
		}
	}
	return errorsFound
}
