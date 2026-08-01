package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

type ClassHandler struct {
	classUsecase usecase.ClassUsecase
}

func NewClassHandler(classUsecase usecase.ClassUsecase) *ClassHandler {
	return &ClassHandler{
		classUsecase: classUsecase,
	}
}

// Create godoc
// @Summary Create academic class
// @Description Create a new class within a category for a tenant
// @Tags Classes
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param X-Tenant-ID header string true "Tenant ID dalam format UUID"
// @Param request body domain.CreateClassRequest true "Create class payload"
// @Success 201 {object} domain.HTTPResponse{data=domain.ClassResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/classes [post]
func (h *ClassHandler) Create(c *gin.Context) {
	tenantIDStr := c.GetString("tenant_id")
	if tenantIDStr == "" {
		tenantIDStr = c.GetHeader("X-Tenant-ID")
	}

	if tenantIDStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"message": "Tenant ID is missing in context or header",
			"data":    nil,
		})
		return
	}

	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "Invalid Tenant ID format",
			"data":    nil,
		})
		return
	}

	var req domain.CreateClassRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": err.Error(),
			"data":    nil,
		})
		return
	}

	res, err := h.classUsecase.CreateClass(c.Request.Context(), tenantID, &req)
	if err != nil {
		if errors.Is(err, usecase.ErrTenantInactiveOrNotFound) {
			c.JSON(http.StatusForbidden, gin.H{
				"status":  "error",
				"message": err.Error(),
				"data":    nil,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to create class: " + err.Error(),
			"data":    nil,
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":  "success",
		"message": "Class created successfully",
		"data":    res,
	})
}
