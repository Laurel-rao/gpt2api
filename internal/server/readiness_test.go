package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/432539/gpt2api/internal/config"
	"github.com/432539/gpt2api/internal/videoworkflow"
)

func TestReadinessEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		check      func(context.Context) error
		wantStatus int
	}{
		{name: "ready", check: func(context.Context) error { return nil }, wantStatus: http.StatusOK},
		{name: "not ready", check: func(context.Context) error { return errors.New("database unavailable") }, wantStatus: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := New(&Deps{Config: &config.Config{}, ReadyCheck: tt.check})
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			router.ServeHTTP(recorder, req)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestVideoWorkflowRoutes(t *testing.T) {
	handler := videoworkflow.NewHandler(videoworkflow.NewService(nil))
	router := New(&Deps{Config: &config.Config{}, VideoWorkflowH: handler})
	want := map[string]bool{
		"GET /p/vwf/:version_id":                                             false,
		"HEAD /p/vwf/:version_id":                                            false,
		"GET /api/me/video-assets":                                           false,
		"POST /api/me/video-assets":                                          false,
		"DELETE /api/me/video-assets/:asset_id":                              false,
		"POST /api/me/video-assets/:asset_id/versions/:version_id/sign":      false,
		"POST /api/me/video-assets/:asset_id/versions/:version_id/transform": false,
		"POST /api/me/video-workflows/:id/runs":                              false,
		"GET /api/me/video-workflows/:id/runs":                               false,
		"GET /api/me/video-workflow-runs/:run_id":                            false,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Errorf("missing route %s", route)
		}
	}
}
