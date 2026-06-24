package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/config"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/tts"
)

type fakeExtractor struct{ result domain.ExtractionResult }

func (f fakeExtractor) Extract(string) (domain.ExtractionResult, error) { return f.result, nil }

type fakeEngine struct{ name string }

func (f fakeEngine) Name() string { return f.name }
func (f fakeEngine) Synthesize(_ context.Context, _ string, output string, _ domain.ProcessingOptions, voice tts.VoiceSpec, progress tts.ProgressCallback) (tts.VoiceSpec, error) {
	if progress != nil {
		progress(1, "fake audio")
	}
	return voice, os.WriteFile(output, []byte("fake wav"), 0o644)
}

func TestPipelineWritesCleanedTextAndMetadata(t *testing.T) {
	root := t.TempDir()
	settings := config.Settings{DataDir: filepath.Join(root, "data"), CacheDir: filepath.Join(root, "cache"), MaxPDFPages: 20, SpeechRateMin: 0.5, SpeechRateMax: 2, MaxWorkers: 1, MaxPendingJobs: 10}
	if err := settings.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	box := domain.BBox{Left: 30, Top: 100, Right: 580, Bottom: 130}
	extraction := domain.ExtractionResult{
		PageCount: 1,
		PageSizes: map[int]domain.PageSize{1: {Width: 612, Height: 792}},
		Blocks: []domain.ExtractedBlock{
			{Text: "Abstract", Label: "section_header", PageNo: 1, BBox: &box},
			{Text: "The study evaluates a local scientific reading pipeline [12].", Label: "paragraph", PageNo: 1, BBox: &box},
		},
	}
	catalog := tts.NewCatalog(settings.VoicesDir())
	pipeline := &ReadMyPaperPipeline{Settings: settings, Extractor: fakeExtractor{extraction}, Catalog: catalog, Piper: fakeEngine{name: "piper"}, Kokoro: fakeEngine{name: "kokoro"}}
	pdfPath := filepath.Join(settings.UploadsDir(), "job-123", "source.pdf")
	if err := os.MkdirAll(filepath.Dir(pdfPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(settings.OutputsDir(), "job-123")
	options := domain.DefaultProcessingOptions()
	options.JobID = "job-123"
	options.Filename = "paper.pdf"
	options.CreatedAt = "2026-04-16T12:00:00Z"
	result, err := pipeline.Process(context.Background(), pdfPath, output, options, nil)
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(result.CleanedTextPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), "[12]") || !strings.Contains(string(text), "scientific reading") {
		t.Fatalf("unexpected cleaned text: %q", text)
	}
	var metadata map[string]any
	data, err := os.ReadFile(filepath.Join(output, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["job_id"] != "job-123" || metadata["filename"] != "paper.pdf" || metadata["engine_used"] != "piper" {
		t.Fatalf("metadata=%#v", metadata)
	}
}

func TestPipelineRejectsPageLimit(t *testing.T) {
	settings := config.Settings{DataDir: filepath.Join(t.TempDir(), "data"), CacheDir: filepath.Join(t.TempDir(), "cache"), MaxPDFPages: 2, SpeechRateMin: 0.5, SpeechRateMax: 2}
	_ = settings.EnsureDirs()
	pipeline := &ReadMyPaperPipeline{Settings: settings, Extractor: fakeExtractor{domain.ExtractionResult{PageCount: 3}}, Catalog: tts.NewCatalog(settings.VoicesDir()), Piper: fakeEngine{name: "piper"}}
	options := domain.DefaultProcessingOptions()
	_, err := pipeline.Process(context.Background(), "source.pdf", filepath.Join(t.TempDir(), "out"), options, nil)
	if err == nil || !strings.Contains(err.Error(), "configured limit") {
		t.Fatalf("expected page-limit error, got %v", err)
	}
}

func TestAtomicWriteFileReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.json")
	if err := atomicWriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "second" {
		t.Fatalf("replacement failed: %q", contents)
	}
}
