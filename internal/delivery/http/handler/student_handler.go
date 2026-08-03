package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

type StudentHandler struct{ usecase usecase.StudentUsecase }

func NewStudentHandler(u usecase.StudentUsecase) *StudentHandler { return &StudentHandler{usecase: u} }

func studentScope(c *gin.Context) (*uuid.UUID, *uuid.UUID, error) {
	userID, err := uuid.Parse(c.GetString("user_id"))
	if err != nil {
		return nil, nil, err
	}
	tenantValue := c.GetString("tenant_id")
	tenantID, tenantErr := uuid.Parse(tenantValue)
	if tenantErr == nil && tenantID != uuid.Nil {
		return &tenantID, nil, nil
	}
	return nil, &userID, nil
}

// @Summary List students
// @Description List students scoped to the authenticated parent or tenant enrollments
// @Tags Students
// @Produce json
// @Security BearerAuth
// @Success 200 {object} domain.HTTPResponse{data=domain.StudentListResponse}
// @Router /api/v1/students [get]
func (h *StudentHandler) List(c *gin.Context) {
	tenantID, parentID, err := studentScope(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Invalid user context", "data": nil})
		return
	}
	query := domain.StudentQuery{Page: 1, PageSize: 20, Search: c.Query("search")}
	if value := c.Query("page"); value != "" {
		query.Page, err = strconv.Atoi(value)
	}
	if err == nil {
		if value := c.Query("page_size"); value != "" {
			query.PageSize, err = strconv.Atoi(value)
		}
	}
	if err != nil || query.Page < 1 || query.PageSize < 1 || query.PageSize > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid pagination", "data": nil})
		return
	}
	result, err := h.usecase.List(c.Request.Context(), tenantID, parentID, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to fetch students", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Students fetched successfully", "data": result})
}

// @Summary Create student
// @Tags Students
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body domain.CreateStudentRequest true "Student payload"
// @Success 201 {object} domain.HTTPResponse{data=domain.Student}
// @Router /api/v1/students [post]
func (h *StudentHandler) Create(c *gin.Context) {
	tenantID, parentID, err := studentScope(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Invalid user context", "data": nil})
		return
	}
	userID := parentID
	var req domain.CreateStudentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error(), "data": nil})
		return
	}
	student, err := h.usecase.Create(c.Request.Context(), tenantID, userID, &req)
	if errors.Is(err, domain.ErrStudentForbidden) {
		c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": "Parent ownership required", "data": nil})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid student payload", "data": nil})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "success", "message": "Student created successfully", "data": student})
}

// @Summary Get student
// @Tags Students
// @Produce json
// @Security BearerAuth
// @Param id path string true "Student UUID"
// @Success 200 {object} domain.HTTPResponse{data=domain.Student}
// @Router /api/v1/students/{id} [get]
func (h *StudentHandler) Get(c *gin.Context) { h.mutate(c, false) }

// @Summary Update student
// @Tags Students
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Student UUID"
// @Param request body domain.UpdateStudentRequest true "Student payload"
// @Success 200 {object} domain.HTTPResponse{data=domain.Student}
// @Router /api/v1/students/{id} [patch]
func (h *StudentHandler) Update(c *gin.Context) { h.mutate(c, true) }
func (h *StudentHandler) mutate(c *gin.Context, update bool) {
	tenantID, parentID, err := studentScope(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Invalid user context", "data": nil})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid student ID", "data": nil})
		return
	}
	if !update {
		student, err := h.usecase.GetByID(c.Request.Context(), tenantID, parentID, id)
		if errors.Is(err, domain.ErrStudentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "Student not found", "data": nil})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to fetch student", "data": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Student fetched successfully", "data": student})
		return
	}
	var req domain.UpdateStudentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error(), "data": nil})
		return
	}
	student, err := h.usecase.Update(c.Request.Context(), tenantID, parentID, id, &req)
	if errors.Is(err, domain.ErrStudentNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "Student not found", "data": nil})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid student payload", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Student updated successfully", "data": student})
}

// @Summary Delete student
// @Tags Students
// @Produce json
// @Security BearerAuth
// @Param id path string true "Student UUID"
// @Success 200 {object} domain.HTTPResponse
// @Failure 409 {object} domain.ErrorResponse
// @Router /api/v1/students/{id} [delete]
func (h *StudentHandler) Delete(c *gin.Context) {
	tenantID, parentID, err := studentScope(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Invalid user context", "data": nil})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid student ID", "data": nil})
		return
	}
	err = h.usecase.Delete(c.Request.Context(), tenantID, parentID, id)
	if errors.Is(err, domain.ErrStudentActiveEnroll) {
		c.JSON(http.StatusConflict, gin.H{"status": "error", "message": "Student has active enrollments", "data": nil})
		return
	}
	if errors.Is(err, domain.ErrStudentNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "Student not found", "data": nil})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to delete student", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Student deleted successfully", "data": nil})
}
