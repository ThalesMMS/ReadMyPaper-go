// Package pdfextract implements local, layout-aware extraction of scientific
// PDFs. It intentionally keeps the extracted representation independent from
// the PDF library so the rest of the pipeline can be tested without opening a
// document.
package pdfextract

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	pdf "github.com/ledongthuc/pdf"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
)

// Extractor is the pipeline-facing PDF extraction contract.
type Extractor interface {
	Extract(path string) (domain.ExtractionResult, error)
}

// NativeExtractor reads PDF text positions directly in Go. It does not upload
// document contents or require a remote service.
type NativeExtractor struct{}

func New() *NativeExtractor { return &NativeExtractor{} }

var (
	headingNumberRE = regexp.MustCompile(`^\s*(?:\d+(?:\.\d+)*[.)]?\s+)?[\pL][\pL\pN &/,:()\-–—]{1,100}$`)
	knownHeadingRE  = regexp.MustCompile(`(?i)^\s*(?:\d+(?:\.\d+)*[.)]?\s+)?(?:abstract|resumo|introduction|introdu[cç][aã]o|background|methods?|m[eé]todos?|materials?\s+and\s+methods?|results?|resultados?|discussion|discuss[aã]o|conclusions?|conclus[oõ]es?|limitations?|related\s+work|evaluation|experiments?|references?|bibliography|refer[eê]ncias?|acknowledge?ments?|agradecimentos?|funding|appendi(?:x|ces)|ap[eê]ndice(?:s)?)\s*$`)
	captionStartRE  = regexp.MustCompile(`(?i)^\s*(?:fig(?:ure|ura)?\.?|table|tabela|supplementary\s+(?:figure|table))\s*[A-Z]?\d+\b`)
	listStartRE     = regexp.MustCompile(`^\s*(?:[•●▪◦‣⁃]|[-–—]|\(?\d+[.)]|\(?[A-Za-z][.)])\s+`)
	mostlyMathRE    = regexp.MustCompile(`^[\pN\pS\pP\sA-Za-z]{1,160}$`)
)

type line struct {
	text       string
	font       string
	fontSize   float64
	left       float64
	right      float64
	baseline   float64
	top        float64
	bottom     float64
	label      string
	originalNo int
}

// Extract opens path and returns positioned text blocks and conservative
// graphical regions. Encrypted, malformed, or textless documents return a
// descriptive error.
func (NativeExtractor) Extract(path string) (result domain.ExtractionResult, err error) {
	file, reader, err := pdf.Open(path)
	if err != nil {
		return result, fmt.Errorf("open PDF: %w", err)
	}
	if file != nil {
		defer file.Close()
	}

	pageCount := reader.NumPage()
	if pageCount <= 0 {
		return result, errors.New("PDF has no readable pages")
	}
	result.PageCount = pageCount
	result.PageSizes = make(map[int]domain.PageSize, pageCount)

	for pageNo := 1; pageNo <= pageCount; pageNo++ {
		page := reader.Page(pageNo)
		if page.V.IsNull() {
			continue
		}
		content := safePageContent(page)
		width, height := pageDimensions(page, content.Text)
		result.PageSizes[pageNo] = domain.PageSize{Width: width, Height: height}

		pageLines := buildLines(content.Text)
		medianSize := medianFontSize(pageLines)
		for index := range pageLines {
			pageLines[index].label = classifyLine(pageLines[index], medianSize, height, pageNo)
		}
		blocks := groupLines(pageLines, pageNo, medianSize)
		result.Blocks = append(result.Blocks, blocks...)
		result.LayoutRegions = append(result.LayoutRegions, rectangleRegions(content.Rect, pageNo, width, height)...)
		for _, block := range blocks {
			if block.Label == "caption" && block.BBox != nil {
				result.LayoutRegions = append(result.LayoutRegions, domain.LayoutRegion{
					Kind: "caption", PageNo: pageNo, BBox: *block.BBox,
				})
			}
		}
	}

	if len(result.Blocks) == 0 {
		return result, errors.New("no selectable text was found; this may be a scanned PDF that requires OCR")
	}
	return result, nil
}

func safePageContent(page pdf.Page) (content pdf.Content) {
	defer func() {
		if recover() != nil {
			content = pdf.Content{}
		}
	}()
	return page.Content()
}

func pageDimensions(page pdf.Page, texts []pdf.Text) (float64, float64) {
	box := inheritedValue(page.V, "CropBox")
	if box.Len() < 4 {
		box = inheritedValue(page.V, "MediaBox")
	}
	if box.Len() >= 4 {
		x0, y0 := box.Index(0).Float64(), box.Index(1).Float64()
		x1, y1 := box.Index(2).Float64(), box.Index(3).Float64()
		width, height := math.Abs(x1-x0), math.Abs(y1-y0)
		if width > 1 && height > 1 {
			return width, height
		}
	}

	// A4/Letter-like fallback, expanded when positioned text lies outside it.
	width, height := 612.0, 792.0
	for _, item := range texts {
		width = math.Max(width, item.X+math.Abs(item.W)+18)
		height = math.Max(height, item.Y+math.Abs(item.FontSize)+18)
	}
	return width, height
}

func inheritedValue(value pdf.Value, key string) pdf.Value {
	for current := value; !current.IsNull(); current = current.Key("Parent") {
		candidate := current.Key(key)
		if !candidate.IsNull() {
			return candidate
		}
	}
	return pdf.Value{}
}

func buildLines(items []pdf.Text) []line {
	glyphs := make([]pdf.Text, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.S) == "" && item.S != " " && item.S != "\t" {
			continue
		}
		if item.FontSize <= 0 || math.IsNaN(item.X) || math.IsNaN(item.Y) {
			continue
		}
		glyphs = append(glyphs, item)
	}
	if len(glyphs) == 0 {
		return nil
	}

	// Baselines are clustered before horizontal sorting. PDF coordinates grow
	// bottom-to-top, hence the descending Y order.
	sort.SliceStable(glyphs, func(i, j int) bool {
		if math.Abs(glyphs[i].Y-glyphs[j].Y) <= 0.8 {
			return glyphs[i].X < glyphs[j].X
		}
		return glyphs[i].Y > glyphs[j].Y
	})

	clusters := make([][]pdf.Text, 0)
	for _, glyph := range glyphs {
		best := -1
		bestDistance := math.Inf(1)
		for index := len(clusters) - 1; index >= 0 && index >= len(clusters)-5; index-- {
			baseline := averageBaseline(clusters[index])
			tolerance := math.Max(1.5, math.Abs(glyph.FontSize)*0.32)
			distance := math.Abs(glyph.Y - baseline)
			if distance <= tolerance && distance < bestDistance {
				best = index
				bestDistance = distance
			}
		}
		if best < 0 {
			clusters = append(clusters, []pdf.Text{glyph})
		} else {
			clusters[best] = append(clusters[best], glyph)
		}
	}

	lines := make([]line, 0, len(clusters))
	for originalNo, cluster := range clusters {
		sort.SliceStable(cluster, func(i, j int) bool { return cluster[i].X < cluster[j].X })
		built := assembleLine(cluster)
		built.originalNo = originalNo
		if strings.TrimSpace(built.text) != "" {
			lines = append(lines, built)
		}
	}
	sort.SliceStable(lines, func(i, j int) bool {
		if math.Abs(lines[i].baseline-lines[j].baseline) <= 1.2 {
			return lines[i].left < lines[j].left
		}
		return lines[i].baseline > lines[j].baseline
	})
	return lines
}

func averageBaseline(items []pdf.Text) float64 {
	var total float64
	for _, item := range items {
		total += item.Y
	}
	return total / float64(len(items))
}

func assembleLine(items []pdf.Text) line {
	left, right := math.Inf(1), math.Inf(-1)
	bottom, top := math.Inf(1), math.Inf(-1)
	var sizeWeight, sizeTotal float64
	fontCounts := map[string]int{}
	var builder strings.Builder
	var previous *pdf.Text

	for index := range items {
		item := items[index]
		left = math.Min(left, item.X)
		right = math.Max(right, item.X+math.Max(item.W, 0))
		bottom = math.Min(bottom, item.Y)
		top = math.Max(top, item.Y+math.Abs(item.FontSize))
		weight := math.Max(math.Abs(item.W), 1)
		sizeTotal += math.Abs(item.FontSize) * weight
		sizeWeight += weight
		fontCounts[item.Font]++

		value := sanitizeGlyph(item.S)
		if value == "" {
			continue
		}
		if previous != nil && needsSpace(*previous, item, builder.String(), value) {
			builder.WriteByte(' ')
		}
		builder.WriteString(value)
		copy := item
		previous = &copy
	}

	text := strings.TrimSpace(strings.Join(strings.Fields(builder.String()), " "))
	return line{
		text: text, font: dominantFont(fontCounts), fontSize: safeDivide(sizeTotal, sizeWeight),
		left: left, right: right, baseline: bottom, top: top, bottom: bottom,
	}
}

func sanitizeGlyph(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsPrint(r) || unicode.IsSpace(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func needsSpace(previous, current pdf.Text, accumulated, value string) bool {
	if accumulated == "" || strings.HasSuffix(accumulated, " ") || strings.HasPrefix(value, " ") {
		return false
	}
	gap := current.X - (previous.X + math.Max(previous.W, 0))
	threshold := math.Max(0.75, math.Min(math.Abs(previous.FontSize), math.Abs(current.FontSize))*0.16)
	if gap > threshold {
		return true
	}
	last, _ := utf8.DecodeLastRuneInString(accumulated)
	first, _ := utf8.DecodeRuneInString(value)
	// Some generators report zero glyph widths. Add a space only when the
	// coordinate jump is meaningful and two word-like runes would concatenate.
	return previous.W <= 0 && gap > 0.8 && isWordRune(last) && isWordRune(first)
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func dominantFont(counts map[string]int) string {
	best, bestCount := "", -1
	for font, count := range counts {
		if count > bestCount {
			best, bestCount = font, count
		}
	}
	return best
}

func safeDivide(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func medianFontSize(lines []line) float64 {
	values := make([]float64, 0, len(lines))
	for _, item := range lines {
		if item.fontSize > 0 && utf8.RuneCountInString(item.text) >= 3 {
			values = append(values, item.fontSize)
		}
	}
	if len(values) == 0 {
		return 10
	}
	sort.Float64s(values)
	middle := len(values) / 2
	if len(values)%2 == 0 {
		return (values[middle-1] + values[middle]) / 2
	}
	return values[middle]
}

func classifyLine(item line, median, pageHeight float64, pageNo int) string {
	text := strings.TrimSpace(item.text)
	length := utf8.RuneCountInString(text)
	if captionStartRE.MatchString(text) {
		return "caption"
	}
	if pageHeight > 0 && length <= 100 {
		if item.top >= pageHeight*0.955 {
			return "page_header"
		}
		if item.bottom <= pageHeight*0.035 {
			return "page_footer"
		}
	}
	if looksFormula(text) {
		return "formula"
	}
	if looksTableRow(text) {
		return "table"
	}
	if listStartRE.MatchString(text) {
		return "list_item"
	}
	if pageNo == 1 && item.top >= pageHeight*0.62 && length <= 240 && item.fontSize >= median*1.35 {
		return "title"
	}
	if knownHeadingRE.MatchString(text) || (length <= 120 && item.fontSize >= median*1.18 && headingNumberRE.MatchString(text) && headingCapitalization(text)) {
		return "section_header"
	}
	return "paragraph"
}

func headingCapitalization(text string) bool {
	words := strings.Fields(strings.TrimSpace(text))
	if len(words) == 0 || len(words) > 14 || strings.HasSuffix(text, ".") {
		return false
	}
	capitalized := 0
	letters := 0
	for _, word := range words {
		word = strings.TrimLeft(word, "0123456789.)")
		r, _ := utf8.DecodeRuneInString(word)
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsUpper(r) {
				capitalized++
			}
		}
	}
	return letters > 0 && (capitalized*2 >= letters || strings.ToUpper(text) == text)
}

func looksFormula(text string) bool {
	if len(text) < 3 || len(text) > 160 || !mostlyMathRE.MatchString(text) {
		return false
	}
	operators := 0
	letters := 0
	for _, r := range text {
		if unicode.IsLetter(r) {
			letters++
		}
		if strings.ContainsRune("=∑∫√≈≠≤≥±→←∂∇", r) {
			operators++
		}
	}
	return operators >= 2 && letters < 25
}

func looksTableRow(text string) bool {
	fields := strings.Fields(text)
	if len(fields) < 5 || utf8.RuneCountInString(text) > 220 {
		return false
	}
	numeric := 0
	for _, field := range fields {
		trimmed := strings.Trim(field, "()[]{}.,;%±−-–—")
		if trimmed == "" {
			continue
		}
		isNumber := true
		for _, r := range trimmed {
			if !unicode.IsDigit(r) && r != '.' && r != ',' {
				isNumber = false
				break
			}
		}
		if isNumber {
			numeric++
		}
	}
	return float64(numeric)/float64(len(fields)) >= 0.55
}

func groupLines(lines []line, pageNo int, median float64) []domain.ExtractedBlock {
	if len(lines) == 0 {
		return nil
	}
	result := make([]domain.ExtractedBlock, 0, len(lines))
	current := lines[0]

	flush := func() {
		box := domain.BBox{Left: current.left, Top: current.top, Right: current.right, Bottom: current.bottom}
		result = append(result, domain.ExtractedBlock{
			Text: strings.TrimSpace(current.text), Label: current.label, PageNo: pageNo,
			BBox: &box, FontName: current.font, FontSize: current.fontSize,
		})
	}

	for _, next := range lines[1:] {
		if canMergeLines(current, next, median) {
			separator := " "
			if strings.HasSuffix(current.text, "-") && startsLowercase(next.text) {
				current.text = strings.TrimSuffix(current.text, "-")
				separator = ""
			}
			current.text += separator + next.text
			current.left = math.Min(current.left, next.left)
			current.right = math.Max(current.right, next.right)
			current.top = math.Max(current.top, next.top)
			current.bottom = math.Min(current.bottom, next.bottom)
			current.fontSize = math.Max(current.fontSize, next.fontSize)
			continue
		}
		flush()
		current = next
	}
	flush()
	return result
}

func canMergeLines(previous, current line, median float64) bool {
	if previous.label != current.label || (current.label != "paragraph" && current.label != "list_item") {
		return false
	}
	verticalGap := previous.bottom - current.top
	maxGap := math.Max(median*0.95, 5.0)
	if verticalGap < -median*0.35 || verticalGap > maxGap {
		return false
	}
	leftDifference := math.Abs(previous.left - current.left)
	width := math.Max(previous.right-previous.left, current.right-current.left)
	if leftDifference > math.Max(24, width*0.12) {
		return false
	}
	// Prevent accidental cross-column merging when the next line starts to the
	// right of the previous line's end.
	if current.left > previous.right+math.Max(18, median*2) {
		return false
	}
	return true
}

func startsLowercase(text string) bool {
	r, _ := utf8.DecodeRuneInString(strings.TrimSpace(text))
	return unicode.IsLower(r)
}

func rectangleRegions(rects []pdf.Rect, pageNo int, pageWidth, pageHeight float64) []domain.LayoutRegion {
	if pageWidth <= 0 || pageHeight <= 0 {
		return nil
	}
	pageArea := pageWidth * pageHeight
	regions := make([]domain.LayoutRegion, 0)
	for _, rect := range rects {
		left, right := math.Min(rect.Min.X, rect.Max.X), math.Max(rect.Min.X, rect.Max.X)
		bottom, top := math.Min(rect.Min.Y, rect.Max.Y), math.Max(rect.Min.Y, rect.Max.Y)
		width, height := right-left, top-bottom
		areaRatio := width * height / pageArea
		if width < 72 || height < 42 || areaRatio < 0.035 || areaRatio > 0.72 {
			continue
		}
		if left <= 4 && bottom <= 4 && right >= pageWidth-4 && top >= pageHeight-4 {
			continue
		}
		regions = append(regions, domain.LayoutRegion{
			Kind: "graphic", PageNo: pageNo,
			BBox: domain.BBox{Left: left, Top: top, Right: right, Bottom: bottom},
		})
	}
	return deduplicateRegions(regions)
}

func deduplicateRegions(regions []domain.LayoutRegion) []domain.LayoutRegion {
	result := make([]domain.LayoutRegion, 0, len(regions))
	for _, candidate := range regions {
		duplicate := false
		for _, existing := range result {
			if candidate.PageNo == existing.PageNo && bboxSimilarity(candidate.BBox, existing.BBox) > 0.92 {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, candidate)
		}
	}
	return result
}

func bboxSimilarity(a, b domain.BBox) float64 {
	left, right := math.Max(math.Min(a.Left, a.Right), math.Min(b.Left, b.Right)), math.Min(math.Max(a.Left, a.Right), math.Max(b.Left, b.Right))
	bottom, top := math.Max(math.Min(a.Top, a.Bottom), math.Min(b.Top, b.Bottom)), math.Min(math.Max(a.Top, a.Bottom), math.Max(b.Top, b.Bottom))
	intersection := math.Max(0, right-left) * math.Max(0, top-bottom)
	areaA := math.Abs((a.Right - a.Left) * (a.Bottom - a.Top))
	areaB := math.Abs((b.Right - b.Left) * (b.Bottom - b.Top))
	union := areaA + areaB - intersection
	if union <= 0 {
		return 0
	}
	return intersection / union
}
