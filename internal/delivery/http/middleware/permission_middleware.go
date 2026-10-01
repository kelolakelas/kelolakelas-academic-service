package middleware

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

// RequirePermission applies the persisted permission check described in ADR 0002
// to the route it guards: the role, the tenant, and the membership are all read
// from the verified JWT claim, so a caller without a role, tenant, or member_id
// claim is denied before identity is consulted.
//
// KEL-140: a parent token carries ownership rather than a role, so it can never
// satisfy a permission check. It is denied here, before identity is consulted,
// even when the token also carries tenant membership claims (ADR 0002 counts
// such a token as a parent). Parent reads are served by the ownership rules
// inside the handlers on routes guarded by RequirePermissionUnlessParent or
// RequirePermissionForTenantResource; parent writes are denied everywhere.
func RequirePermission(client grpcclient.PermissionClient, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetBool("is_parent") {
			permissionFailure(c, http.StatusForbidden)
			c.Abort()
			return
		}
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

// RequirePermissionForTenantResource guards handlers which first parse the tenant
// context. Keep invalid tenant claims on that existing handler path (401 before any
// usecase), including parent tokens without a tenant. Parent tokens with a valid
// tenant also retain their existing handler behavior; tenant members require permission.
func RequirePermissionForTenantResource(client grpcclient.PermissionClient, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := uuid.Parse(c.GetString("tenant_id")); err != nil {
			c.Next()
			return
		}
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

// RequirePermissionForTenantResourceDenyParent guards the mutation routes that
// share the tenant-resource table with parent reads (KEL-140). Invalid tenant
// claims keep the existing handler path (401 before any usecase), exactly like
// RequirePermissionForTenantResource. A parent token carrying a valid tenant
// is denied here with 403 before identity is consulted, so parent writes
// never reach a handler; tenant members require the permission.
func RequirePermissionForTenantResourceDenyParent(client grpcclient.PermissionClient, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := uuid.Parse(c.GetString("tenant_id")); err != nil {
			c.Next()
			return
		}
		if c.GetBool("is_parent") {
			permissionFailure(c, http.StatusForbidden)
			c.Abort()
			return
		}
		if !permissionAllowed(c, client, permission) {
			c.Abort()
			return
		}
		c.Next()
	}
}

// permissionFailure writes the failure response for a denied or unavailable
// authorization check. A denial is a 403 without any data change, and an
// unreachable or unusable authorization service is a 503, matching the
// catalogue mutations.
func permissionFailure(c *gin.Context, status int) {
	message := "Permission denied"
	if status == http.StatusServiceUnavailable {
		message = "Authorization service unavailable"
	}
	slog.WarnContext(c.Request.Context(), "permission check failed", "request_id", RequestID(c.Request.Context()), "status", status)
	c.JSON(status, gin.H{"status": "error", "message": message, "data": nil})
}

func permissionAllowed(c *gin.Context, client grpcclient.PermissionClient, permission string) bool {
	if client == nil {
		permissionFailure(c, http.StatusServiceUnavailable)
		return false
	}
	roleID, err := uuid.Parse(c.GetString("role_id"))
	if err != nil || roleID == uuid.Nil {
		permissionFailure(c, http.StatusForbidden)
		return false
	}
	// The authorization question is scoped to the tenant resolved from the verified JWT
	// claim, so a role that belongs to another tenant cannot authorize a mutation here. A
	// caller without a tenant claim is rejected before identity is consulted.
	tenantID, err := uuid.Parse(c.GetString("tenant_id"))
	if err != nil || tenantID == uuid.Nil {
		permissionFailure(c, http.StatusForbidden)
		return false
	}
	// KEL-80: the check is pinned to the membership the verified token was issued for, so
	// identity can deny a member who was removed or moved to another role while the token
	// is still valid. A tenant token without a usable member_id claim cannot be pinned and
	// is rejected here, before identity is consulted.
	memberID, err := uuid.Parse(c.GetString("member_id"))
	if err != nil || memberID == uuid.Nil {
		permissionFailure(c, http.StatusForbidden)
		return false
	}

	allowed, err := client.CheckPermission(OutgoingContext(c.Request.Context()), tenantID.String(), roleID.String(), memberID.String(), permission)
	if err != nil {
		permissionFailure(c, http.StatusServiceUnavailable)
		return false
	}
	if !allowed {
		permissionFailure(c, http.StatusForbidden)
		return false
	}
	return true
}
