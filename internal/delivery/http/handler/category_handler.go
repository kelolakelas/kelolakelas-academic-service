package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

type CategoryHandler struct {
	categoryUsecase usecase.CategoryUsecase
}

func NewCategoryHandler(categoryUsecase usecase.CategoryUsecase) *CategoryHandler {
	return &CategoryHandler{
		categoryUsecase: categoryUsecase,
	}
}

// Create godoc
// @Summary Create academic category
// @Description Create a new subject/course category for a tenant
// @Tags Categories
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param X-Tenant-ID header string true "Tenant ID dalam format UUID"
// @Param request body domain.CreateCategoryRequest true "Create category payload"
// @Success 201 {object} domain.HTTPResponse{data=domain.CategoryResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/categories [post]
func (h *CategoryHandler) Create(c *gin.Context) {
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

	var req domain.CreateCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": err.Error(),
			"data":    nil,
		})
		return
	}

	res, err := h.categoryUsecase.CreateCategory(c.Request.Context(), tenantID, &req)
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
			"message": "Failed to create category: " + err.Error(),
			"data":    nil,
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":  "success",
		"message": "Category created successfully",
		"data":    res,
	})
}

// Delete godoc
// @Summary Delete academic category
// @Description Soft-delete a tenant-owned category when it has no active classes
// @Tags Categories
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param X-Tenant-ID header string true "Tenant ID dalam format UUID"
// @Param id path string true "Category ID (UUID)"
// @Success 200 {object} domain.HTTPResponse
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/categories/{id} [delete]
func (h *CategoryHandler) Delete(c *gin.Context) {
	tenantID, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid category ID format", "data": nil})
		return
	}
	if err := h.categoryUsecase.DeleteCategory(c.Request.Context(), tenantID, id); err != nil {
		switch {
		case errors.Is(err, domain.ErrCategoryForbidden):
			c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": err.Error(), "data": nil})
		case errors.Is(err, domain.ErrCategoryNotFound):
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": err.Error(), "data": nil})
		case errors.Is(err, domain.ErrCategoryActiveClasses):
			c.JSON(http.StatusConflict, gin.H{"status": "error", "message": err.Error(), "data": nil})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to delete category", "data": nil})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Category deleted successfully", "data": nil})
}

func tenantIDFromContext(c *gin.Context) (uuid.UUID, error) {
	tenantIDStr := c.GetString("tenant_id")
	if tenantIDStr == "" {
		tenantIDStr = c.GetHeader("X-Tenant-ID")
	}
	if tenantIDStr == "" {
		return uuid.Nil, errors.New("tenant ID is missing")
	}
	return uuid.Parse(tenantIDStr)
}

func writeTenantError(c *gin.Context, err error) {
	if err.Error() == "tenant ID is missing" {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": err.Error(), "data": nil})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid Tenant ID format", "data": nil})
}
