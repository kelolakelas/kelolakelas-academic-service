package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

func RequirePermission(client grpcclient.PermissionClient, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if client == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "Authorization service unavailable", "data": nil})
			c.Abort()
			return
		}
		roleID, err := uuid.Parse(c.GetString("role_id"))
		if err != nil || roleID == uuid.Nil {
			c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": "Permission denied", "data": nil})
			c.Abort()
			return
		}
		// The authorization question is scoped to the tenant resolved from the verified JWT
		// claim, so a role that belongs to another tenant cannot authorize a mutation here. A
		// caller without a tenant claim is rejected before identity is consulted.
		tenantID, err := uuid.Parse(c.GetString("tenant_id"))
		if err != nil || tenantID == uuid.Nil {
			c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": "Permission denied", "data": nil})
			c.Abort()
			return
		}

		allowed, err := client.CheckPermission(c.Request.Context(), tenantID.String(), roleID.String(), permission)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "Authorization service unavailable", "data": nil})
			c.Abort()
			return
		}
		if !allowed {
			c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": "Permission denied", "data": nil})
			c.Abort()
			return
		}
		c.Next()
	}
}
