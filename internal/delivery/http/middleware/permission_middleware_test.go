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

// KEL-21: the student and enrollment routes serve both tenant members and
// parents. A parent token carries ownership instead of a role, so it must pass
// through untouched, while every other caller is subject to exactly the same
// check RequirePermission performs.
func TestRequirePermissionUnlessParent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	roleID := uuid.New()
	tenantID := uuid.New()
	cases := []struct {
		name       string
		tenantID   string
		roleID     string
		isParent   bool
		allowed    bool
		clientErr  error
		wantStatus int
		wantCalled bool
	}{
		{name: "parent passes without a role or tenant claim", isParent: true, wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "parent passes while identity is unavailable", isParent: true, clientErr: errors.New("identity down"), wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "non-parent without role is denied", tenantID: tenantID.String(), wantStatus: http.StatusForbidden},
		{name: "non-parent without tenant is denied", roleID: roleID.String(), wantStatus: http.StatusForbidden},
		{name: "non-parent denied by permission", tenantID: tenantID.String(), roleID: roleID.String(), wantStatus: http.StatusForbidden},
		{name: "non-parent allowed", tenantID: tenantID.String(), roleID: roleID.String(), allowed: true, wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "non-parent with identity unavailable", tenantID: tenantID.String(), roleID: roleID.String(), clientErr: errors.New("identity down"), wantStatus: http.StatusServiceUnavailable},
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
				c.Set("is_parent", tc.isParent)
				c.Next()
			})
			router.GET("/students", RequirePermissionUnlessParent(client, "student:read"), func(c *gin.Context) {
				called = true
				c.Status(http.StatusNoContent)
			})

			request := httptest.NewRequest(http.MethodGet, "/students", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != tc.wantStatus || called != tc.wantCalled {
				t.Fatalf("status=%d called=%t, want status=%d called=%t", response.Code, called, tc.wantStatus, tc.wantCalled)
			}
			// A parent must never reach identity: the ownership rules inside the
			// handler are the only authority for that caller.
			if tc.isParent && client.name != "" {
				t.Fatalf("parent triggered an identity lookup for %q", client.name)
			}
			if tc.wantCalled && !tc.isParent && (client.tenantID != tc.tenantID || client.roleID != tc.roleID || client.name != "student:read") {
				t.Fatalf("identity lookup=(%s,%s,%s), want (%s,%s,student:read)", client.tenantID, client.roleID, client.name, tc.tenantID, tc.roleID)
			}
		})
	}
}
