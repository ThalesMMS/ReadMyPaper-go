package domain

import (
	"math"
	"time"
)

// JobStatus describes the lifecycle of a processing job.
type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
)

// BBox uses PDF points in left, top, right, bottom order. Extractors may use
// either a top-left or bottom-left origin; reading-order code normalizes it.
type BBox struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
}

func (b BBox) Width() float64  { return math.Abs(b.Right - b.Left) }
func (b BBox) Height() float64 { return math.Abs(b.Bottom - b.Top) }

// ExtractedBlock is the common representation shared by PDF extraction,
// reading-order repair, deterministic cleaning, and optional LLM review.
type ExtractedBlock struct {
	Text     string  `json:"text"`
	Label    string  `json:"label"`
	PageNo   int     `json:"page_no,omitempty"`
	BBox     *BBox   `json:"bbox,omitempty"`
	FontName string  `json:"font_name,omitempty"`
	FontSize float64 `json:"font_size,omitempty"`
}

// LayoutRegion marks a graphical obstacle such as a picture, table, caption,
// or large drawn rectangle. Text overlapping these regions is usually noise.
type LayoutRegion struct {
	Kind   string `json:"kind"`
	PageNo int    `json:"page_no"`
	BBox   BBox   `json:"bbox"`
}

type PageSize struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type ExtractionResult struct {
	Blocks        []ExtractedBlock `json:"blocks"`
	PageCount     int              `json:"page_count"`
	PageSizes     map[int]PageSize `json:"page_sizes"`
	LayoutRegions []LayoutRegion   `json:"layout_regions"`
}

// ProcessingOptions mirrors the Python implementation while adding explicit
// executable overrides for the command-backed TTS engines.
type ProcessingOptions struct {
	Language               string  `json:"language"`
	VoiceKey               string  `json:"voice_key"`
	SpeechRate             float64 `json:"speech_rate"`
	RemoveNumericCitations bool    `json:"remove_numeric_citations"`
	DropReferencesSection  bool    `json:"drop_references_section"`
	DropAcknowledgements   bool    `json:"drop_acknowledgements"`
	DropAppendices         bool    `json:"drop_appendices"`
	KeepHeadings           bool    `json:"keep_headings"`
	ChunkMaxChars          int     `json:"chunk_max_chars"`
	PauseMS                int     `json:"pause_ms"`
	TTSEngine              string  `json:"tts_engine"`
	UseLLMCleaner          bool    `json:"use_llm_cleaner"`
	LLMBaseURL             string  `json:"llm_base_url,omitempty"`
	LLMModel               string  `json:"llm_model,omitempty"`
	LLMAPIKey              string  `json:"-"`
	JobID                  string  `json:"job_id"`
	Filename               string  `json:"filename"`
	CreatedAt              string  `json:"created_at"`
}

func DefaultProcessingOptions() ProcessingOptions {
	return ProcessingOptions{
		Language:               "auto",
		VoiceKey:               "auto",
		SpeechRate:             1.0,
		RemoveNumericCitations: true,
		DropReferencesSection:  true,
		DropAcknowledgements:   true,
		DropAppendices:         true,
		KeepHeadings:           true,
		ChunkMaxChars:          900,
		PauseMS:                220,
		TTSEngine:              "piper",
	}
}

type CleaningStats struct {
	Pages                int            `json:"pages"`
	TotalBlocks          int            `json:"total_blocks"`
	KeptBlocks           int            `json:"kept_blocks"`
	DroppedBlocks        int            `json:"dropped_blocks"`
	DroppedByLabel       map[string]int `json:"dropped_by_label"`
	DroppedByRule        map[string]int `json:"dropped_by_rule"`
	DetectedLanguage     string         `json:"detected_language"`
	ReadingOrderRepaired bool           `json:"reading_order_repaired"`
	LayoutRegionsFound   int            `json:"layout_regions_found"`
	LayoutFilterDropped  int            `json:"layout_filter_dropped"`
	LLMBlocksProcessed   int            `json:"llm_blocks_processed"`
	LLMBlocksDropped     int            `json:"llm_blocks_dropped"`
	LLMBlocksRewritten   int            `json:"llm_blocks_rewritten"`
}

func NewCleaningStats(pages int) CleaningStats {
	return CleaningStats{
		Pages:            pages,
		DroppedByLabel:   make(map[string]int),
		DroppedByRule:    make(map[string]int),
		DetectedLanguage: "unknown",
	}
}

type JobResult struct {
	CleanedTextPath  string         `json:"cleaned_text_path,omitempty"`
	AudioPath        string         `json:"audio_path,omitempty"`
	OriginalPDFPath  string         `json:"original_pdf_path,omitempty"`
	DetectedLanguage string         `json:"detected_language,omitempty"`
	EngineUsed       string         `json:"engine_used,omitempty"`
	Stats            *CleaningStats `json:"stats,omitempty"`
}

type JobState struct {
	JobID      string            `json:"job_id"`
	Filename   string            `json:"filename"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
	Status     JobStatus         `json:"status"`
	Step       string            `json:"step"`
	Progress   float64           `json:"progress"`
	Error      string            `json:"error,omitempty"`
	EngineUsed string            `json:"engine_used,omitempty"`
	Options    ProcessingOptions `json:"options"`
	Result     JobResult         `json:"result"`
}

func (j JobState) Clone() JobState {
	clone := j
	if j.Result.Stats != nil {
		stats := *j.Result.Stats
		stats.DroppedByLabel = cloneIntMap(j.Result.Stats.DroppedByLabel)
		stats.DroppedByRule = cloneIntMap(j.Result.Stats.DroppedByRule)
		clone.Result.Stats = &stats
	}
	return clone
}

func cloneIntMap(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
