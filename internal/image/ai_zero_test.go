package image

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAIZeroGenerationPayload(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGVsbG8="}]}`))
	}))
	defer srv.Close()

	c := NewAIZeroClient(AIZeroConfig{
		BaseURL:        srv.URL + "/v1",
		APIKey:         "test-key",
		Quality:        "low",
		Background:     "auto",
		OutputFormat:   "png",
		ResponseFormat: "url",
	})
	resp, err := c.generate(context.Background(), RunOptions{
		UpstreamModel: "gpt-image-2",
		Prompt:        "hello",
		N:             2,
		Size:          "1792x1024",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].B64JSON == "" {
		t.Fatalf("unexpected resp: %+v", resp)
	}
	if got["model"] != "gpt-image-2" || got["prompt"] != "hello" || got["size"] != "1536x1024" {
		t.Fatalf("unexpected payload: %#v", got)
	}
	if got["quality"] != "low" || got["background"] != "auto" ||
		got["output_format"] != "png" || got["response_format"] != "b64_json" {
		t.Fatalf("missing explicit image params: %#v", got)
	}
}

func TestAIZeroGenerationNormalizesUnsupportedModel(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGVsbG8="}]}`))
	}))
	defer srv.Close()

	c := NewAIZeroClient(AIZeroConfig{BaseURL: srv.URL + "/v1", APIKey: "test-key"})
	_, err := c.generate(context.Background(), RunOptions{
		UpstreamModel: "gpt-5-4",
		Prompt:        "hello",
		Size:          "1024x1024",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["model"] != "gpt-image-2" {
		t.Fatalf("unsupported model should fallback to gpt-image-2: %#v", got)
	}
}

func TestAIZeroEditPayloadUsesJSONImages(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type = %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGVsbG8="}]}`))
	}))
	defer srv.Close()

	c := NewAIZeroClient(AIZeroConfig{BaseURL: srv.URL + "/v1", APIKey: "test-key"})
	_, err := c.generate(context.Background(), RunOptions{
		UpstreamModel: "auto",
		Prompt:        "edit",
		N:             1,
		Size:          "1024x1536",
		References: []ReferenceImage{{
			Data:     []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},
			FileName: "ref.png",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	images, ok := got["images"].([]any)
	if !ok || len(images) != 1 {
		t.Fatalf("images missing: %#v", got)
	}
	if s, ok := images[0].(string); !ok || !strings.HasPrefix(s, "data:image/png;base64,") {
		t.Fatalf("unexpected image ref: %#v", images[0])
	}
	if got["model"] != "gpt-image-2" {
		t.Fatalf("auto model should default to gpt-image-2: %#v", got)
	}
}
