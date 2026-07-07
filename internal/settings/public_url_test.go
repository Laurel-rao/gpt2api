package settings

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicURLFromRequestKeepsForwardedHostPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://server/site-assets/ref.mp4", nil)
	req.Header.Set("X-Forwarded-Proto", "http")
	req.Header.Set("X-Forwarded-Host", "123.207.53.152:8080")

	got := PublicURLFromRequest(req, "", "/site-assets/ref.mp4")
	want := "http://123.207.53.152:8080/site-assets/ref.mp4"
	if got != want {
		t.Fatalf("public url = %q, want %q", got, want)
	}
}

func TestPublicURLFromRequestAddsForwardedPortWhenHostHasNoPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://server/site-assets/ref.mp4", nil)
	req.Header.Set("X-Forwarded-Proto", "http")
	req.Header.Set("X-Forwarded-Host", "123.207.53.152")
	req.Header.Set("X-Forwarded-Port", "8080")

	got := PublicURLFromRequest(req, "", "/site-assets/ref.mp4")
	want := "http://123.207.53.152:8080/site-assets/ref.mp4"
	if got != want {
		t.Fatalf("public url = %q, want %q", got, want)
	}
}

func TestPublicURLFromRequestDoesNotAddDefaultPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://server/site-assets/ref.mp4", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "example.com")
	req.Header.Set("X-Forwarded-Port", "443")

	got := PublicURLFromRequest(req, "", "/site-assets/ref.mp4")
	want := "https://example.com/site-assets/ref.mp4"
	if got != want {
		t.Fatalf("public url = %q, want %q", got, want)
	}
}

func TestPublicURLFromRequestUsesConfiguredBase(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://server/site-assets/ref.mp4", nil)
	req.Header.Set("X-Forwarded-Host", "123.207.53.152:8080")

	got := PublicURLFromRequest(req, "https://cdn.example.com/", "site-assets/ref.mp4")
	want := "https://cdn.example.com/site-assets/ref.mp4"
	if got != want {
		t.Fatalf("public url = %q, want %q", got, want)
	}
}
