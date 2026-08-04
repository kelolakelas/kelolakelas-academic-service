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

type SessionHandler struct{ usecase usecase.ScheduleUsecase }

func NewSessionHandler(sessionUsecase usecase.ScheduleUsecase) *SessionHandler {
	return &SessionHandler{usecase: sessionUsecase}
}

func parseSessionQuery(c *gin.Context) (domain.SessionQuery, error) {
	q := domain.SessionQuery{Page: 1, PageSize: 20, Search: c.Query("search")}
	for key, target := range map[string]*int{"page": &q.Page, "page_size": &q.PageSize} {
		if value := c.Query(key); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 1 || (key == "page_size" && parsed > 100) {
				return q, errors.New("invalid pagination")
			}
			*target = parsed
		}
	}
	for key, target := range map[string]**uuid.UUID{"class_id": &q.ClassID, "schedule_id": &q.ScheduleID, "enrollment_id": &q.EnrollmentID, "tutor_id": &q.TutorID} {
		if value := c.Query(key); value != "" {
			parsed, err := uuid.Parse(value)
			if err != nil {
				return q, errors.New("invalid " + key)
			}
			*target = &parsed
		}
	}
	if status := c.Query("status"); status != "" {
		switch status {
		case "scheduled", "rescheduled", "cancelled", "completed":
			q.Status = status
		default:
			return q, errors.New("invalid status")
		}
	}
	var err error
	if value := c.Query("date_from"); value != "" {
		parsed, parseErr := time.Parse("2006-01-02", value)
		err = parseErr
		q.DateFrom = &parsed
	}
	if err != nil {
		return q, errors.New("invalid date_from")
	}
	if value := c.Query("date_to"); value != "" {
		parsed, parseErr := time.Parse("2006-01-02", value)
		err = parseErr
		q.DateTo = &parsed
	}
	if err != nil {
		return q, errors.New("invalid date_to")
	}
	if q.DateFrom != nil && q.DateTo != nil && q.DateFrom.After(*q.DateTo) {
		return q, errors.New("date_from must not be after date_to")
	}
	return q, nil
}

func sessionTenantID(c *gin.Context) (uuid.UUID, error) { return uuid.Parse(c.GetString("tenant_id")) }

// ListSessions godoc
// @Summary List tenant sessions
// @Description List class sessions scoped to the active tenant
// @Tags Sessions
// @Produce json
// @Security BearerAuth
// @Success 200 {object} domain.HTTPResponse{data=domain.SessionListResponse}
// @Failure 400,401,500 {object} domain.ErrorResponse
// @Router /api/v1/sessions [get]
func (h *SessionHandler) ListSessions(c *gin.Context) {
	query, err := parseSessionQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error(), "data": nil})
		return
	}
	tenantID, err := sessionTenantID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Invalid tenant context", "data": nil})
		return
	}
	result, err := h.usecase.ListSessions(c.Request.Context(), tenantID, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to fetch sessions", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Sessions fetched successfully", "data": result})
}

// GetSession godoc
// @Summary Get tenant session
// @Description Get one session scoped to the active tenant
// @Tags Sessions
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session UUID"
// @Success 200 {object} domain.HTTPResponse{data=domain.ClassSession}
// @Failure 400,401,404,500 {object} domain.ErrorResponse
// @Router /api/v1/sessions/{id} [get]
func (h *SessionHandler) GetSession(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid session ID", "data": nil})
		return
	}
	tenantID, err := sessionTenantID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Invalid tenant context", "data": nil})
		return
	}
	session, err := h.usecase.GetSession(c.Request.Context(), tenantID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "Session not found", "data": nil})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to fetch session", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Session fetched successfully", "data": session})
}

// DeleteSession godoc
// @Summary Delete class session
// @Description Soft-delete a tenant-owned class session by ID
// @Tags Sessions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param X-Tenant-ID header string true "Tenant ID dalam format UUID"
// @Param id path string true "Session ID (UUID)"
// @Success 200 {object} domain.HTTPResponse
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/sessions/{id} [delete]
func (h *SessionHandler) DeleteSession(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid session ID", "data": nil})
		return
	}
	tenantID, err := sessionTenantID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Invalid tenant context", "data": nil})
		return
	}
	if err := h.usecase.DeleteSession(c.Request.Context(), tenantID, id); err != nil {
		switch {
		case errors.Is(err, domain.ErrSessionForbidden):
			c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": err.Error(), "data": nil})
		case errors.Is(err, usecase.ErrSessionNotFound):
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": err.Error(), "data": nil})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to delete session", "data": nil})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Session deleted successfully", "data": nil})
}
