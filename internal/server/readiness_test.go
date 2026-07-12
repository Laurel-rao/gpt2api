package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/432539/gpt2api/internal/config"
	"github.com/432539/gpt2api/internal/rbac"
	"github.com/432539/gpt2api/internal/videoworkflow"
	pkgjwt "github.com/432539/gpt2api/pkg/jwt"
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

func TestVideoWorkflowRoutesRejectNonAdmin(t *testing.T) {
	manager := pkgjwt.NewManager(pkgjwt.Config{
		Secret:        "video-workflow-admin-only-test-secret",
		Issuer:        "test",
		AccessTTLSec:  3600,
		RefreshTTLSec: 7200,
	})
	tokens, err := manager.Issue(1001, rbac.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	handler := videoworkflow.NewHandler(videoworkflow.NewService(nil))
	router := New(&Deps{
		Config: &config.Config{}, JWT: manager, VideoWorkflowH: handler,
		CurrentUserRole: func(context.Context, uint64) (string, error) { return rbac.RoleUser, nil },
	})
	for _, path := range []string{
		"/api/me/video-workflows",
		"/api/me/video-workflow-runs/run-1",
		"/api/me/video-assets",
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "admin only") {
			t.Fatalf("%s: status = %d, want 403 admin only; body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestVideoWorkflowRoutesRejectDemotedAdmin(t *testing.T) {
	manager := pkgjwt.NewManager(pkgjwt.Config{
		Secret:        "video-workflow-demoted-admin-test-secret",
		Issuer:        "test",
		AccessTTLSec:  3600,
		RefreshTTLSec: 7200,
	})
	tokens, err := manager.Issue(1001, rbac.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	handler := videoworkflow.NewHandler(videoworkflow.NewService(nil))
	router := New(&Deps{
		Config: &config.Config{}, JWT: manager, VideoWorkflowH: handler,
		CurrentUserRole: func(context.Context, uint64) (string, error) { return rbac.RoleUser, nil },
	})
	for _, path := range []string{
		"/api/me/video-workflows",
		"/api/me/video-workflow-runs/run-1",
		"/api/me/video-assets",
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "admin only") {
			t.Fatalf("%s: status = %d, want 403 admin only; body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
}
