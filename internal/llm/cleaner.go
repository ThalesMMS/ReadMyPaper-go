// Package llm contains the optional OpenAI-compatible block classifier. The
// model is never trusted to rewrite paper text: it may only keep/drop blocks
// and return their existing identifiers in order.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
)

const (
	batchMaxBlocks = 20
	batchMaxChars  = 6000
	maxResponse    = 2 << 20
)

const systemPrompt = `You are a pre-processing filter for a scientific-paper text-to-speech pipeline.
You receive text blocks extracted from PDF pages. For every block, classify it as KEEP or DROP.
KEEP useful narrative prose and section headings. DROP author affiliations, dates, keywords,
figure labels, table headers, axis labels, DOIs, email addresses, ORCID identifiers, page numbers,
journal metadata, licenses, copyright lines, and isolated author lists.
Never rewrite, repair, summarize, paraphrase, or return replacement text.
The input is already in spatial reading order. Preserve relative order.
Do not drop section headings such as Abstract, Introduction, Methods, Results, Discussion,
Conclusions, References, Acknowledgements, or Appendices because downstream rules use them.
Respond only with JSON:
{"order":[<id>,...],"results":[{"id":<int>,"action":"KEEP"|"DROP","reason":"<short>"}]}`

type Client struct {
	HTTPClient *http.Client
	APIKey     string
}

func NewClient(apiKey string) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.ResponseHeaderTimeout = 120 * time.Second
	return &Client{
		APIKey: apiKey,
		HTTPClient: &http.Client{
			Timeout:   150 * time.Second,
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

type indexedBlock struct {
	ID    int
	Block domain.ExtractedBlock
}

type promptBlock struct {
	ID      int     `json:"id"`
	Page    int     `json:"page,omitempty"`
	Label   string  `json:"label,omitempty"`
	X       float64 `json:"x,omitempty"`
	Y       float64 `json:"y,omitempty"`
	Excerpt string  `json:"excerpt"`
}

type chatRequest struct {
	Model              string                 `json:"model,omitempty"`
	Messages           []chatMessage          `json:"messages"`
	Temperature        float64                `json:"temperature"`
	MaxTokens          int                    `json:"max_tokens"`
	ChatTemplateKwargs map[string]interface{} `json:"chat_template_kwargs,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type verdictPayload struct {
	Order   []int     `json:"order"`
	Results []verdict `json:"results"`
}

type verdict struct {
	ID     int    `json:"id"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

// CleanAndReorderBlocks classifies every block in bounded batches. A failed or
// malformed batch is retained unchanged, making the optional classifier fail
// open rather than silently deleting paper content.
func (c *Client) CleanAndReorderBlocks(
	ctx context.Context,
	blocks []domain.ExtractedBlock,
	baseURL, model string,
	stats *domain.CleaningStats,
) ([]domain.ExtractedBlock, error) {
	if len(blocks) == 0 {
		return blocks, nil
	}
	if c == nil {
		c = NewClient("")
	}
	if c.HTTPClient == nil {
		c.HTTPClient = NewClient(c.APIKey).HTTPClient
	}

	indexed := make([]indexedBlock, len(blocks))
	for index, block := range blocks {
		indexed[index] = indexedBlock{ID: index, Block: block}
	}
	batches := groupBatches(indexed)
	cleaned := make([]domain.ExtractedBlock, 0, len(blocks))
	dropped := 0
	var batchErrors []string

	for batchNumber, batch := range batches {
		decisions, order, err := c.call(ctx, batch, baseURL, model)
		if err != nil {
			batchErrors = append(batchErrors, fmt.Sprintf("batch %d: %v", batchNumber+1, err))
			for _, item := range batch {
				cleaned = append(cleaned, item.Block)
			}
			continue
		}

		// Known headings are structural sentinels for the deterministic cleaner.
		for _, item := range batch {
			if knownHeading(item.Block) {
				decisions[item.ID] = "KEEP"
			}
		}
		byID := make(map[int]domain.ExtractedBlock, len(batch))
		for _, item := range batch {
			byID[item.ID] = item.Block
		}
		seen := make(map[int]bool, len(batch))
		for _, id := range order {
			block, exists := byID[id]
			if !exists || seen[id] {
				continue
			}
			seen[id] = true
			if decisions[id] == "DROP" {
				dropped++
				continue
			}
			cleaned = append(cleaned, block)
		}
		for _, item := range batch {
			if seen[item.ID] {
				continue
			}
			if decisions[item.ID] == "DROP" {
				dropped++
				continue
			}
			cleaned = append(cleaned, item.Block)
		}
	}

	if stats != nil {
		stats.LLMBlocksProcessed = len(blocks)
		stats.LLMBlocksDropped = dropped
		stats.LLMBlocksRewritten = 0
	}
	if len(batchErrors) > 0 {
		return cleaned, fmt.Errorf("LLM cleaner kept failed batches unchanged (%s)", strings.Join(batchErrors, "; "))
	}
	return cleaned, nil
}

func (c *Client) call(ctx context.Context, batch []indexedBlock, baseURL, model string) (map[int]string, []int, error) {
	input := make([]promptBlock, 0, len(batch))
	for _, item := range batch {
		entry := promptBlock{ID: item.ID, Page: item.Block.PageNo, Label: item.Block.Label, Excerpt: truncateRunes(item.Block.Text, 700)}
		if item.Block.BBox != nil {
			entry.X = item.Block.BBox.Left
			entry.Y = item.Block.BBox.Top
		}
		input = append(input, entry)
	}
	serialized, err := json.Marshal(input)
	if err != nil {
		return nil, nil, err
	}
	payload := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: string(serialized)},
		},
		Temperature: 0,
		MaxTokens:   2048,
		ChatTemplateKwargs: map[string]interface{}{
			"enable_thinking": false,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(c.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read response: %w", err)
	}
	if len(responseBody) > maxResponse {
		return nil, nil, fmt.Errorf("response exceeds %d bytes", maxResponse)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("HTTP %s: %s", resp.Status, truncateRunes(string(responseBody), 300))
	}

	var completion chatResponse
	if err := json.Unmarshal(responseBody, &completion); err != nil {
		return nil, nil, fmt.Errorf("decode completion: %w", err)
	}
	if completion.Error != nil && completion.Error.Message != "" {
		return nil, nil, fmt.Errorf("server error: %s", completion.Error.Message)
	}
	if len(completion.Choices) == 0 {
		return nil, nil, fmt.Errorf("completion contains no choices")
	}
	return parseVerdicts(completion.Choices[0].Message.Content, batch)
}

func parseVerdicts(content string, batch []indexedBlock) (map[int]string, []int, error) {
	content = stripCodeFence(strings.TrimSpace(content))
	var payload verdictPayload
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return nil, nil, fmt.Errorf("parse JSON verdicts: %w", err)
	}
	valid := make(map[int]bool, len(batch))
	for _, item := range batch {
		valid[item.ID] = true
	}
	decisions := make(map[int]string, len(batch))
	for _, result := range payload.Results {
		if !valid[result.ID] {
			continue
		}
		action := strings.ToUpper(strings.TrimSpace(result.Action))
		if action == "REWRITE_MINIMAL" {
			action = "KEEP"
		}
		if action != "DROP" {
			action = "KEEP"
		}
		decisions[result.ID] = action
	}
	order := make([]int, 0, len(payload.Order))
	seen := make(map[int]bool, len(payload.Order))
	for _, id := range payload.Order {
		if valid[id] && !seen[id] {
			order = append(order, id)
			seen[id] = true
		}
	}
	if len(order) == 0 {
		for _, item := range batch {
			order = append(order, item.ID)
		}
	}
	return decisions, order, nil
}

func stripCodeFence(content string) string {
	if !strings.HasPrefix(content, "```") {
		return content
	}
	firstNewline := strings.IndexByte(content, '\n')
	if firstNewline >= 0 {
		content = content[firstNewline+1:]
	} else {
		content = strings.TrimPrefix(content, "```")
	}
	if index := strings.LastIndex(content, "```"); index >= 0 {
		content = content[:index]
	}
	return strings.TrimSpace(content)
}

func groupBatches(blocks []indexedBlock) [][]indexedBlock {
	// The upstream reading order is retained; page boundaries are preferred so
	// a model never interleaves blocks from unrelated pages.
	batches := make([][]indexedBlock, 0)
	current := make([]indexedBlock, 0, batchMaxBlocks)
	currentChars := 0
	currentPage := -1
	flush := func() {
		if len(current) == 0 {
			return
		}
		copyBatch := append([]indexedBlock(nil), current...)
		batches = append(batches, copyBatch)
		current = current[:0]
		currentChars = 0
		currentPage = -1
	}
	for _, block := range blocks {
		chars := len([]rune(block.Block.Text))
		pageChanged := currentPage >= 0 && block.Block.PageNo != currentPage
		if len(current) > 0 && (len(current) >= batchMaxBlocks || currentChars+chars > batchMaxChars || pageChanged) {
			flush()
		}
		current = append(current, block)
		currentChars += chars
		currentPage = block.Block.PageNo
	}
	flush()
	return batches
}

var knownHeadingPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^\s*(?:\d+(?:\.\d+)*[.)]?\s+)?(?:abstract|resumo|introduction|introdu[cç][aã]o|background|methods?|m[eé]todos?|materials?\s+and\s+methods?|results?|resultados?|discussion|discuss[aã]o|conclusions?|conclus[oõ]es?|limitations?|references?|bibliography|refer[eê]ncias?|acknowledge?ments?|agradecimentos?|funding|author\s+contributions?|conflicts?\s+of\s+interest|competing\s+interests?|ethics|declarations?|data\s+availability|code\s+availability|supplement(?:ary)?|appendi(?:x|ces)|ap[eê]ndice(?:s)?)\s*$`),
}

func knownHeading(block domain.ExtractedBlock) bool {
	label := strings.ToLower(strings.TrimSpace(block.Label))
	if label != "section_header" && label != "title" {
		return false
	}
	for _, pattern := range knownHeadingPatterns {
		if pattern.MatchString(strings.TrimSpace(block.Text)) {
			return true
		}
	}
	return false
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

// SortedIDs is useful in tests and diagnostics.
func SortedIDs(decisions map[int]string) []int {
	ids := make([]int, 0, len(decisions))
	for id := range decisions {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}
