package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAICorrectorCorrect(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization header = %q", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}

		if payload["model"] != "gpt-4o-mini" {
			t.Fatalf("model = %v", payload["model"])
		}
		if payload["input"] != "hello,,world" {
			t.Fatalf("input = %v", payload["input"])
		}
		if payload["instructions"] == "" {
			t.Fatal("expected instructions")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output_text":"Hello, world."}`))
	}))
	defer server.Close()

	corrector := OpenAICorrector{
		Endpoint:   server.URL,
		Model:      "gpt-4o-mini",
		APIKeyEnv:  "OPENAI_API_KEY",
		HTTPClient: server.Client(),
	}

	result, err := corrector.Correct(context.Background(), "hello,,world")
	if err != nil {
		t.Fatalf("Correct() error = %v", err)
	}
	if result.Text != "Hello, world." {
		t.Fatalf("result.Text = %q", result.Text)
	}
}

func TestOpenAICorrectorCorrectFromOutputArray(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"output": [
				{
					"type": "message",
					"content": [
						{"type":"output_text","text":"Hello, world."}
					]
				}
			]
		}`))
	}))
	defer server.Close()

	corrector := OpenAICorrector{
		Endpoint:   server.URL,
		Model:      "gpt-4o-mini",
		APIKeyEnv:  "OPENAI_API_KEY",
		HTTPClient: server.Client(),
	}

	result, err := corrector.Correct(context.Background(), "hello,,world")
	if err != nil {
		t.Fatalf("Correct() error = %v", err)
	}
	if result.Text != "Hello, world." {
		t.Fatalf("result.Text = %q", result.Text)
	}
}

func TestOpenAICorrectorMissingAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	corrector := OpenAICorrector{}
	_, err := corrector.Correct(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("expected missing key error, got %v", err)
	}
}
