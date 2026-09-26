package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

// RequirePermission applies the persisted permission check described in ADR 0002
// to the route it guards: the role, the tenant, and the membership are all read
// from the verified JWT claim, so a caller without a role, tenant, or member_id
// claim is denied before identity is consulted.
func RequirePermission(client grpcclient.PermissionClient, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !permissionAllowed(c, client, permission) {
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequirePermissionUnlessParent guards the routes that serve both tenant members
// and parents. A parent token carries ownership rather than a role, so the
// permission question cannot be asked for it: the owned-resource rules inside the
// handler stay authoritative for those callers. Every other caller must satisfy
// the same permission check RequirePermission performs, which keeps the denial and
// identity-failure semantics identical to the catalogue mutations.
func RequirePermissionUnlessParent(client grpcclient.PermissionClient, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetBool("is_parent") {
			c.Next()
			return
		}
		if !permissionAllowed(c, client, permission) {
			c.Abort()
			return
		}
		c.Next()
	}
}

// permissionAllowed writes the failure response and reports false when the caller
// must not proceed. A denial is a 403 without any data change, and an unreachable
// or unusable authorization service is a 503, matching the catalogue mutations.
func permissionAllowed(c *gin.Context, client grpcclient.PermissionClient, permission string) bool {
	if client == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "Authorization service unavailable", "data": nil})
		return false
	}
	roleID, err := uuid.Parse(c.GetString("role_id"))
	if err != nil || roleID == uuid.Nil {
		c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": "Permission denied", "data": nil})
		return false
	}
	// The authorization question is scoped to the tenant resolved from the verified JWT
	// claim, so a role that belongs to another tenant cannot authorize a mutation here. A
	// caller without a tenant claim is rejected before identity is consulted.
	tenantID, err := uuid.Parse(c.GetString("tenant_id"))
	if err != nil || tenantID == uuid.Nil {
		c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": "Permission denied", "data": nil})
		return false
	}
	// KEL-80: the check is pinned to the membership the verified token was issued for, so
	// identity can deny a member who was removed or moved to another role while the token
	// is still valid. A tenant token without a usable member_id claim cannot be pinned and
	// is rejected here, before identity is consulted.
	memberID, err := uuid.Parse(c.GetString("member_id"))
	if err != nil || memberID == uuid.Nil {
		c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": "Permission denied", "data": nil})
		return false
	}

	allowed, err := client.CheckPermission(c.Request.Context(), tenantID.String(), roleID.String(), memberID.String(), permission)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "Authorization service unavailable", "data": nil})
		return false
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": "Permission denied", "data": nil})
		return false
	}
	return true
}
