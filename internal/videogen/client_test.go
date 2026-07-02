package videogen

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeModelsUsesModelIDAsValue(t *testing.T) {
	client, _ := testClientWithModels(t, []Model{
		{ID: "0f1ff0a3-fb26-4163-9820-1779ad9f5f13", Name: "Seedance-2.0-D", Type: "video"},
		{ID: "0e37fa2d-72b3-483a-81b4-ad595cd147c7", Name: "Seedance-2.0-D-V", Type: "video"},
	})
	_, _, modelName, models, err := client.ProbeModels(context.Background())
	if err != nil {
		t.Fatalf("ProbeModels error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models len = %d", len(models))
	}
	if modelName != "Seedance-2.0-D-V" {
		t.Fatalf("preferred model name = %q", modelName)
	}
	if models[0].Value != "0e37fa2d-72b3-483a-81b4-ad595cd147c7" {
		t.Fatalf("model value should use id, got %q", models[0].Value)
	}
}

func TestResolveDefaultAliasPrefersSeedanceDVModelID(t *testing.T) {
	client, _ := testClientWithModels(t, []Model{
		{ID: "0f1ff0a3-fb26-4163-9820-1779ad9f5f13", Name: "Seedance-2.0-D", Type: "video"},
		{ID: "0e37fa2d-72b3-483a-81b4-ad595cd147c7", Name: "Seedance-2.0-D-V", Type: "video"},
	})
	got, err := client.resolveModelID(context.Background(), Config{
		BaseURL: client.baseURL,
		APIKey:  "test-key",
		Model:   "Seedance 2.0",
	}, Options{})
	if err != nil {
		t.Fatalf("resolveModelID error: %v", err)
	}
	if got != "0e37fa2d-72b3-483a-81b4-ad595cd147c7" {
		t.Fatalf("model id = %q", got)
	}
}

func TestRuntimeConfigDefaultsToSeedanceDVModelID(t *testing.T) {
	client := NewClient(Config{APIKey: "test-key"})
	if got := client.runtimeConfig().Model; got != "0e37fa2d-72b3-483a-81b4-ad595cd147c7" {
		t.Fatalf("default model = %q", got)
	}
}

func testClientWithModels(t *testing.T, models []Model) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(models)
	}))
	t.Cleanup(srv.Close)
	return NewClient(Config{BaseURL: srv.URL, APIKey: "test-key"}), srv
}
