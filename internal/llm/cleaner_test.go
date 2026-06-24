package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
)

func TestCleanAndReorderBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"order\":[2,0,1],\"results\":[{\"id\":0,\"action\":\"KEEP\"},{\"id\":1,\"action\":\"DROP\"},{\"id\":2,\"action\":\"KEEP\"}]}"}}]}`))
	}))
	defer server.Close()

	blocks := []domain.ExtractedBlock{
		{Text: "Introduction", Label: "section_header", PageNo: 1},
		{Text: "john@example.org", Label: "paragraph", PageNo: 1},
		{Text: "Useful prose.", Label: "paragraph", PageNo: 1},
	}
	stats := domain.NewCleaningStats(1)
	client := NewClient("")
	got, err := client.CleanAndReorderBlocks(context.Background(), blocks, server.URL+"/v1", "local", &stats)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Text != "Useful prose." || got[1].Text != "Introduction" {
		t.Fatalf("unexpected output: %#v", got)
	}
	if stats.LLMBlocksProcessed != 3 || stats.LLMBlocksDropped != 1 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
}

func TestKnownHeadingCannotBeDropped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"order\":[0],\"results\":[{\"id\":0,\"action\":\"DROP\"}]}"}}]}`))
	}))
	defer server.Close()
	blocks := []domain.ExtractedBlock{{Text: "Methods", Label: "section_header", PageNo: 1}}
	got, err := NewClient("").CleanAndReorderBlocks(context.Background(), blocks, server.URL, "", nil)
	if err != nil || len(got) != 1 {
		t.Fatalf("heading should survive: got=%#v err=%v", got, err)
	}
}

func TestMalformedResponseFailsOpen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"not json"}}]}`))
	}))
	defer server.Close()
	blocks := []domain.ExtractedBlock{{Text: "Keep me", Label: "paragraph", PageNo: 1}}
	got, err := NewClient("").CleanAndReorderBlocks(context.Background(), blocks, server.URL, "", nil)
	if err == nil || !strings.Contains(err.Error(), "kept failed batches") || len(got) != 1 {
		t.Fatalf("expected fail-open warning: got=%#v err=%v", got, err)
	}
}
