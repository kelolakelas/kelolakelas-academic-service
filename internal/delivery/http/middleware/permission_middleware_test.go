package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type permissionClientStub struct {
	allowed bool
	err     error
	roleID  string
	name    string
}

func (s *permissionClientStub) CheckPermission(_ context.Context, roleID, permission string) (bool, error) {
	s.roleID, s.name = roleID, permission
	return s.allowed, s.err
}

func (*permissionClientStub) Close() error { return nil }

func TestRequirePermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	roleID := uuid.New()
	cases := []struct {
		name       string
		roleID     string
		allowed    bool
		clientErr  error
		wantStatus int
		wantCalled bool
	}{
		{name: "missing role denies before identity lookup", wantStatus: http.StatusForbidden},
		{name: "denied", roleID: roleID.String(), wantStatus: http.StatusForbidden},
		{name: "allowed", roleID: roleID.String(), allowed: true, wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "identity unavailable", roleID: roleID.String(), clientErr: errors.New("identity down"), wantStatus: http.StatusServiceUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &permissionClientStub{allowed: tc.allowed, err: tc.clientErr}
			router := gin.New()
			called := false
			router.Use(func(c *gin.Context) {
				if tc.roleID != "" {
					c.Set("role_id", tc.roleID)
				}
				c.Next()
			})
			router.POST("/mutate", RequirePermission(client, "class:update"), func(c *gin.Context) {
				called = true
				c.Status(http.StatusNoContent)
			})

			request := httptest.NewRequest(http.MethodPost, "/mutate", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != tc.wantStatus || called != tc.wantCalled {
				t.Fatalf("status=%d called=%t, want status=%d called=%t", response.Code, called, tc.wantStatus, tc.wantCalled)
			}
			if tc.roleID != "" && (client.roleID != tc.roleID || client.name != "class:update") {
				t.Fatalf("identity lookup=(%s,%s)", client.roleID, client.name)
			}
		})
	}
}
