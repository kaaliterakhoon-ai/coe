package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"coe/internal/audio"
)

const (
	defaultOpenAITranscriptionEndpoint = "https://api.openai.com/v1/audio/transcriptions"
	maxOpenAIAudioUploadBytes          = 25 * 1024 * 1024
)

type OpenAIClient struct {
	Endpoint   string
	Model      string
	APIKeyEnv  string
	Language   string
	Prompt     string
	HTTPClient *http.Client
}

func (c OpenAIClient) Name() string {
	model := c.Model
	if model == "" {
		model = "gpt-4o-mini-transcribe"
	}
	return "openai-" + model
}

func (c OpenAIClient) Transcribe(ctx context.Context, capture audio.Result) (Result, error) {
	keyEnv := c.APIKeyEnv
	if keyEnv == "" {
		keyEnv = "OPENAI_API_KEY"
	}
	apiKey := os.Getenv(keyEnv)
	if apiKey == "" {
		return Result{}, fmt.Errorf("missing OpenAI API key in %s", keyEnv)
	}

	wav, err := audio.EncodeWAV(capture)
	if err != nil {
		return Result{}, err
	}
	if len(wav) > maxOpenAIAudioUploadBytes {
		return Result{}, fmt.Errorf("audio payload is %d bytes, over OpenAI 25 MB upload limit", len(wav))
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	fileWriter, err := writer.CreateFormFile("file", "speech.wav")
	if err != nil {
		return Result{}, err
	}
	if _, err := fileWriter.Write(wav); err != nil {
		return Result{}, err
	}

	model := c.Model
	if model == "" {
		model = "gpt-4o-mini-transcribe"
	}
	if err := writer.WriteField("model", model); err != nil {
		return Result{}, err
	}
	if err := writer.WriteField("response_format", "json"); err != nil {
		return Result{}, err
	}
	if c.Language != "" {
		if err := writer.WriteField("language", c.Language); err != nil {
			return Result{}, err
		}
	}
	if c.Prompt != "" {
		if err := writer.WriteField("prompt", c.Prompt); err != nil {
			return Result{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return Result{}, err
	}

	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = defaultOpenAITranscriptionEndpoint
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
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
			return Result{}, fmt.Errorf("openai transcription failed: %s", apiErr.Error.Message)
		}
		return Result{}, fmt.Errorf("openai transcription failed: %s", resp.Status)
	}

	var payload struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Result{}, err
	}

	return Result{Text: strings.TrimSpace(payload.Text)}, nil
}
