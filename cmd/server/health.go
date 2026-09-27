package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// healthHandler godoc
// @Summary Liveness probe
// @Description Answers 200 while the process serves HTTP; it checks no dependency.
// @Tags Health
// @Produce json
// @Success 200 {object} map[string]string
// @Router /health [get]
func healthHandler(service string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": service,
		})
	}
}
