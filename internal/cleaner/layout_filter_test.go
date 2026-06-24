package cleaner

import (
	"testing"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
)

func TestFilterByLayout(t *testing.T) {
	normal := block("Normal paragraph", 1, 50, 100, 550, 130)
	axis := block("Axis label", 1, 200, 350, 350, 370)
	regions := []domain.LayoutRegion{{Kind: "picture", PageNo: 1, BBox: domain.BBox{Left: 180, Top: 300, Right: 400, Bottom: 500}}}
	kept, dropped := FilterByLayout([]domain.ExtractedBlock{normal, axis}, regions, 12)
	if dropped != 1 || len(kept) != 1 || kept[0].Text != normal.Text {
		t.Fatalf("kept=%#v dropped=%d", kept, dropped)
	}
}

func TestShortTitleNearRegionDropped(t *testing.T) {
	label := block("Class Activation Map", 1, 200, 280, 350, 295)
	regions := []domain.LayoutRegion{{Kind: "picture", PageNo: 1, BBox: domain.BBox{Left: 180, Top: 300, Right: 400, Bottom: 500}}}
	kept, dropped := FilterByLayout([]domain.ExtractedBlock{label}, regions, 12)
	if dropped != 1 || len(kept) != 0 {
		t.Fatalf("expected drop, got kept=%d dropped=%d", len(kept), dropped)
	}
}

func TestBoundingBoxOverlapNormalizesCoordinates(t *testing.T) {
	if !bboxesOverlap(domain.BBox{Left: 0, Top: 100, Right: 100, Bottom: 0}, domain.BBox{Left: 50, Top: 50, Right: 120, Bottom: 120}) {
		t.Fatal("expected overlap with mixed coordinate direction")
	}
}
