package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/tutorin-id/tutorin-academic-service/internal/domain"
	"github.com/tutorin-id/tutorin-academic-service/internal/usecase"
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
