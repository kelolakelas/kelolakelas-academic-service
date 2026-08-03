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
	classUsecase    usecase.ClassUsecase
	creationUsecase usecase.ClassCreationUsecase
}

func NewClassHandler(classUsecase usecase.ClassUsecase, creationUsecase usecase.ClassCreationUsecase) *ClassHandler {
	return &ClassHandler{
		classUsecase:    classUsecase,
		creationUsecase: creationUsecase,
	}
}

// CreateWithCategory godoc
// @Summary Create category, class, and initial schedules
// @Description Atomically creates a category and class, plus initial schedules and sessions for group classes. Private classes must omit schedules.
// @Tags Classes
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param X-Tenant-ID header string true "Tenant ID dalam format UUID"
// @Param request body domain.CreateClassWithCategoryRequest true "Create category and class payload"
// @Success 201 {object} domain.HTTPResponse{data=domain.CreateClassWithCategoryResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/classes/with-category [post]
func (h *ClassHandler) CreateWithCategory(c *gin.Context) {
	tenantIDStr := c.GetString("tenant_id")
	if tenantIDStr == "" {
		tenantIDStr = c.GetHeader("X-Tenant-ID")
	}
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid Tenant ID format", "data": nil})
		return
	}
	var req domain.CreateClassWithCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error(), "data": nil})
		return
	}
	res, err := h.creationUsecase.CreateClassWithCategory(c.Request.Context(), tenantID, &req)
	if err != nil {
		if errors.Is(err, usecase.ErrTenantInactiveOrNotFound) {
			c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": err.Error(), "data": nil})
			return
		}
		if errors.Is(err, usecase.ErrPrivateSchedulesNotAllowed) || err.Error() == "group classes require at least one schedule" {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error(), "data": nil})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to create class: " + err.Error(), "data": nil})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "success", "message": "Category, class, and initial schedules created successfully", "data": res})
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

// Delete godoc
// @Summary Delete academic class
// @Description Soft-delete a tenant-owned class, cancel future sessions, and disable its active schedules
// @Tags Classes
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param X-Tenant-ID header string true "Tenant ID dalam format UUID"
// @Param id path string true "Class ID (UUID)"
// @Success 200 {object} domain.HTTPResponse
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/classes/{id} [delete]
func (h *ClassHandler) Delete(c *gin.Context) {
	tenantID, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid class ID format", "data": nil})
		return
	}
	if err := h.classUsecase.DeleteClass(c.Request.Context(), tenantID, id); err != nil {
		switch {
		case errors.Is(err, domain.ErrClassForbidden):
			c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": err.Error(), "data": nil})
		case errors.Is(err, domain.ErrClassNotFound):
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": err.Error(), "data": nil})
		case errors.Is(err, domain.ErrClassActiveEnrollments):
			c.JSON(http.StatusConflict, gin.H{"status": "error", "message": err.Error(), "data": nil})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to delete class", "data": nil})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Class deleted successfully", "data": nil})
}
