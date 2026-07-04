package middleware

import "testing"

func TestIsAssetFetchPath(t *testing.T) {
	for _, path := range []string{
		"/assets/index.js",
		"/ecommerce-assets/videos/video_250/out.mp4",
		"/site-assets/logo.png",
		"/p/img/task/0",
		"/favicon.ico",
		"/apple-touch-icon.png",
	} {
		if !isAssetFetchPath(path) {
			t.Fatalf("expected asset path: %s", path)
		}
	}
	for _, path := range []string{"/api/me/menu", "/api/me/ecommerce/tasks", "/healthz"} {
		if isAssetFetchPath(path) {
			t.Fatalf("expected non asset path: %s", path)
		}
	}
}

func TestSlowRequestStatsRecord(t *testing.T) {
	stats := newSlowRequestStats()
	total, routeTotal := stats.Record("GET", "/api/me/menu", 200)
	if total != 1 || routeTotal != 1 {
		t.Fatalf("first record total=%d route=%d", total, routeTotal)
	}
	total, routeTotal = stats.Record("GET", "/api/me/menu", 200)
	if total != 2 || routeTotal != 2 {
		t.Fatalf("second same route total=%d route=%d", total, routeTotal)
	}
	total, routeTotal = stats.Record("GET", "/api/me", 200)
	if total != 3 || routeTotal != 1 {
		t.Fatalf("different route total=%d route=%d", total, routeTotal)
	}
}
