// Package pipeline orchestrates extraction, reading-order repair, scientific
// text cleanup, optional LLM review, and local speech synthesis.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/cleaner"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/config"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/llm"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/pdfextract"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/tts"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/util"
)

type ReadMyPaperPipeline struct {
	Settings  config.Settings
	Extractor pdfextract.Extractor
	Catalog   *tts.Catalog
	LLM       *llm.Client
	Piper     tts.Engine
	Kokoro    tts.Engine
}

func New(settings config.Settings) *ReadMyPaperPipeline {
	catalog := tts.NewCatalog(settings.VoicesDir())
	return &ReadMyPaperPipeline{
		Settings:  settings,
		Extractor: pdfextract.New(),
		Catalog:   catalog,
		LLM:       llm.NewClient(settings.LLMAPIKey),
		Piper:     tts.PiperEngine{Catalog: catalog, PythonBinary: settings.PythonBinary, ModelsDir: settings.ModelsDir()},
		Kokoro:    tts.KokoroEngine{PythonBinary: settings.PythonBinary, ModelsDir: settings.ModelsDir()},
	}
}

type metadata struct {
	JobID             string                   `json:"job_id"`
	Filename          string                   `json:"filename"`
	CreatedAt         string                   `json:"created_at"`
	SourcePDF         string                   `json:"source_pdf"`
	DetectedLanguage  string                   `json:"detected_language"`
	EffectiveLanguage string                   `json:"effective_language"`
	Options           domain.ProcessingOptions `json:"options"`
	Stats             domain.CleaningStats     `json:"stats"`
	Voice             *tts.VoiceSpec           `json:"voice,omitempty"`
	EngineUsed        string                   `json:"engine_used,omitempty"`
}

func (p *ReadMyPaperPipeline) Process(
	ctx context.Context,
	pdfPath, outputDir string,
	options domain.ProcessingOptions,
	progress func(float64, string),
) (domain.JobResult, error) {
	if p == nil || p.Extractor == nil || p.Catalog == nil || p.Piper == nil {
		return domain.JobResult{}, fmt.Errorf("pipeline is not fully configured")
	}
	if err := validateOptions(&options, p.Settings); err != nil {
		return domain.JobResult{}, err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return domain.JobResult{}, fmt.Errorf("create output directory: %w", err)
	}
	emit := func(ratio float64, step string) {
		if progress != nil {
			progress(ratio, step)
		}
	}
	checkContext := func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}

	emit(0.05, "Extracting PDF")
	extraction, err := p.Extractor.Extract(pdfPath)
	if err != nil {
		return domain.JobResult{}, err
	}
	if extraction.PageCount > p.Settings.MaxPDFPages {
		return domain.JobResult{}, fmt.Errorf("PDF has %d pages; configured limit is %d", extraction.PageCount, p.Settings.MaxPDFPages)
	}
	if err := checkContext(); err != nil {
		return domain.JobResult{}, err
	}

	emit(0.18, "Repairing reading order")
	blocks := cleaner.RepairReadingOrder(extraction.Blocks, extraction.PageSizes)
	emit(0.28, "Filtering figures and tables")
	blocks, spatialDropped := cleaner.FilterByLayout(blocks, extraction.LayoutRegions, 12)
	if err := checkContext(); err != nil {
		return domain.JobResult{}, err
	}

	emit(0.35, "Cleaning scientific content")
	textCleaner := cleaner.NewScientificTextCleaner(options)
	cleanedText, stats := textCleaner.Clean(blocks, extraction.PageCount)
	stats.ReadingOrderRepaired = true
	stats.LayoutRegionsFound = len(extraction.LayoutRegions)
	stats.LayoutFilterDropped = spatialDropped

	llmBase := strings.TrimSpace(options.LLMBaseURL)
	if llmBase == "" {
		llmBase = strings.TrimSpace(p.Settings.LLMBaseURL)
	}
	if options.UseLLMCleaner && llmBase != "" {
		if p.LLM == nil {
			return domain.JobResult{}, fmt.Errorf("LLM cleaner is enabled but no LLM client is configured")
		}
		emit(0.42, "Reviewing blocks with local LLM")
		llmCandidateCount := len(blocks)
		normalized, normalizeErr := util.NormalizeBaseURL(llmBase)
		if normalizeErr != nil {
			return domain.JobResult{}, normalizeErr
		}
		model := options.LLMModel
		if strings.TrimSpace(model) == "" {
			model = p.Settings.LLMModel
		}
		llmBlocks, llmErr := p.LLM.CleanAndReorderBlocks(ctx, blocks, normalized, model, &stats)
		if llmErr != nil {
			log.Printf("ReadMyPaper: %v", llmErr)
		}
		blocks = llmBlocks
		var cleanStats domain.CleaningStats
		cleanedText, cleanStats = textCleaner.Clean(blocks, extraction.PageCount)
		llmDropped := stats.LLMBlocksDropped
		stats.KeptBlocks = cleanStats.KeptBlocks
		stats.DroppedBlocks = cleanStats.DroppedBlocks + llmDropped
		stats.TotalBlocks = cleanStats.TotalBlocks + llmDropped
		stats.DroppedByLabel = cleanStats.DroppedByLabel
		stats.DroppedByRule = cleanStats.DroppedByRule
		stats.DetectedLanguage = cleanStats.DetectedLanguage
		stats.ReadingOrderRepaired = true
		stats.LayoutRegionsFound = len(extraction.LayoutRegions)
		stats.LayoutFilterDropped = spatialDropped
		stats.LLMBlocksProcessed = llmCandidateCount
		stats.LLMBlocksDropped = llmDropped
	}
	cleanedText = strings.TrimSpace(cleanedText)
	if cleanedText == "" {
		return domain.JobResult{}, fmt.Errorf("no narrative scientific text remained after cleanup")
	}

	effectiveLanguage := stats.DetectedLanguage
	if options.Language != "" && options.Language != "auto" {
		effectiveLanguage = options.Language
	}
	if effectiveLanguage == "unknown" || effectiveLanguage == "" {
		effectiveLanguage = "en"
	}
	textPath := filepath.Join(outputDir, "cleaned_text.txt")
	if err := atomicWriteFile(textPath, []byte(cleanedText+"\n"), 0o644); err != nil {
		return domain.JobResult{}, err
	}
	emit(0.50, "Saved cleaned text")

	meta := metadata{
		JobID: options.JobID, Filename: options.Filename, CreatedAt: options.CreatedAt,
		SourcePDF: pdfPath, DetectedLanguage: stats.DetectedLanguage,
		EffectiveLanguage: effectiveLanguage, Options: options, Stats: stats,
	}
	metadataPath := filepath.Join(outputDir, "metadata.json")
	if err := writeJSONAtomic(metadataPath, meta); err != nil {
		return domain.JobResult{}, err
	}

	requestedEngine := normalizeEngine(options.TTSEngine)
	voice := p.Catalog.Resolve(options.VoiceKey, effectiveLanguage, requestedEngine)
	audioPath := filepath.Join(outputDir, "reading.wav")
	emit(0.55, "Generating speech")
	engine := p.Piper
	engineUsed := "piper"
	if requestedEngine == "kokoro" && voice.Engine == "kokoro" && p.Kokoro != nil {
		engine = p.Kokoro
		engineUsed = "kokoro"
	}
	voice, err = engine.Synthesize(ctx, cleanedText, audioPath, options, voice, func(inner float64, step string) {
		emit(0.55+inner*0.43, step)
	})
	if err != nil && engineUsed == "kokoro" {
		log.Printf("ReadMyPaper: Kokoro failed; falling back to Piper: %v", err)
		_ = os.Remove(audioPath)
		voice = p.Catalog.Resolve("auto", effectiveLanguage, "piper")
		voice, err = p.Piper.Synthesize(ctx, cleanedText, audioPath, options, voice, func(inner float64, step string) {
			emit(0.55+inner*0.43, "Piper fallback: "+step)
		})
		engineUsed = "piper"
	}
	if err != nil {
		return domain.JobResult{}, err
	}

	meta.Voice = &voice
	meta.EngineUsed = engineUsed
	if err := writeJSONAtomic(metadataPath, meta); err != nil {
		return domain.JobResult{}, err
	}
	emit(1, "Done")
	return domain.JobResult{
		CleanedTextPath: textPath, AudioPath: audioPath, OriginalPDFPath: pdfPath,
		DetectedLanguage: effectiveLanguage, EngineUsed: engineUsed, Stats: &stats,
	}, nil
}

func validateOptions(options *domain.ProcessingOptions, settings config.Settings) error {
	if options.SpeechRate < settings.SpeechRateMin || options.SpeechRate > settings.SpeechRateMax {
		return fmt.Errorf("speech rate must be between %.2f and %.2f", settings.SpeechRateMin, settings.SpeechRateMax)
	}
	if options.ChunkMaxChars <= 0 {
		options.ChunkMaxChars = 900
	}
	if options.PauseMS < 0 {
		options.PauseMS = 0
	}
	if options.CreatedAt == "" {
		options.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if options.UseLLMCleaner && strings.TrimSpace(options.LLMBaseURL) == "" && strings.TrimSpace(settings.LLMBaseURL) == "" {
		return fmt.Errorf("LLM base URL is required when the LLM cleaner is enabled")
	}
	return nil
}

func normalizeEngine(engine string) string {
	if strings.EqualFold(strings.TrimSpace(engine), "kokoro") {
		return "kokoro"
	}
	return "piper"
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWriteFile(path, data, 0o644)
}

func atomicWriteFile(path string, contents []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.Write(contents); err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := replaceFile(temporary, path); err != nil {
		return err
	}
	committed = true
	return nil
}

// replaceFile uses atomic rename where the platform supports replacement. On
// Windows, os.Rename cannot overwrite an existing file, so a retry after
// removing the old regular file is required for metadata updates.
func replaceFile(source, destination string) error {
	firstErr := os.Rename(source, destination)
	if firstErr == nil {
		return nil
	}
	info, statErr := os.Lstat(destination)
	if statErr != nil || info.IsDir() {
		return firstErr
	}
	if err := os.Remove(destination); err != nil {
		return fmt.Errorf("replace destination: %w", err)
	}
	if err := os.Rename(source, destination); err != nil {
		return fmt.Errorf("commit replacement: %w", err)
	}
	return nil
}
