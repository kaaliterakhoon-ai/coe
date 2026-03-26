package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultOpenAIResponsesEndpoint = "https://api.openai.com/v1/responses"

const defaultCorrectionInstructions = "You are correcting ASR output for dictation. Preserve meaning. Fix punctuation, spacing, capitalization, and obvious ASR artifacts. Keep the original language. Do not add explanations. Return only the corrected text."

type OpenAICorrector struct {
	Endpoint   string
	Model      string
	APIKeyEnv  string
	Prompt     string
	HTTPClient *http.Client
}

func (c OpenAICorrector) Name() string {
	model := c.Model
	if model == "" {
		model = "gpt-4o-mini"
	}
	return "openai-" + model
}

func (c OpenAICorrector) Correct(ctx context.Context, input string) (Result, error) {
	keyEnv := c.APIKeyEnv
	if keyEnv == "" {
		keyEnv = "OPENAI_API_KEY"
	}
	apiKey := os.Getenv(keyEnv)
	if apiKey == "" {
		return Result{}, fmt.Errorf("missing OpenAI API key in %s", keyEnv)
	}

	model := c.Model
	if model == "" {
		model = "gpt-4o-mini"
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = defaultOpenAIResponsesEndpoint
	}
	instructions := c.Prompt
	if instructions == "" {
		instructions = defaultCorrectionInstructions
	}

	payload := map[string]any{
		"model":             model,
		"instructions":      instructions,
		"input":             input,
		"max_output_tokens": 300,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil && apiErr.Error.Message != "" {
			return Result{}, fmt.Errorf("openai correction failed: %s", apiErr.Error.Message)
		}
		return Result{}, fmt.Errorf("openai correction failed: %s", resp.Status)
	}

	var payloadResp responsePayload
	if err := json.NewDecoder(resp.Body).Decode(&payloadResp); err != nil {
		return Result{}, err
	}

	return Result{Text: strings.TrimSpace(payloadResp.text())}, nil
}

type responsePayload struct {
	OutputText string           `json:"output_text"`
	Output     []responseOutput `json:"output"`
}

type responseOutput struct {
	Type    string                  `json:"type"`
	Content []responseOutputContent `json:"content"`
}

type responseOutputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (p responsePayload) text() string {
	if strings.TrimSpace(p.OutputText) != "" {
		return p.OutputText
	}

	var parts []string
	for _, item := range p.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type != "output_text" {
				continue
			}
			text := strings.TrimSpace(content.Text)
			if text != "" {
				parts = append(parts, text)
			}
		}
	}

	return strings.Join(parts, "\n")
}
