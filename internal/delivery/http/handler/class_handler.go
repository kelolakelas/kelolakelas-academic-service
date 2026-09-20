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
// @Summary Create class using an existing category
// @Description Creates a class using an existing category owned by the requester and assigns teachers. Schedules and sessions are created separately.
// @Tags Classes
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body domain.CreateClassWithCategoryRequest true "Create class with existing category payload"
// @Success 201 {object} domain.HTTPResponse{data=domain.CreateClassWithCategoryResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/classes/with-category [post]
func (h *ClassHandler) CreateWithCategory(c *gin.Context) {
	tenantID, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
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
		if errors.Is(err, domain.ErrCategoryNotFound) || errors.Is(err, domain.ErrCategoryForbidden) {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error(), "data": nil})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to create class: " + err.Error(), "data": nil})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "success", "message": "Class created successfully", "data": res})
}

// Create godoc
// @Summary Create academic class
// @Description Create a new class within a category for a tenant
// @Tags Classes
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body domain.CreateClassRequest true "Create class payload"
// @Success 201 {object} domain.HTTPResponse{data=domain.ClassResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/classes [post]
func (h *ClassHandler) Create(c *gin.Context) {
	tenantID, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
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

// Update godoc
// @Summary Update an academic class
// @Description Updates the sellable attributes (name, description, price, category) of a tenant-owned class. Existing enrollments keep their stored gross_amount.
// @Tags Classes
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Class ID (UUID)"
// @Param request body domain.UpdateClassRequest true "Class update payload"
// @Success 200 {object} domain.HTTPResponse{data=domain.ClassResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 422 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/classes/{id} [patch]
func (h *ClassHandler) Update(c *gin.Context) {
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
	var req domain.UpdateClassRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error(), "data": nil})
		return
	}
	res, err := h.classUsecase.UpdateClass(c.Request.Context(), tenantID, id, &req)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrClassNotFound):
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": err.Error(), "data": nil})
		case errors.Is(err, domain.ErrClassForbidden):
			c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": err.Error(), "data": nil})
		case errors.Is(err, domain.ErrCategoryNotFound),
			errors.Is(err, domain.ErrCategoryForbidden),
			errors.Is(err, domain.ErrClassTypeImmutable),
			errors.Is(err, domain.ErrClassNameRequired),
			errors.Is(err, domain.ErrInvalidClassPrice):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"status": "error", "message": err.Error(), "data": nil})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to update class", "data": nil})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Class updated successfully", "data": res})
}

// UpdatePublication godoc
// @Summary Publish or unpublish an academic class
// @Description Updates only the publication status of a tenant-owned class.
// @Tags Classes
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Class ID (UUID)"
// @Param request body domain.UpdateClassPublicationRequest true "Class publication status payload"
// @Success 200 {object} domain.HTTPResponse{data=domain.ClassResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/classes/{id}/published [patch]
func (h *ClassHandler) UpdatePublication(c *gin.Context) {
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
	var req domain.UpdateClassPublicationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error(), "data": nil})
		return
	}
	res, err := h.classUsecase.UpdateClassPublication(c.Request.Context(), tenantID, id, &req)
	if err != nil {
		if errors.Is(err, domain.ErrClassNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": err.Error(), "data": nil})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to update class publication status", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Class publication status updated successfully", "data": res})
}
