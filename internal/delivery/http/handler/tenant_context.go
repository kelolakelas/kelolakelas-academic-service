package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Tenant context failures are separated so handlers can answer with a stable
// status code instead of leaking a parse error.
var (
	errTenantContextMissing = errors.New("tenant context is missing")
	errTenantContextInvalid = errors.New("tenant context is invalid")
)

// tenantIDFromContext resolves the active tenant exclusively from the verified
// JWT claim set by AuthMiddleware.
//
// Client-supplied headers are never trusted. The api-gateway forwards inbound
// headers unchanged and only injects X-Tenant-ID when the token carries a
// tenant, so a parent token (which legitimately has no tenant claim) could
// otherwise name any tenant and read unpublished catalog data or create
// enrollments on its behalf.
func tenantIDFromContext(c *gin.Context) (uuid.UUID, error) {
	raw := c.GetString("tenant_id")
	if raw == "" {
		return uuid.Nil, errTenantContextMissing
	}
	tenantID, err := uuid.Parse(raw)
	if err != nil || tenantID == uuid.Nil {
		return uuid.Nil, errTenantContextInvalid
	}
	return tenantID, nil
}

// writeTenantError maps tenant context failures onto HTTP responses.
//
// A caller without a tenant claim (for example a parent) is rejected with 403
// because the route requires a tenant scope the token does not grant; that is
// an authorization decision, not a malformed request.
func writeTenantError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errTenantContextMissing):
		c.JSON(http.StatusForbidden, gin.H{
			"status":  "error",
			"message": "Tenant context is required for this operation",
			"data":    nil,
		})
	case errors.Is(err, errTenantContextInvalid):
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"message": "Invalid tenant context",
			"data":    nil,
		})
	default:
		c.JSON(http.StatusForbidden, gin.H{
			"status":  "error",
			"message": "Tenant context is required for this operation",
			"data":    nil,
		})
	}
}
