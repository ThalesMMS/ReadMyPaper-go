package cleaner

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
	"golang.org/x/text/unicode/norm"
)

var keepLabels = map[string]bool{
	"title": true, "section_header": true, "paragraph": true, "text": true, "list_item": true,
}

var dropLabels = map[string]bool{
	"caption": true, "chart": true, "code": true, "document_index": true,
	"footnote": true, "formula": true, "page_footer": true, "page_header": true,
	"picture": true, "reference": true, "table": true,
}

func compilePatterns(patterns ...string) []*regexp.Regexp {
	result := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		// Go's RE2 syntax has no non-capturing groups; ordinary groups are
		// equivalent for our matching-only use.
		pattern = strings.ReplaceAll(pattern, "(?:", "(")
		result = append(result, regexp.MustCompile("(?i)"+pattern))
	}
	return result
}

var whitelistSections = compilePatterns(
	`^abstract$`, `^resumo$`, `^introduction$`, `^introdu[cç][aã]o$`, `^background$`, `^contexto$`,
	`^methods?$`, `^m[eé]todos?$`, `^materials?\s+and\s+methods?$`, `^materiais?\s+e\s+m[eé]todos?$`,
	`^methodology$`, `^metodologia$`, `^experimental\s+(setup|design|methods?)$`,
	`^results?$`, `^resultados?$`, `^results?\s+and\s+discussion$`, `^resultados?\s+e\s+discuss[aã]o$`,
	`^discussion$`, `^discuss[aã]o$`, `^conclusions?$`, `^conclus[oõ]es?$`, `^limitations?$`,
	`^limita[cç][oõ]es?$`, `^related\s+work$`, `^trabalhos?\s+relacionados?$`,
	`^literature\s+review$`, `^revis[aã]o\s+(da\s+)?literatura$`, `^overview$`,
	`^proposed\s+(method|approach|framework|model|system)$`, `^implementation$`, `^implementa[cç][aã]o$`,
	`^evaluation$`, `^avalia[cç][aã]o$`, `^analysis$`, `^an[aá]lise$`, `^experiments?$`, `^experimentos?$`,
	`^case\s+stud(y|ies)$`, `^estudo(s)?\s+de\s+caso$`, `^clinical\s+(significance|implications?)$`,
	`^future\s+(work|directions?)$`, `^trabalhos?\s+futuros?$`, `^\d+\.?\s+`,
)

var referenceSectionPatterns = compilePatterns(
	`^references?$`, `^bibliography$`, `^refer[eê]ncias?$`, `^bibliografia$`, `^literature\s+cited$`,
)

var acknowledgementSectionPatterns = compilePatterns(
	`^acknowledge?ments?$`, `^agradecimentos?$`, `^funding$`, `^financiamento$`,
	`^author\s+contributions?$`, `^contribui[cç][oõ]es?\s+dos?\s+autores?$`,
	`^conflicts?\s+of\s+interest$`, `^conflitos?\s+de\s+interesse$`, `^competing\s+interests?$`,
	`^ethics?\s*(statement|approval)?$`, `^declara[cç][aã]o\s+de\s+[eé]tica$`,
	`^declarations?$`, `^declara[cç][oõ]es?$`, `^data\s+availability\s*(statement)?$`,
	`^disponibilidade\s+(de\s+)?dados$`, `^code\s+availability$`,
	`^consent\s+(for\s+)?(publication|to\s+participate)?$`, `^consentimento$`,
	`^open\s+access$`, `^acesso\s+aberto$`, `^publisher['’]?s?\s+note$`, `^nota\s+do\s+editor$`,
	`^author\s+(details?|information)$`, `^(about|information\s+about)\s+(the\s+)?authors?$`,
	`^corresponding\s+author$`, `^how\s+to\s+cite$`, `^como\s+citar$`, `^credit\s+author$`,
	`^informed\s+consent$`,
)

var appendixSectionPatterns = compilePatterns(
	`^supplement(ary|al)?\s+(materials?|information|data)$`, `^material\s+suplementar$`,
	`^informa[cç][aã]o\s+suplementar$`, `^appendi(x|ces)$`, `^ap[eê]ndice(s)?$`,
	`^additional\s+files?$`, `^abbreviations?$`, `^abrevia[cç][oõ]es?$`,
)

var articleTypeSections = compilePatterns(
	`^technical\s+note$`, `^research\s+article$`, `^original\s+(article|research|paper)$`,
	`^review\s+article$`, `^case\s+report$`, `^short\s+communication$`,
	`^letter\s+to\s+(the\s+)?editor$`, `^brief\s+report$`,
)

var frontMatterPatterns = compilePatterns(
	`^Received:?\s`, `^Accepted:?\s`, `^Published:?\s`, `^Revised:?\s`, `^Submitted:?\s`,
	`^Available\s+online:?\s`, `^https?://`, `^doi:\s*10\.`, `^\d+\.\d+/`,
	`^Keywords?:?\s`, `^Key\s*words?:?\s`, `^Palavras[\-\s]?chave:?\s`,
	`^Corresponding\s+author`, `^[*†‡§¶‖]\s*\w`, `^e[\-\s]?mail:`, `^Email:`, `^ORCID:`,
	`^©\s`, `^Copyright\s`, `^ISSN\s`, `^Volume\s+\d`, `^Article\s+(ID|number|info)`,
	`^Cite\s+(this|as)`, `^Editor:?\s`, `^Academic\s+Editor`, `^Handling\s+editor`, `^Reviewer`,
	`^TECHNICAL\s+NOTE`, `^RESEARCH\s+ARTICLE`, `^ORIGINAL\s+(ARTICLE|RESEARCH|PAPER)`,
	`^REVIEW\s+ARTICLE`, `^CASE\s+REPORT`, `^SHORT\s+COMMUNICATION`,
	`^LETTER\s+TO\s+(THE\s+)?EDITOR`, `^BRIEF\s+REPORT`,
)

var endMatterPrefixes = []string{
	"Supplementary Information", "Supplementary Material", "Additional file", "Publisher's Note",
	"Publisher's note", "Springer Nature", "Open Access This article", "Creative Commons",
	"How to cite this article", "This article is licensed under", "This is an open access article",
	"This is an Open Access article", "Distributed under the terms", "Disponível em:",
	"Acesso aberto",
}

var inlineEndMatterPatterns = compilePatterns(
	`^Ethics\s+approval\b`, `^Consent\s+to\s+(participate|publication)\b`,
	`^Consent\s+for\s+publication\b`, `^Data\s+availability\b`, `^Code\s+availability\b`,
	`^Author\s+contributions?\b`, `^Acknowledgements?\b`, `^Acknowledgments?\b`,
	`^Conflicts?\s+of\s+interest\b`, `^Competing\s+interests?\b`, `^Funding\b`,
	`^Declarations?\b`, `^Informed\s+consent\b`,
)

var (
	numericCitationRE      = regexp.MustCompile(`\s*\[(\d+\s*([-–,;]\s*\d+)*)\]`)
	parenNumericCitationRE = regexp.MustCompile(`\s*\((\d+\s*([-–,;]\s*\d+)*)\)`)
	multiSpaceTextRE       = regexp.MustCompile(`[ \t]{2,}`)
	multiNewlineRE         = regexp.MustCompile(`\n{3,}`)
	captionRE              = regexp.MustCompile(`(?i)^(figure|fig\.?|table|figura|tabela|supplementary figure|supplementary table)\s+\d+`)
	pageNumberRE           = regexp.MustCompile(`(?i)^(page\s+)?\d{1,4}$`)
	tablePipeRE            = regexp.MustCompile(`\|.+\|`)
	tableSpacesRE          = regexp.MustCompile(`\S+\s{3,}\S+\s{3,}\S+`)
	numericHeavyRE         = regexp.MustCompile(`^[\d\s.,;%/()+\-–=]+$`)
	affiliationRE          = regexp.MustCompile(`(?i)^(\d{1,2}\s+)?(Department|Faculty|School|Institute|Center|Centre|Laboratory|Hospital|University|College|Universidade|Faculdade|Instituto|Departamento|Laboratório|Hospital)\b`)
	emailRE                = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`)
	orcidRE                = regexp.MustCompile(`\d{4}-\d{4}-\d{4}-\d{3}[\dX]`)
	numberedReferenceRE    = regexp.MustCompile(`^(\[\d+\]\s*|\d{1,3}\.\s+)[\p{Lu}][\p{Ll}]`)
	referenceLeadRE        = regexp.MustCompile(`^[\p{Lu}][\p{Ll}]+([-'][\pL]+)*\s+[\p{Lu}]{1,3}(,|\s+)`)
	referenceEvidenceRE    = regexp.MustCompile(`(?i)(\d+:\d+|pp\.?\s+\d+|vol\.?\s+\d+|Proc\.|Conf\.|Sci\s+Rep|Nature|Lancet|BMJ|Radiol|Neurosurg|Psychiatry|Imaging|Med\s|Surg\s|Biomed|PLoS|arXiv|Springer|Elsevier|Wiley|Ann[.,]|Cham[.,]|Int\s+J\s|Eur\s+J\s|Am\s+J\s|Br\s+J\s|\(eds?\)|In:\s|https?://|doi\.?\s*(org|10\.)|Pathol|Oncol|Clin\s|Acta\s|Comput|Inform)`)
	hyphenWhitespaceRE     = regexp.MustCompile(`([\pL\pN])-\s+([\pL\pN])`)
	spacedInitialRE        = regexp.MustCompile(`^([A-Z])\s+([a-z]{2,}\b)`)
	spaceBeforePunctRE     = regexp.MustCompile(`\s+([,.;:])`)
	leadingNumberHeadingRE = regexp.MustCompile(`^\d+\.?\s+`)
)

type ScientificTextCleaner struct {
	Options domain.ProcessingOptions
}

func NewScientificTextCleaner(options domain.ProcessingOptions) ScientificTextCleaner {
	return ScientificTextCleaner{Options: options}
}

func (c ScientificTextCleaner) Clean(blocks []domain.ExtractedBlock, pageCount int) (string, domain.CleaningStats) {
	stats := domain.NewCleaningStats(pageCount)
	stats.TotalBlocks = len(blocks)
	repeated := findRepeatedFurniture(blocks)
	parts := make([]string, 0, len(blocks))
	skipSection := false

	for _, block := range blocks {
		label := normalizeLabel(block.Label)
		text := normalizeText(block.Text)
		if text == "" {
			drop(&stats, "empty", false)
			continue
		}
		if dropLabels[label] {
			if label == "reference" {
				skipSection = true
			}
			drop(&stats, label, true)
			continue
		}
		if label != "" && !keepLabels[label] && looksLikeNonProse(text) {
			drop(&stats, "non_prose:"+label, false)
			continue
		}
		if repeated[strings.ToLower(text)] {
			drop(&stats, "repeated_furniture", false)
			continue
		}
		if label == "title" || label == "section_header" {
			switch c.classifyHeading(text) {
			case "drop":
				skipSection = true
				drop(&stats, "section_heading_drop", false)
				continue
			case "drop_heading_only":
				drop(&stats, "section_heading_drop", false)
				continue
			default:
				skipSection = false
				if c.Options.KeepHeadings {
					parts = append(parts, text)
					stats.KeptBlocks++
				}
				continue
			}
		}
		if skipSection {
			drop(&stats, "section_skip", false)
			continue
		}
		rule := ""
		switch {
		case captionRE.MatchString(text):
			rule = "caption_like"
		case pageNumberRE.MatchString(text):
			rule = "page_number"
		case looksLikeTableLine(text):
			rule = "table_like"
		case looksLikeNonProse(text):
			rule = "non_prose"
		case matchesAny(text, frontMatterPatterns):
			rule = "front_matter"
		case startsWithAny(text, endMatterPrefixes):
			rule = "end_matter"
		case matchesAny(text, inlineEndMatterPatterns):
			rule = "inline_end_matter"
		case looksLikeReferenceEntry(text):
			rule = "reference_entry"
		case looksLikeAffiliation(text):
			rule = "affiliation"
		}
		if rule != "" {
			drop(&stats, rule, false)
			continue
		}
		if c.Options.RemoveNumericCitations {
			text = removeNumericCitations(text)
			text = normalizeText(text)
			if text == "" {
				drop(&stats, "citations_only", false)
				continue
			}
		}
		parts = append(parts, text)
		stats.KeptBlocks++
	}
	cleaned := mergeParts(parts)
	stats.DroppedBlocks = stats.TotalBlocks - stats.KeptBlocks
	stats.DetectedLanguage = DetectLanguage(cleaned)
	return cleaned, stats
}

func (c ScientificTextCleaner) classifyHeading(heading string) string {
	normalized := normalizeText(heading)
	stripped := strings.TrimSpace(leadingNumberHeadingRE.ReplaceAllString(normalized, ""))
	if c.Options.DropReferencesSection && matchesAny(stripped, referenceSectionPatterns) {
		return "drop"
	}
	if c.Options.DropAcknowledgements && matchesAny(stripped, acknowledgementSectionPatterns) {
		return "drop"
	}
	if c.Options.DropAppendices && matchesAny(stripped, appendixSectionPatterns) {
		return "drop"
	}
	if matchesAny(stripped, articleTypeSections) {
		return "drop_heading_only"
	}
	if matchesAny(stripped, whitelistSections) || leadingNumberHeadingRE.MatchString(normalized) {
		return "keep"
	}
	return "unknown"
}

func (c ScientificTextCleaner) SplitText(text string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = c.Options.ChunkMaxChars
	}
	if maxChars <= 0 {
		maxChars = 900
	}
	normalized := mergeParts([]string{text})
	if utf8.RuneCountInString(normalized) <= maxChars {
		if normalized == "" {
			return nil
		}
		return []string{normalized}
	}
	sentences := splitSentences(normalized)
	chunks := make([]string, 0)
	current := ""
	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		proposal := strings.TrimSpace(current + " " + sentence)
		switch {
		case current != "" && utf8.RuneCountInString(proposal) > maxChars:
			chunks = append(chunks, current)
			if utf8.RuneCountInString(sentence) > maxChars {
				chunks = append(chunks, splitLongSentence(sentence, maxChars)...)
				current = ""
			} else {
				current = sentence
			}
		case utf8.RuneCountInString(sentence) > maxChars:
			chunks = append(chunks, splitLongSentence(sentence, maxChars)...)
			current = ""
		default:
			current = proposal
		}
	}
	if current != "" {
		chunks = append(chunks, current)
	}
	result := chunks[:0]
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk) != "" {
			result = append(result, strings.TrimSpace(chunk))
		}
	}
	return result
}

func DetectLanguage(text string) string {
	sample := strings.ToLower(text)
	sampleRunes := []rune(sample)
	if len(sampleRunes) > 12000 {
		sample = string(sampleRunes[:12000])
	}
	if strings.TrimSpace(sample) == "" {
		return "unknown"
	}
	portugueseMarkers := []string{"ção", "ções", " não ", " para ", " com ", " uma ", " este ", " são ", " foi ", " dos ", " das ", " entre ", " resultados", " métodos"}
	englishMarkers := []string{" the ", " and ", " of ", " to ", " in ", " was ", " were ", " results", " methods", " patients", " study "}
	ptScore, enScore := 0, 0
	padded := " " + sample + " "
	for _, marker := range portugueseMarkers {
		ptScore += strings.Count(padded, marker)
	}
	for _, marker := range englishMarkers {
		enScore += strings.Count(padded, marker)
	}
	if strings.ContainsAny(sample, "ãõçáéíóúâêô") {
		ptScore += 3
	}
	if ptScore > enScore && ptScore >= 2 {
		return "pt-BR"
	}
	return "en"
}

func normalizeLabel(label string) string { return strings.ToLower(strings.TrimSpace(label)) }

func normalizeText(text string) string {
	text = norm.NFKC.String(text)
	text = strings.ReplaceAll(text, "\u00ad", "")
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = hyphenWhitespaceRE.ReplaceAllString(text, `$1$2`)
	text = spacedInitialRE.ReplaceAllString(text, `$1$2`)
	text = strings.ReplaceAll(text, "•", "")
	text = multiSpaceTextRE.ReplaceAllString(text, " ")
	return strings.Trim(text, " \t\n\r")
}

func removeNumericCitations(text string) string {
	text = numericCitationRE.ReplaceAllString(text, "")
	text = parenNumericCitationRE.ReplaceAllString(text, "")
	return strings.TrimSpace(spaceBeforePunctRE.ReplaceAllString(text, `$1`))
}

func mergeParts(parts []string) string {
	merged := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if len(merged) > 0 && shouldMergeContinuation(merged[len(merged)-1], part) {
			merged[len(merged)-1] = mergeContinuation(merged[len(merged)-1], part)
		} else {
			merged = append(merged, part)
		}
	}
	result := strings.Join(merged, "\n\n")
	return strings.TrimSpace(multiNewlineRE.ReplaceAllString(result, "\n\n"))
}

func shouldMergeContinuation(previous, current string) bool {
	previous = strings.TrimRightFunc(previous, unicode.IsSpace)
	current = strings.TrimLeftFunc(current, unicode.IsSpace)
	if previous == "" || current == "" {
		return false
	}
	first, _ := utf8.DecodeRuneInString(current)
	if strings.HasSuffix(previous, "-") && unicode.IsLower(first) {
		return true
	}
	if strings.Count(previous, "(") > strings.Count(previous, ")") {
		return true
	}
	if looksLikeShortHeading(previous) {
		return false
	}
	return unicode.IsLower(first) && !endsSentence(previous)
}

func mergeContinuation(previous, current string) string {
	previous = strings.TrimRightFunc(previous, unicode.IsSpace)
	current = strings.TrimLeftFunc(current, unicode.IsSpace)
	first, _ := utf8.DecodeRuneInString(current)
	if strings.HasSuffix(previous, "-") && unicode.IsLower(first) {
		return strings.TrimSuffix(previous, "-") + current
	}
	return previous + " " + current
}

func endsSentence(text string) bool {
	text = strings.TrimRight(text, ")]}'\"")
	return strings.HasSuffix(text, ".") || strings.HasSuffix(text, "?") || strings.HasSuffix(text, "!")
}

func looksLikeShortHeading(text string) bool {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > 80 || strings.ContainsAny(text, ".!?") {
		return false
	}
	words := strings.Fields(text)
	if len(words) == 0 || len(words) > 8 {
		return false
	}
	for _, word := range words {
		clean := strings.Trim(word, "()[]{}:;,\"")
		first, _ := utf8.DecodeRuneInString(clean)
		if !(unicode.IsUpper(first) || isAllUpper(clean) || strings.Contains(clean, "/") || strings.EqualFold(clean, "of")) {
			return false
		}
	}
	return true
}

func isAllUpper(text string) bool {
	hasLetter := false
	for _, r := range text {
		if unicode.IsLetter(r) {
			hasLetter = true
			if unicode.IsLower(r) {
				return false
			}
		}
	}
	return hasLetter
}

func looksLikeTableLine(text string) bool {
	if tablePipeRE.MatchString(text) || tableSpacesRE.MatchString(text) {
		return true
	}
	tokens := strings.Fields(text)
	if len(tokens) < 6 {
		return false
	}
	numeric := 0
	for _, token := range tokens {
		cleaned := strings.Trim(token, ".,;%()[]")
		if isNumericToken(cleaned) {
			numeric++
		}
	}
	return float64(numeric)/float64(len(tokens)) > 0.55
}

func isNumericToken(token string) bool {
	if token == "" {
		return false
	}
	dotSeen := false
	for _, r := range token {
		if r == '.' && !dotSeen {
			dotSeen = true
			continue
		}
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func looksLikeNonProse(text string) bool {
	if utf8.RuneCountInString(text) <= 2 {
		return true
	}
	if numericHeavyRE.MatchString(text) && utf8.RuneCountInString(text) < 40 {
		return true
	}
	return strings.Count(text, "=") >= 2
}

func looksLikeReferenceEntry(text string) bool {
	length := utf8.RuneCountInString(text)
	if length < 30 || length > 1000 {
		return false
	}
	return numberedReferenceRE.MatchString(text) || (referenceLeadRE.MatchString(text) && referenceEvidenceRE.MatchString(text))
}

func looksLikeAffiliation(text string) bool {
	if utf8.RuneCountInString(text) > 300 {
		return false
	}
	if affiliationRE.MatchString(text) {
		return true
	}
	if utf8.RuneCountInString(text) < 100 {
		return (emailRE.MatchString(text) && len(strings.Fields(text)) < 15) || orcidRE.MatchString(text)
	}
	return false
}

func matchesAny(text string, patterns []*regexp.Regexp) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

func startsWithAny(text string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func findRepeatedFurniture(blocks []domain.ExtractedBlock) map[string]bool {
	counts := make(map[string]int)
	for _, block := range blocks {
		text := strings.ToLower(normalizeText(block.Text))
		if text == "" || utf8.RuneCountInString(text) > 90 {
			continue
		}
		counts[text]++
	}
	result := make(map[string]bool)
	for text, count := range counts {
		if count >= 3 {
			result[text] = true
		}
	}
	return result
}

func drop(stats *domain.CleaningStats, key string, byLabel bool) {
	bucket := stats.DroppedByRule
	if byLabel {
		bucket = stats.DroppedByLabel
	}
	bucket[key]++
}

func splitSentences(text string) []string {
	runes := []rune(text)
	start := 0
	parts := make([]string, 0)
	for index, r := range runes {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		next := index + 1
		for next < len(runes) && unicode.IsSpace(runes[next]) {
			next++
		}
		if next < len(runes) && (unicode.IsUpper(runes[next]) || unicode.IsDigit(runes[next])) {
			parts = append(parts, string(runes[start:index+1]))
			start = next
			index = next - 1
		}
	}
	if start < len(runes) {
		parts = append(parts, string(runes[start:]))
	}
	return parts
}

func splitLongSentence(sentence string, limit int) []string {
	parts := splitOnPunctuation(sentence)
	chunks := make([]string, 0)
	current := ""
	for _, part := range parts {
		proposal := strings.TrimSpace(current + " " + part)
		if current != "" && utf8.RuneCountInString(proposal) > limit {
			chunks = append(chunks, current)
			current = part
		} else {
			current = proposal
		}
	}
	if current != "" {
		if utf8.RuneCountInString(current) <= limit {
			chunks = append(chunks, current)
		} else {
			chunks = append(chunks, splitByWords(current, limit)...)
		}
	}
	return chunks
}

func splitOnPunctuation(text string) []string {
	runes := []rune(text)
	start := 0
	result := make([]string, 0)
	for index, r := range runes {
		if r != ',' && r != ';' && r != ':' {
			continue
		}
		end := index + 1
		if end < len(runes) && unicode.IsSpace(runes[end]) {
			result = append(result, strings.TrimSpace(string(runes[start:end])))
			for end < len(runes) && unicode.IsSpace(runes[end]) {
				end++
			}
			start = end
		}
	}
	if start < len(runes) {
		result = append(result, strings.TrimSpace(string(runes[start:])))
	}
	return result
}

func splitByWords(text string, limit int) []string {
	if limit <= 0 {
		limit = 900
	}
	words := strings.Fields(text)
	chunks := make([]string, 0)
	current := ""
	flush := func() {
		if current != "" {
			chunks = append(chunks, current)
			current = ""
		}
	}
	for _, word := range words {
		wordRunes := []rune(word)
		if len(wordRunes) > limit {
			flush()
			for len(wordRunes) > limit {
				chunks = append(chunks, string(wordRunes[:limit]))
				wordRunes = wordRunes[limit:]
			}
			if len(wordRunes) > 0 {
				current = string(wordRunes)
			}
			continue
		}
		proposal := strings.TrimSpace(current + " " + word)
		if current != "" && utf8.RuneCountInString(proposal) > limit {
			flush()
			current = word
		} else {
			current = proposal
		}
	}
	flush()
	return chunks
}

// SortedDropRules returns stable keys for diagnostics and tests.
func SortedDropRules(stats domain.CleaningStats) []string {
	keys := make([]string, 0, len(stats.DroppedByRule))
	for key := range stats.DroppedByRule {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
