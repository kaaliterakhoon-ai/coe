package asr

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"coe/internal/audio"
)

func TestOpenAIClientTranscribe(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization header = %q", got)
		}

		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("MultipartReader() error = %v", err)
		}

		fields := map[string]string{}
		var fileData []byte

		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("NextPart() error = %v", err)
			}

			data, err := io.ReadAll(part)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}

			if part.FormName() == "file" {
				fileData = data
				continue
			}
			fields[part.FormName()] = string(data)
		}

		if fields["model"] != "gpt-4o-mini-transcribe" {
			t.Fatalf("model = %q", fields["model"])
		}
		if fields["language"] != "zh" {
			t.Fatalf("language = %q", fields["language"])
		}
		if fields["response_format"] != "json" {
			t.Fatalf("response_format = %q", fields["response_format"])
		}
		if string(fileData[:4]) != "RIFF" || string(fileData[8:12]) != "WAVE" {
			t.Fatalf("expected WAV upload, got %q / %q", string(fileData[:4]), string(fileData[8:12]))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"  hello world  "}`))
	}))
	defer server.Close()

	client := OpenAIClient{
		Endpoint:   server.URL,
		Model:      "gpt-4o-mini-transcribe",
		APIKeyEnv:  "OPENAI_API_KEY",
		Language:   "zh",
		HTTPClient: server.Client(),
	}

	result, err := client.Transcribe(context.Background(), audio.Result{
		Data:       []byte{0x01, 0x02, 0x03, 0x04},
		ByteCount:  4,
		SampleRate: 16000,
		Channels:   1,
		Format:     "s16",
	})
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if result.Text != "hello world" {
		t.Fatalf("result.Text = %q", result.Text)
	}
}

func TestOpenAIClientMissingAPIKey(t *testing.T) {
	if err := os.Unsetenv("OPENAI_API_KEY"); err != nil {
		t.Fatalf("Unsetenv() error = %v", err)
	}

	client := OpenAIClient{}
	_, err := client.Transcribe(context.Background(), audio.Result{
		Data:       []byte{0x01, 0x02},
		ByteCount:  2,
		SampleRate: 16000,
		Channels:   1,
		Format:     "s16",
	})
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("expected missing key error, got %v", err)
	}
}
