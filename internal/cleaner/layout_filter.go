package cleaner

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
)

const defaultLayoutMargin = 12.0

var shortTitleCasePattern = regexp.MustCompile(`^[\p{Lu}][\p{Ll}]+(?:\s+(?:[\p{Lu}][\p{Ll}]+|[\p{Lu}]{2,}|\d+))*\s*$`)

// FilterByLayout removes text inside or immediately adjacent to pictures,
// tables, captions, and large graphical rectangles.
func FilterByLayout(blocks []domain.ExtractedBlock, regions []domain.LayoutRegion, margin float64) ([]domain.ExtractedBlock, int) {
	if len(regions) == 0 {
		return blocks, 0
	}
	if margin < 0 {
		margin = defaultLayoutMargin
	}
	byPage := make(map[int][]domain.BBox)
	for _, region := range regions {
		byPage[region.PageNo] = append(byPage[region.PageNo], expandBBox(region.BBox, margin))
	}
	kept := make([]domain.ExtractedBlock, 0, len(blocks))
	dropped := 0
	for _, block := range blocks {
		if shouldDropByLayout(block, byPage) {
			dropped++
			continue
		}
		kept = append(kept, block)
	}
	return kept, dropped
}

func shouldDropByLayout(block domain.ExtractedBlock, regions map[int][]domain.BBox) bool {
	if block.PageNo == 0 || block.BBox == nil {
		return false
	}
	pageRegions := regions[block.PageNo]
	if len(pageRegions) == 0 {
		return false
	}
	for _, region := range pageRegions {
		if bboxesOverlap(*block.BBox, region) {
			return true
		}
	}
	text := strings.TrimSpace(block.Text)
	if utf8.RuneCountInString(text) <= 60 && looksLikeTitleCaseNonProse(text) {
		for _, region := range pageRegions {
			if bboxesOverlap(*block.BBox, expandBBox(region, 30)) {
				return true
			}
		}
	}
	return false
}

func looksLikeTitleCaseNonProse(text string) bool {
	if shortTitleCasePattern.MatchString(text) {
		return true
	}
	words := strings.Fields(text)
	if len(words) == 0 || len(words) > 7 {
		return false
	}
	for _, word := range words {
		r, _ := utf8.DecodeRuneInString(strings.Trim(word, "()[]{}:;,."))
		if !unicode.IsUpper(r) && !allDigits(word) {
			return false
		}
	}
	return true
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func expandBBox(box domain.BBox, margin float64) domain.BBox {
	return domain.BBox{
		Left: box.Left - margin, Top: box.Top - margin,
		Right: box.Right + margin, Bottom: box.Bottom + margin,
	}
}

func bboxesOverlap(a, b domain.BBox) bool {
	al, ar := minMax(a.Left, a.Right)
	at, ab := minMax(a.Top, a.Bottom)
	bl, br := minMax(b.Left, b.Right)
	bt, bb := minMax(b.Top, b.Bottom)
	return !(ar <= bl || br <= al || ab <= bt || bb <= at)
}

func minMax(a, b float64) (float64, float64) {
	if a <= b {
		return a, b
	}
	return b, a
}
