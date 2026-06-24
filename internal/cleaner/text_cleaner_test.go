package cleaner

import (
	"strings"
	"testing"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
)

func TestScientificCleanerDropsFrontAndEndMatter(t *testing.T) {
	blocks := []domain.ExtractedBlock{
		{Text: "RESEARCH ARTICLE", Label: "section_header", PageNo: 1},
		{Text: "Deep Learning for Medical Imaging", Label: "title", PageNo: 1},
		{Text: "1 Department of Radiology, University Hospital", Label: "paragraph", PageNo: 1},
		{Text: "Received: 5 January 2024", Label: "paragraph", PageNo: 1},
		{Text: "Abstract", Label: "section_header", PageNo: 1},
		{Text: "This study evaluates an AI-assisted diagnostic system.", Label: "paragraph", PageNo: 1},
		{Text: "Methods", Label: "section_header", PageNo: 2},
		{Text: "We included 2070 anonymized images [12].", Label: "paragraph", PageNo: 2},
		{Text: "Results", Label: "section_header", PageNo: 3},
		{Text: "The model achieved 99 percent accuracy.", Label: "paragraph", PageNo: 3},
		{Text: "Acknowledgements", Label: "section_header", PageNo: 4},
		{Text: "We thank all contributors.", Label: "paragraph", PageNo: 4},
		{Text: "References", Label: "section_header", PageNo: 5},
		{Text: "Smith JA. Example reference. Nature 1:10-20", Label: "paragraph", PageNo: 5},
	}
	cleaned, stats := NewScientificTextCleaner(domain.DefaultProcessingOptions()).Clean(blocks, 5)
	for _, expected := range []string{"Deep Learning for Medical Imaging", "AI-assisted", "2070 anonymized images", "99 percent"} {
		if !strings.Contains(cleaned, expected) {
			t.Errorf("missing %q in %q", expected, cleaned)
		}
	}
	for _, forbidden := range []string{"RESEARCH ARTICLE", "Department of Radiology", "Received:", "Acknowledgements", "contributors", "References", "Smith JA"} {
		if strings.Contains(cleaned, forbidden) {
			t.Errorf("leaked %q in %q", forbidden, cleaned)
		}
	}
	if strings.Contains(cleaned, "[12]") {
		t.Errorf("numeric citation was not removed: %q", cleaned)
	}
	if stats.KeptBlocks == 0 || stats.DroppedBlocks == 0 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
}

func TestChunkingRespectsRuneLimit(t *testing.T) {
	cleaner := NewScientificTextCleaner(domain.DefaultProcessingOptions())
	chunks := cleaner.SplitText(strings.Repeat("A sentence with scientific content. ", 30), 120)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks: %#v", chunks)
	}
	for _, chunk := range chunks {
		if len([]rune(chunk)) > 120 {
			t.Fatalf("chunk exceeds limit: %d", len([]rune(chunk)))
		}
	}
}

func TestDetectLanguage(t *testing.T) {
	if got := DetectLanguage("Este estudo foi realizado com pacientes e os resultados são relevantes para a prática clínica."); got != "pt-BR" {
		t.Fatalf("got %q", got)
	}
	if got := DetectLanguage("The study was performed in patients and the results were relevant to clinical practice."); got != "en" {
		t.Fatalf("got %q", got)
	}
}

func TestChunkingSplitsSingleOversizedToken(t *testing.T) {
	cleaner := NewScientificTextCleaner(domain.DefaultProcessingOptions())
	chunks := cleaner.SplitText(strings.Repeat("á", 301), 100)
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d: %#v", len(chunks), chunks)
	}
	for _, chunk := range chunks {
		if len([]rune(chunk)) > 100 {
			t.Fatalf("oversized chunk: %d runes", len([]rune(chunk)))
		}
	}
}
