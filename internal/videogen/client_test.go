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

func TestBalanceUsesAccountBalanceEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/account/balance" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Balance{
			Credits:         984816,
			RechargeBalance: 7,
			FreeQuotas: []FreeQuota{{
				ModelID:        "uuid",
				ModelName:      "Seedream 5.0",
				RemainingCount: 3,
			}},
		})
	}))
	t.Cleanup(srv.Close)

	client := NewClient(Config{BaseURL: srv.URL, APIKey: "test-key"})
	got, err := client.Balance(context.Background())
	if err != nil {
		t.Fatalf("Balance error: %v", err)
	}
	if got.Credits != 984816 || got.RechargeBalance != 7 {
		t.Fatalf("unexpected balance: %+v", got)
	}
	if len(got.FreeQuotas) != 1 || got.FreeQuotas[0].ModelName != "Seedream 5.0" || got.FreeQuotas[0].RemainingCount != 3 {
		t.Fatalf("unexpected free quotas: %+v", got.FreeQuotas)
	}
	if got.DurationMs < 0 {
		t.Fatalf("duration should be non-negative: %d", got.DurationMs)
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
