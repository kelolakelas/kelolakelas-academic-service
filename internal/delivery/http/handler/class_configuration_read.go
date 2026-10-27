package handler

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"gorm.io/gorm"
)

type ClassConfigurationReadHandler struct {
	Read *usecase.ClassConfigurationRead
}

func classReadScope(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	tenant, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		c.JSON(400, gin.H{"status": "error", "message": "Invalid class ID"})
		return tenant, id, false
	}
	return tenant, id, true
}
func classReadResult(c *gin.Context, result any, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"status": "error", "message": "Class not found"})
		return
	}
	if err != nil {
		logInternalError(c.Request.Context(), "read class configuration", err)
		c.JSON(503, gin.H{"status": "error", "message": "Saved configuration unavailable"})
		return
	}
	c.JSON(200, domain.HTTPResponse{Status: "success", Data: result})
}

// Class godoc
// @Summary Read one tenant-owned class, including unpublished configuration
// @Tags Classes
// @Produce json
// @Security BearerAuth
// @x-permission {"permission":"class:read","parent_tokens":"denied"}
// @Param id path string true "Class UUID"
// @Success 200 {object} domain.HTTPResponse{data=domain.ClassResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 503 {object} domain.ErrorResponse
// @Router /api/v1/classes/{id} [get]
func (h *ClassConfigurationReadHandler) Class(c *gin.Context) {
	tenant, id, ok := classReadScope(c)
	if !ok {
		return
	}
	result, err := h.Read.Class(c.Request.Context(), tenant, id)
	classReadResult(c, result, err)
}

// Schedules godoc
// @Summary Page saved schedules of one tenant-owned class
// @Description Requires both class:read and schedule:read. Includes nondeleted current, future and historical versions, without enrollment/parent/payment preloads.
// @Tags Schedules
// @Produce json
// @Security BearerAuth
// @x-permission {"permission":"class:read","parent_tokens":"denied"}
// @Param id path string true "Class UUID"
// @Param page query int false "Page" default(1)
// @Param page_size query int false "Items per page (max100)" default(20)
// @Success 200 {object} domain.HTTPResponse{data=domain.ScheduleListResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 503 {object} domain.ErrorResponse
// @Router /api/v1/classes/{id}/schedules [get]
func (h *ClassConfigurationReadHandler) Schedules(c *gin.Context) {
	tenant, id, ok := classReadScope(c)
	if !ok {
		return
	}
	query, err := listQuery(c)
	if err != nil {
		c.JSON(400, gin.H{"status": "error", "message": "Invalid pagination"})
		return
	}
	result, err := h.Read.Schedules(c.Request.Context(), tenant, id, query)
	classReadResult(c, result, err)
}
