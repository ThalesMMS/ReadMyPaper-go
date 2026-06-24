package cleaner

import (
	"testing"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
)

func block(text string, page int, left, top, right, bottom float64) domain.ExtractedBlock {
	box := domain.BBox{Left: left, Top: top, Right: right, Bottom: bottom}
	return domain.ExtractedBlock{Text: text, Label: "paragraph", PageNo: page, BBox: &box}
}

func textsOf(blocks []domain.ExtractedBlock) []string {
	result := make([]string, len(blocks))
	for index, item := range blocks {
		result[index] = item.Text
	}
	return result
}

func assertTexts(t *testing.T, got []domain.ExtractedBlock, want []string) {
	t.Helper()
	values := textsOf(got)
	if len(values) != len(want) {
		t.Fatalf("length %d, want %d: %#v", len(values), len(want), values)
	}
	for index := range want {
		if values[index] != want[index] {
			t.Fatalf("order %#v, want %#v", values, want)
		}
	}
}

func TestTwoColumnReadingOrder(t *testing.T) {
	blocks := []domain.ExtractedBlock{
		block("right-top", 1, 320, 100, 590, 130), block("left-top", 1, 30, 100, 270, 130),
		block("left-bottom", 1, 30, 200, 270, 230), block("right-bottom", 1, 320, 200, 590, 230),
	}
	ordered := RepairReadingOrder(blocks, map[int]domain.PageSize{1: {Width: 612, Height: 792}})
	assertTexts(t, ordered, []string{"left-top", "left-bottom", "right-top", "right-bottom"})
}

func TestThreeColumnReadingOrder(t *testing.T) {
	blocks := []domain.ExtractedBlock{
		block("c3-top", 1, 430, 100, 590, 130), block("c2-bottom", 1, 220, 250, 380, 280),
		block("c1-bottom", 1, 30, 250, 170, 280), block("c2-top", 1, 220, 100, 380, 130),
		block("c3-bottom", 1, 430, 250, 590, 280), block("c1-top", 1, 30, 100, 170, 130),
	}
	ordered := RepairReadingOrder(blocks, map[int]domain.PageSize{1: {Width: 612, Height: 792}})
	assertTexts(t, ordered, []string{"c1-top", "c1-bottom", "c2-top", "c2-bottom", "c3-top", "c3-bottom"})
}

func TestSpanningBlockSplitsSegments(t *testing.T) {
	blocks := []domain.ExtractedBlock{
		block("right-bottom", 1, 330, 320, 590, 350), block("left-bottom", 1, 30, 320, 270, 350),
		block("heading", 1, 30, 230, 590, 260), block("right-top", 1, 330, 120, 590, 150),
		block("left-top", 1, 30, 120, 270, 150),
	}
	ordered := RepairReadingOrder(blocks, map[int]domain.PageSize{1: {Width: 612, Height: 792}})
	assertTexts(t, ordered, []string{"left-top", "right-top", "heading", "left-bottom", "right-bottom"})
}

func TestNoBBoxPreservesOrder(t *testing.T) {
	blocks := []domain.ExtractedBlock{{Text: "a"}, {Text: "b"}}
	assertTexts(t, RepairReadingOrder(blocks, nil), []string{"a", "b"})
}
