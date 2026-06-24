package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

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
	if desktop.preview.Disabled() {
		t.Fatal("cleaned text preview must stay enabled so dark mode uses normal text contrast")
	}
	if desktop.detailID.Importance == widget.LowImportance {
		t.Fatal("job detail text must not use low-contrast importance in dark mode")
	}

	// Exercises the close intercept. It must stop polling, shut down the job
	// manager and remove itself before Window.Close, rather than recurse.
	desktop.Window().Close()
	application.Quit()
}

func TestDesktopBuildsLLMAPIKeyFieldFromSettings(t *testing.T) {
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
		LLMEnabled:     true,
		LLMBaseURL:     "http://127.0.0.1:11434/v1",
		LLMModel:       "local-model",
		LLMAPIKey:      "configured-key",
	}
	if err := settings.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	application := test.NewApp()
	store := jobs.NewStore()
	manager := jobs.NewManager(store, noOpProcessor{}, 1)
	desktop := NewDesktop(application, settings, store, manager, tts.NewCatalog(settings.VoicesDir()))
	defer application.Quit()

	if desktop.llmAPIKey == nil {
		t.Fatal("LLM API key field was not initialized")
	}
	if desktop.llmAPIKey.Text != "configured-key" {
		t.Fatalf("LLM API key default mismatch: %q", desktop.llmAPIKey.Text)
	}
	if !desktop.llmFields.Visible() {
		t.Fatal("LLM fields should be visible when LLM starts enabled")
	}

	desktop.Window().Close()
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
