package main

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

const probeTimeout = time.Second

// readinessHandler godoc
// @Summary Readiness probe
// @Description Pings the database and the identity gRPC health service within one second. `status` and each `components` entry are `healthy` or `unavailable`; any unavailable component answers 503.
// @Tags Health
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 503 {object} map[string]interface{}
// @Router /ready [get]
func readinessHandler(db *sql.DB, identityAddress string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), probeTimeout)
		defer cancel()
		components := gin.H{"database": "healthy", "identity_grpc": "healthy"}
		status, code := "healthy", http.StatusOK
		if db == nil || db.PingContext(ctx) != nil {
			components["database"] = "unavailable"
			status, code = "unavailable", http.StatusServiceUnavailable
		}
		// Health/Check is a read-only RPC; dialing and the RPC share the same deadline.
		conn, err := grpc.NewClient(identityAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			var result *healthpb.HealthCheckResponse
			result, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
			if err == nil && result.GetStatus() != healthpb.HealthCheckResponse_SERVING {
				err = context.Canceled
			}
			conn.Close()
		}
		if err != nil {
			components["identity_grpc"] = "unavailable"
			status, code = "unavailable", http.StatusServiceUnavailable
		}
		c.JSON(code, gin.H{"status": status, "service": "academic-service", "components": components})
	}
}
