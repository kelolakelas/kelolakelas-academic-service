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
	allowed  bool
	err      error
	tenantID string
	roleID   string
	name     string
}

func (s *permissionClientStub) CheckPermission(_ context.Context, tenantID, roleID, permission string) (bool, error) {
	s.tenantID, s.roleID, s.name = tenantID, roleID, permission
	return s.allowed, s.err
}

func (*permissionClientStub) Close() error { return nil }

func TestRequirePermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	roleID := uuid.New()
	tenantID := uuid.New()
	cases := []struct {
		name       string
		tenantID   string
		roleID     string
		allowed    bool
		clientErr  error
		wantStatus int
		wantCalled bool
	}{
		{name: "missing role denies before identity lookup", tenantID: tenantID.String(), wantStatus: http.StatusForbidden},
		{name: "missing tenant denies before identity lookup", roleID: roleID.String(), wantStatus: http.StatusForbidden},
		{name: "denied", tenantID: tenantID.String(), roleID: roleID.String(), wantStatus: http.StatusForbidden},
		{name: "allowed", tenantID: tenantID.String(), roleID: roleID.String(), allowed: true, wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "identity unavailable", tenantID: tenantID.String(), roleID: roleID.String(), clientErr: errors.New("identity down"), wantStatus: http.StatusServiceUnavailable},
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
				if tc.tenantID != "" {
					c.Set("tenant_id", tc.tenantID)
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
			if tc.wantCalled && (client.tenantID != tc.tenantID || client.roleID != tc.roleID || client.name != "class:update") {
				t.Fatalf("identity lookup=(%s,%s,%s), want (%s,%s,class:update)", client.tenantID, client.roleID, client.name, tc.tenantID, tc.roleID)
			}
		})
	}
}
