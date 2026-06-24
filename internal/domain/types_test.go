package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProcessingOptionsDoesNotMarshalLLMAPIKey(t *testing.T) {
	options := DefaultProcessingOptions()
	options.UseLLMCleaner = true
	options.LLMAPIKey = "sk-secret"

	data, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-secret") || strings.Contains(string(data), "llm_api_key") {
		t.Fatalf("LLM API key leaked into JSON: %s", data)
	}
}
