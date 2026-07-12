package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/432539/gpt2api/internal/rbac"
)

func currentAdminStatus(claimedRole string, resolve UserRoleResolver) int {
	router := gin.New()
	router.GET("/",
		func(c *gin.Context) {
			c.Set(CtxUserID, uint64(1001))
			c.Set(CtxRole, claimedRole)
			c.Next()
		},
		RequireCurrentAdmin(resolve),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	return recorder.Code
}

func TestRequireCurrentAdmin(t *testing.T) {
	adminRole := func(context.Context, uint64) (string, error) { return rbac.RoleAdmin, nil }
	userRole := func(context.Context, uint64) (string, error) { return rbac.RoleUser, nil }
	lookupFailure := func(context.Context, uint64) (string, error) { return "", errors.New("database unavailable") }

	tests := []struct {
		name        string
		claimedRole string
		resolve     UserRoleResolver
		want        int
	}{
		{name: "current admin", claimedRole: rbac.RoleAdmin, resolve: adminRole, want: http.StatusNoContent},
		{name: "promoted admin", claimedRole: rbac.RoleUser, resolve: adminRole, want: http.StatusNoContent},
		{name: "demoted admin", claimedRole: rbac.RoleAdmin, resolve: userRole, want: http.StatusForbidden},
		{name: "lookup failure", claimedRole: rbac.RoleAdmin, resolve: lookupFailure, want: http.StatusInternalServerError},
		{name: "missing resolver", claimedRole: rbac.RoleAdmin, want: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := currentAdminStatus(test.claimedRole, test.resolve); got != test.want {
				t.Fatalf("status = %d, want %d", got, test.want)
			}
		})
	}
}
