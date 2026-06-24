package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"fyne.io/fyne/v2/test"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/config"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/jobs"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/tts"
)

type noOpProcessor struct{}

func (noOpProcessor) Process(
	context.Context,
	string,
	string,
	domain.ProcessingOptions,
	func(float64, string),
) (domain.JobResult, error) {
	return domain.JobResult{}, nil
}

func TestDesktopBuildsAndCloses(t *testing.T) {
	root := t.TempDir()
	settings := config.Settings{
		DataDir:        filepath.Join(root, "data"),
		CacheDir:       filepath.Join(root, "cache"),
		MaxWorkers:     1,
		MaxUploadBytes: 10 << 20,
		MaxPDFPages:    20,
		SpeechRateMin:  0.5,
		SpeechRateMax:  2,
		MaxPendingJobs: 4,
	}
	if err := settings.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	application := test.NewApp()
	store := jobs.NewStore()
	manager := jobs.NewManager(store, noOpProcessor{}, 1)
	desktop := NewDesktop(application, settings, store, manager, tts.NewCatalog(settings.VoicesDir()))

	if desktop.Window().Content() == nil {
		t.Fatal("desktop window has no content")
	}
	if desktop.tabs == nil || desktop.newJobTab == nil || desktop.jobsTab == nil {
		t.Fatal("desktop tabs were not initialized")
	}
	if desktop.process == nil || desktop.jobList == nil || desktop.preview == nil {
		t.Fatal("essential controls were not initialized")
	}

	// Exercises the close intercept. It must stop polling, shut down the job
	// manager and remove itself before Window.Close, rather than recurse.
	desktop.Window().Close()
	application.Quit()
}

func TestReadPreviewTruncatesAtUTF8Boundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preview.txt")
	if err := os.WriteFile(path, []byte("abcádef"), 0o644); err != nil {
		t.Fatal(err)
	}
	preview := readPreview(path, 4)
	if !utf8.ValidString(preview) {
		t.Fatalf("preview is invalid UTF-8: %q", preview)
	}
	if !strings.Contains(preview, "preview truncated") {
		t.Fatalf("truncation marker missing: %q", preview)
	}
}
