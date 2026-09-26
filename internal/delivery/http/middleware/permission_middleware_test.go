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
	calls    int
	tenantID string
	roleID   string
	memberID string
	name     string
}

func (s *permissionClientStub) CheckPermission(_ context.Context, tenantID, roleID, memberID, permission string) (bool, error) {
	s.calls++
	s.tenantID, s.roleID, s.memberID, s.name = tenantID, roleID, memberID, permission
	return s.allowed, s.err
}

func (*permissionClientStub) Close() error { return nil }

func TestRequirePermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	roleID := uuid.New()
	tenantID := uuid.New()
	memberID := uuid.New()
	cases := []struct {
		name         string
		tenantID     string
		roleID       string
		memberID     string
		allowed      bool
		clientErr    error
		wantStatus   int
		wantCalled   bool
		wantIdentity bool
	}{
		{name: "missing role denies before identity lookup", tenantID: tenantID.String(), memberID: memberID.String(), wantStatus: http.StatusForbidden},
		{name: "missing tenant denies before identity lookup", roleID: roleID.String(), memberID: memberID.String(), wantStatus: http.StatusForbidden},
		// KEL-80: a tenant token that cannot be pinned to a membership never reaches identity.
		{name: "missing member_id denies before identity lookup", tenantID: tenantID.String(), roleID: roleID.String(), allowed: true, wantStatus: http.StatusForbidden},
		{name: "non-UUID member_id denies before identity lookup", tenantID: tenantID.String(), roleID: roleID.String(), memberID: "not-a-uuid", allowed: true, wantStatus: http.StatusForbidden},
		{name: "nil member_id denies before identity lookup", tenantID: tenantID.String(), roleID: roleID.String(), memberID: uuid.Nil.String(), allowed: true, wantStatus: http.StatusForbidden},
		{name: "denied (removed member or changed role)", tenantID: tenantID.String(), roleID: roleID.String(), memberID: memberID.String(), wantStatus: http.StatusForbidden, wantIdentity: true},
		{name: "allowed", tenantID: tenantID.String(), roleID: roleID.String(), memberID: memberID.String(), allowed: true, wantStatus: http.StatusNoContent, wantCalled: true, wantIdentity: true},
		{name: "identity unavailable", tenantID: tenantID.String(), roleID: roleID.String(), memberID: memberID.String(), clientErr: errors.New("identity down"), wantStatus: http.StatusServiceUnavailable, wantIdentity: true},
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
				if tc.memberID != "" {
					c.Set("member_id", tc.memberID)
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
			if (client.calls > 0) != tc.wantIdentity {
				t.Fatalf("identity calls=%d, want identity consulted=%t", client.calls, tc.wantIdentity)
			}
			if tc.wantIdentity && (client.tenantID != tc.tenantID || client.roleID != tc.roleID || client.memberID != tc.memberID || client.name != "class:update") {
				t.Fatalf("identity lookup=(%s,%s,%s,%s), want (%s,%s,%s,class:update)", client.tenantID, client.roleID, client.memberID, client.name, tc.tenantID, tc.roleID, tc.memberID)
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
	memberID := uuid.New()
	cases := []struct {
		name         string
		tenantID     string
		roleID       string
		memberID     string
		isParent     bool
		allowed      bool
		clientErr    error
		wantStatus   int
		wantCalled   bool
		wantIdentity bool
	}{
		{name: "parent passes without a role or tenant claim", isParent: true, wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "parent passes while identity is unavailable", isParent: true, clientErr: errors.New("identity down"), wantStatus: http.StatusNoContent, wantCalled: true},
		// ADR 0002 counts a token with is_parent as a parent even when it also carries
		// tenant membership claims; KEL-80 must not start asking identity for it.
		{name: "parent with tenant membership claims still skips identity", isParent: true, tenantID: tenantID.String(), roleID: roleID.String(), memberID: memberID.String(), wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "parent with an invalid member_id still skips identity", isParent: true, tenantID: tenantID.String(), roleID: roleID.String(), memberID: "not-a-uuid", wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "non-parent without role is denied", tenantID: tenantID.String(), memberID: memberID.String(), wantStatus: http.StatusForbidden},
		{name: "non-parent without tenant is denied", roleID: roleID.String(), memberID: memberID.String(), wantStatus: http.StatusForbidden},
		{name: "non-parent without member_id is denied", tenantID: tenantID.String(), roleID: roleID.String(), allowed: true, wantStatus: http.StatusForbidden},
		{name: "non-parent with non-UUID member_id is denied", tenantID: tenantID.String(), roleID: roleID.String(), memberID: "member-1", allowed: true, wantStatus: http.StatusForbidden},
		{name: "non-parent denied by permission", tenantID: tenantID.String(), roleID: roleID.String(), memberID: memberID.String(), wantStatus: http.StatusForbidden, wantIdentity: true},
		{name: "non-parent allowed", tenantID: tenantID.String(), roleID: roleID.String(), memberID: memberID.String(), allowed: true, wantStatus: http.StatusNoContent, wantCalled: true, wantIdentity: true},
		{name: "non-parent with identity unavailable", tenantID: tenantID.String(), roleID: roleID.String(), memberID: memberID.String(), clientErr: errors.New("identity down"), wantStatus: http.StatusServiceUnavailable, wantIdentity: true},
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
				if tc.memberID != "" {
					c.Set("member_id", tc.memberID)
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
			if (client.calls > 0) != tc.wantIdentity {
				t.Fatalf("identity calls=%d, want identity consulted=%t", client.calls, tc.wantIdentity)
			}
			if tc.wantIdentity && (client.tenantID != tc.tenantID || client.roleID != tc.roleID || client.memberID != tc.memberID || client.name != "student:read") {
				t.Fatalf("identity lookup=(%s,%s,%s,%s), want (%s,%s,%s,student:read)", client.tenantID, client.roleID, client.memberID, client.name, tc.tenantID, tc.roleID, tc.memberID)
			}
		})
	}
}

// The member_id the permission check sends must be the one from the verified token,
// end to end through AuthMiddleware, so a client cannot choose which membership is checked.
func TestRequirePermissionForwardsMemberIDFromVerifiedToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	claims := Claims{
		UserID:   uuid.NewString(),
		TenantID: uuid.NewString(),
		RoleID:   uuid.NewString(),
		MemberID: uuid.NewString(),
	}
	client := &permissionClientStub{allowed: true}
	router := gin.New()
	router.POST("/mutate", AuthMiddleware(emailClaimTestSecret), RequirePermission(client, "class:update"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodPost, "/mutate", nil)
	request.Header.Set("Authorization", "Bearer "+signEmailClaimToken(t, claims))
	request.Header.Set("X-Member-ID", uuid.NewString())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want %d (body=%s)", response.Code, http.StatusNoContent, response.Body.String())
	}
	if client.memberID != claims.MemberID || client.tenantID != claims.TenantID || client.roleID != claims.RoleID {
		t.Fatalf("identity lookup=(tenant=%s role=%s member=%s), want claims (%s,%s,%s)", client.tenantID, client.roleID, client.memberID, claims.TenantID, claims.RoleID, claims.MemberID)
	}
}
