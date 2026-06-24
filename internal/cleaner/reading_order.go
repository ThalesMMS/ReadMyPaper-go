package cleaner

import (
	"math"
	"sort"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
)

const (
	fullWidthRatio               = 0.60
	columnGapRatio               = 0.10
	columnDetectionMaxWidthRatio = 0.50
	maxColumns                   = 4
	columnSpanMargin             = 4.0
	yTolerance                   = 3.0
)

type indexedBlock struct {
	index int
	block domain.ExtractedBlock
}

type column struct {
	center float64
	left   float64
	right  float64
}

// RepairReadingOrder detects text columns on each page and orders blocks
// top-to-bottom within each column while preserving full-width anchors.
func RepairReadingOrder(blocks []domain.ExtractedBlock, pageSizes map[int]domain.PageSize) []domain.ExtractedBlock {
	if len(blocks) == 0 {
		return blocks
	}
	byPage := make(map[int][]indexedBlock)
	pageOrder := make([]int, 0)
	seen := make(map[int]bool)
	for index, block := range blocks {
		page := block.PageNo
		byPage[page] = append(byPage[page], indexedBlock{index: index, block: block})
		if !seen[page] {
			seen[page] = true
			pageOrder = append(pageOrder, page)
		}
	}
	sort.SliceStable(pageOrder, func(i, j int) bool {
		if pageOrder[i] == 0 {
			return false
		}
		if pageOrder[j] == 0 {
			return true
		}
		return pageOrder[i] < pageOrder[j]
	})

	ordered := make([]domain.ExtractedBlock, 0, len(blocks))
	for _, page := range pageOrder {
		size, ok := pageSizes[page]
		if page == 0 || !ok || size.Width <= 0 || size.Height <= 0 {
			for _, item := range byPage[page] {
				ordered = append(ordered, item.block)
			}
			continue
		}
		ordered = append(ordered, orderPage(byPage[page], size.Width, size.Height)...)
	}
	return ordered
}

func orderPage(pageBlocks []indexedBlock, pageWidth, pageHeight float64) []domain.ExtractedBlock {
	withBBox := make([]indexedBlock, 0, len(pageBlocks))
	withoutBBox := make([]indexedBlock, 0)
	for _, item := range pageBlocks {
		if item.block.BBox != nil {
			withBBox = append(withBBox, item)
		} else {
			withoutBBox = append(withoutBBox, item)
		}
	}
	if len(withBBox) == 0 {
		result := make([]domain.ExtractedBlock, 0, len(pageBlocks))
		for _, item := range pageBlocks {
			result = append(result, item.block)
		}
		return result
	}

	columns := detectColumns(withBBox, pageWidth)
	if len(columns) <= 1 {
		combined := append(append([]indexedBlock{}, withBBox...), withoutBBox...)
		sort.SliceStable(combined, func(i, j int) bool {
			yi := bboxY(combined[i].block, pageHeight)
			yj := bboxY(combined[j].block, pageHeight)
			if yi == yj {
				return combined[i].index < combined[j].index
			}
			return yi < yj
		})
		result := make([]domain.ExtractedBlock, 0, len(combined))
		for _, item := range combined {
			result = append(result, item.block)
		}
		return result
	}

	spanning := make([]indexedBlock, 0)
	remaining := make([]columnItem, 0)
	for _, item := range withBBox {
		if spansMultipleColumns(item.block, columns, pageWidth) {
			spanning = append(spanning, item)
		} else {
			remaining = append(remaining, columnItem{column: nearestColumn(item.block, columns), item: item})
		}
	}
	sort.SliceStable(spanning, func(i, j int) bool {
		yi := bboxY(spanning[i].block, pageHeight)
		yj := bboxY(spanning[j].block, pageHeight)
		if yi == yj {
			return spanning[i].index < spanning[j].index
		}
		return yi < yj
	})

	result := make([]domain.ExtractedBlock, 0, len(pageBlocks))
	for _, span := range spanning {
		spanY := bboxY(span.block, pageHeight)
		before := make([]columnItem, 0)
		keep := make([]columnItem, 0, len(remaining))
		for _, item := range remaining {
			if bboxY(item.item.block, pageHeight) < spanY-yTolerance {
				before = append(before, item)
			} else {
				keep = append(keep, item)
			}
		}
		result = append(result, orderColumnSegment(before, len(columns), pageHeight)...)
		remaining = keep
		result = append(result, span.block)
	}
	result = append(result, orderColumnSegment(remaining, len(columns), pageHeight)...)
	for _, item := range withoutBBox {
		result = append(result, item.block)
	}
	return result
}

type columnCandidate struct {
	midpoint float64
	left     float64
	right    float64
}

type columnItem struct {
	column int
	item   indexedBlock
}

func detectColumns(blocks []indexedBlock, pageWidth float64) []column {
	fallback := []column{{center: pageWidth / 2, left: 0, right: pageWidth}}
	if len(blocks) < 3 {
		return fallback
	}
	maxWidth := pageWidth * columnDetectionMaxWidthRatio
	candidates := make([]columnCandidate, 0, len(blocks))
	for _, item := range blocks {
		box := item.block.BBox
		if box == nil {
			continue
		}
		width := box.Right - box.Left
		if width <= 0 || width >= maxWidth {
			continue
		}
		candidates = append(candidates, columnCandidate{
			midpoint: (box.Left + box.Right) / 2,
			left:     box.Left,
			right:    box.Right,
		})
	}
	if len(candidates) < 3 {
		return fallback
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].midpoint < candidates[j].midpoint })
	minGap := pageWidth * columnGapRatio
	clusters := [][]columnCandidate{{candidates[0]}}
	for _, candidate := range candidates[1:] {
		lastCluster := clusters[len(clusters)-1]
		previous := lastCluster[len(lastCluster)-1].midpoint
		if candidate.midpoint-previous >= minGap {
			clusters = append(clusters, []columnCandidate{candidate})
		} else {
			clusters[len(clusters)-1] = append(clusters[len(clusters)-1], candidate)
		}
	}
	if len(clusters) <= 1 {
		return fallback
	}
	for len(clusters) > maxColumns {
		mergeAt := closestClusterPair(clusters)
		clusters[mergeAt] = append(clusters[mergeAt], clusters[mergeAt+1]...)
		clusters = append(clusters[:mergeAt+1], clusters[mergeAt+2:]...)
		sort.Slice(clusters[mergeAt], func(i, j int) bool {
			return clusters[mergeAt][i].midpoint < clusters[mergeAt][j].midpoint
		})
	}
	columns := make([]column, 0, len(clusters))
	for _, cluster := range clusters {
		left := math.Inf(1)
		right := math.Inf(-1)
		for _, item := range cluster {
			left = math.Min(left, item.left)
			right = math.Max(right, item.right)
		}
		columns = append(columns, column{center: clusterCenter(cluster), left: left, right: right})
	}
	sort.Slice(columns, func(i, j int) bool { return columns[i].center < columns[j].center })
	return columns
}

func closestClusterPair(clusters [][]columnCandidate) int {
	bestIndex := 0
	bestGap := math.Inf(1)
	for index := 0; index < len(clusters)-1; index++ {
		gap := clusterCenter(clusters[index+1]) - clusterCenter(clusters[index])
		if gap < bestGap {
			bestGap = gap
			bestIndex = index
		}
	}
	return bestIndex
}

func clusterCenter(cluster []columnCandidate) float64 {
	var total float64
	for _, item := range cluster {
		total += item.midpoint
	}
	return total / float64(len(cluster))
}

func spansMultipleColumns(block domain.ExtractedBlock, columns []column, pageWidth float64) bool {
	if block.BBox == nil || len(columns) <= 1 {
		return false
	}
	box := block.BBox
	if box.Right-box.Left >= pageWidth*fullWidthRatio {
		return true
	}
	covered := 0
	for _, col := range columns {
		if box.Left-columnSpanMargin <= col.center && col.center <= box.Right+columnSpanMargin {
			covered++
		}
	}
	return covered >= 2
}

func nearestColumn(block domain.ExtractedBlock, columns []column) int {
	if block.BBox == nil {
		return 0
	}
	midpoint := (block.BBox.Left + block.BBox.Right) / 2
	bestIndex := 0
	bestDistance := math.Inf(1)
	for index, col := range columns {
		distance := math.Abs(col.center - midpoint)
		if distance < bestDistance {
			bestDistance = distance
			bestIndex = index
		}
	}
	return bestIndex
}

func orderColumnSegment(items []columnItem, nColumns int, pageHeight float64) []domain.ExtractedBlock {
	byColumn := make(map[int][]indexedBlock)
	for _, item := range items {
		byColumn[item.column] = append(byColumn[item.column], item.item)
	}
	ordered := make([]domain.ExtractedBlock, 0, len(items))
	for columnIndex := 0; columnIndex < nColumns; columnIndex++ {
		columnBlocks := byColumn[columnIndex]
		sort.SliceStable(columnBlocks, func(i, j int) bool {
			yi := bboxY(columnBlocks[i].block, pageHeight)
			yj := bboxY(columnBlocks[j].block, pageHeight)
			if yi == yj {
				return columnBlocks[i].index < columnBlocks[j].index
			}
			return yi < yj
		})
		for _, item := range columnBlocks {
			ordered = append(ordered, item.block)
		}
	}
	return ordered
}

func bboxY(block domain.ExtractedBlock, pageHeight float64) float64 {
	if block.BBox == nil {
		return 0
	}
	if block.BBox.Top <= block.BBox.Bottom {
		return block.BBox.Top
	}
	return pageHeight - block.BBox.Top
}
