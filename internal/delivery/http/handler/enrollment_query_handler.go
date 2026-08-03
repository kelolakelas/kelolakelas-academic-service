package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

func NewEnrollmentQueryHandler(u usecase.EnrollmentUsecase) *EnrollmentHandler {
	return &EnrollmentHandler{enrollmentUsecase: u}
}

func enrollmentScope(c *gin.Context) (*uuid.UUID, *uuid.UUID, error) {
	userID, err := uuid.Parse(c.GetString("user_id"))
	if err != nil {
		return nil, nil, err
	}
	tenantID, err := uuid.Parse(c.GetString("tenant_id"))
	if err == nil && tenantID != uuid.Nil {
		return &tenantID, nil, nil
	}
	return nil, &userID, nil
}

func parseEnrollmentQuery(c *gin.Context) (domain.EnrollmentQuery, error) {
	q := domain.EnrollmentQuery{Page: 1, PageSize: 20, Search: c.Query("search")}
	for key, target := range map[string]*int{"page": &q.Page, "page_size": &q.PageSize} {
		if value := c.Query(key); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 1 || (key == "page_size" && parsed > 100) {
				return q, errors.New("invalid pagination")
			}
			*target = parsed
		}
	}
	if value := c.Query("status"); value != "" {
		switch value {
		case "pending", "active", "completed", "dropped":
			q.Status = value
		default:
			return q, domain.ErrInvalidEnrollmentStatus
		}
	}
	for key, target := range map[string]**uuid.UUID{"class_id": &q.ClassID, "student_id": &q.StudentID} {
		if value := c.Query(key); value != "" {
			parsed, err := uuid.Parse(value)
			if err != nil {
				return q, errors.New("invalid " + key)
			}
			*target = &parsed
		}
	}
	if value := c.Query("date_from"); value != "" {
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			return q, errors.New("invalid date_from")
		}
		q.DateFrom = &parsed
	}
	if value := c.Query("date_to"); value != "" {
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			return q, errors.New("invalid date_to")
		}
		q.DateTo = &parsed
	}
	if q.DateFrom != nil && q.DateTo != nil && q.DateFrom.After(*q.DateTo) {
		return q, errors.New("date_from must not be after date_to")
	}
	return q, nil
}

// @Summary List enrollments
// @Tags Enrollments
// @Produce json
// @Security BearerAuth
// @Success 200 {object} domain.HTTPResponse{data=domain.EnrollmentListResponse}
// @Router /api/v1/enrollments [get]
func (h *EnrollmentHandler) ListQuery(c *gin.Context) {
	tenantID, parentID, err := enrollmentScope(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Invalid user context", "data": nil})
		return
	}
	q, err := parseEnrollmentQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error(), "data": nil})
		return
	}
	result, err := h.enrollmentUsecase.List(c.Request.Context(), tenantID, parentID, q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to fetch enrollments", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Enrollments fetched successfully", "data": result})
}

// @Summary Get enrollment
// @Tags Enrollments
// @Produce json
// @Security BearerAuth
// @Param id path string true "Enrollment UUID"
// @Success 200 {object} domain.HTTPResponse{data=domain.EnrollmentResponse}
// @Router /api/v1/enrollments/{id} [get]
func (h *EnrollmentHandler) GetQuery(c *gin.Context) {
	tenantID, parentID, err := enrollmentScope(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Invalid user context", "data": nil})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid enrollment ID", "data": nil})
		return
	}
	result, err := h.enrollmentUsecase.GetByID(c.Request.Context(), tenantID, parentID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "Enrollment not found", "data": nil})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to fetch enrollment", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Enrollment fetched successfully", "data": result})
}
