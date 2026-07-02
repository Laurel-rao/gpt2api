package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLogoutIsIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewHandler(nil)
	r := gin.New()
	r.POST("/api/auth/logout", h.Logout)
	r.POST("/_gateway/auth/logout", h.Logout)

	for _, path := range []string{"/api/auth/logout", "/_gateway/auth/logout"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, w.Code, http.StatusOK)
		}
	}
}
