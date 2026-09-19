package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

func listQuery(c *gin.Context) (domain.ListQuery, error) {
	query := domain.ListQuery{Page: 1, PageSize: 20, Search: c.Query("search")}
	for key, target := range map[string]*int{"page": &query.Page, "page_size": &query.PageSize} {
		if value := c.Query(key); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 1 || (key == "page_size" && parsed > 100) {
				return query, gin.Error{Err: strconv.ErrSyntax}
			}
			*target = parsed
		}
	}
	return query, nil
}

func tenantID(c *gin.Context) (uuid.UUID, error) {
	return tenantIDFromContext(c)
}

type ListHandler struct {
	category usecase.CategoryUsecase
	class    usecase.ClassUsecase
	schedule usecase.ScheduleUsecase
}

func NewListHandler(category usecase.CategoryUsecase, class usecase.ClassUsecase, schedule usecase.ScheduleUsecase) *ListHandler {
	return &ListHandler{category: category, class: class, schedule: schedule}
}

// ListCategories godoc
// @Summary List tenant categories
// @Tags Categories
// @Produce json
// @Security BearerAuth
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Items per page" default(20)
// @Param search query string false "Search category name"
// @Success 200 {object} domain.HTTPResponse{data=domain.CategoryListResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/categories [get]
func (h *ListHandler) ListCategories(c *gin.Context) {
	query, err := listQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid pagination", "data": nil})
		return
	}
	tenant, err := tenantID(c)
	if err != nil {
		writeTenantError(c, err)
		return
	}
	result, err := h.category.ListCategories(c.Request.Context(), tenant, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to fetch categories", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Categories fetched successfully", "data": result})
}

// ListClasses godoc
// @Summary List tenant classes
// @Tags Classes
// @Produce json
// @Security BearerAuth
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Items per page" default(20)
// @Param search query string false "Search class name"
// @Success 200 {object} domain.HTTPResponse{data=domain.ClassListResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/classes [get]
func (h *ListHandler) ListClasses(c *gin.Context) {
	query, err := listQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid pagination", "data": nil})
		return
	}
	tenant, err := tenantID(c)
	if err != nil {
		writeTenantError(c, err)
		return
	}
	result, err := h.class.ListClasses(c.Request.Context(), tenant, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to fetch classes", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Classes fetched successfully", "data": result})
}

// ListSchedules godoc
// @Summary List tenant schedules
// @Tags Schedules
// @Produce json
// @Security BearerAuth
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Items per page" default(20)
// @Success 200 {object} domain.HTTPResponse{data=domain.ScheduleListResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/schedules [get]
func (h *ListHandler) ListSchedules(c *gin.Context) {
	query, err := listQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid pagination", "data": nil})
		return
	}
	tenant, err := tenantID(c)
	if err != nil {
		writeTenantError(c, err)
		return
	}
	result, err := h.schedule.ListSchedules(c.Request.Context(), tenant, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to fetch schedules", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Schedules fetched successfully", "data": result})
}
