package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

type ScheduleHandler struct {
	scheduleUsecase usecase.ScheduleUsecase
}

func NewScheduleHandler(scheduleUsecase usecase.ScheduleUsecase) *ScheduleHandler {
	return &ScheduleHandler{
		scheduleUsecase: scheduleUsecase,
	}
}

// Delete godoc
// @Summary Delete class schedule
// @Description Soft-delete a tenant-owned schedule and cancel its future scheduled sessions
// @Tags Schedules
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Schedule ID (UUID)"
// @Success 200 {object} domain.HTTPResponse
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/schedules/{id} [delete]
func (h *ScheduleHandler) Delete(c *gin.Context) {
	tenantID, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid schedule ID format", "data": nil})
		return
	}
	if err := h.scheduleUsecase.DeleteSchedule(c.Request.Context(), tenantID, id); err != nil {
		switch {
		case errors.Is(err, domain.ErrScheduleForbidden):
			c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": err.Error(), "data": nil})
		case errors.Is(err, usecase.ErrScheduleNotFound):
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": err.Error(), "data": nil})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to delete schedule", "data": nil})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Schedule deleted successfully", "data": nil})
}

// 1. Create Initial Schedules for an Existing Class
// CreateInitialSchedules godoc
// @Summary Create initial schedules for a class
// @Description Create one or more schedules for an existing tenant-owned class and generate their corresponding sessions
// @Tags Schedules
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body domain.CreateInitialSchedulesRequest true "Create initial schedules request"
// @Success 201 {object} domain.HTTPResponse{data=domain.CreateInitialSchedulesResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/schedules [post]
func (h *ScheduleHandler) CreateInitialSchedules(c *gin.Context) {
	tenantID, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
		return
	}
	var req domain.CreateInitialSchedulesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": err.Error(),
			"data":    nil,
		})
		return
	}

	res, err := h.scheduleUsecase.CreateInitialSchedules(c.Request.Context(), tenantID, &req)
	if err != nil {
		if errors.Is(err, usecase.ErrClassNotFound) || errors.Is(err, usecase.ErrEnrollmentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": err.Error(),
				"data":    nil,
			})
			return
		}
		if errors.Is(err, domain.ErrClassForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": err.Error(), "data": nil})
			return
		}
		if errors.Is(err, usecase.ErrEnrollmentRequired) || errors.Is(err, usecase.ErrInvalidEnrollmentClass) || errors.Is(err, usecase.ErrInvalidEnrollmentTenant) {
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  "error",
				"message": err.Error(),
				"data":    nil,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to create initial schedules: " + err.Error(),
			"data":    nil,
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":  "success",
		"message": "Schedules and sessions created successfully",
		"data":    res,
	})
}

// 2. Temporary Schedule Change (One-off Reschedule / Make-up Class)
// RescheduleSession godoc
// @Summary Reschedule a specific session
// @Description One-off reschedule or make-up class for an existing session
// @Tags Sessions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session ID (UUID)"
// @Param request body domain.RescheduleSessionRequest true "Reschedule session payload"
// @Success 200 {object} domain.HTTPResponse{data=domain.RescheduleSessionResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/sessions/{id}/reschedule [post]
func (h *ScheduleHandler) RescheduleSession(c *gin.Context) {
	tenantID, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
		return
	}
	var req domain.RescheduleSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": err.Error(),
			"data":    nil,
		})
		return
	}

	if idStr := c.Param("id"); idStr != "" && req.SessionID == uuid.Nil {
		if parsedID, err := uuid.Parse(idStr); err == nil {
			req.SessionID = parsedID
		}
	}

	res, err := h.scheduleUsecase.RescheduleSession(c.Request.Context(), tenantID, &req)
	if err != nil {
		if errors.Is(err, usecase.ErrSessionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": err.Error(),
				"data":    nil,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to reschedule session: " + err.Error(),
			"data":    nil,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Session rescheduled successfully",
		"data":    res,
	})
}

// 3. Permanent Schedule Change
// ChangeSchedulePermanent godoc
// @Summary Permanently change schedule
// @Description Apply permanent day/time schedule change and regenerate upcoming sessions
// @Tags Schedules
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Schedule ID (UUID)"
// @Param request body domain.PermanentScheduleChangeRequest true "Permanent schedule change payload"
// @Success 200 {object} domain.HTTPResponse{data=domain.PermanentScheduleChangeResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/schedules/{id}/permanent [put]
func (h *ScheduleHandler) ChangeSchedulePermanent(c *gin.Context) {
	tenantID, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
		return
	}
	var req domain.PermanentScheduleChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": err.Error(),
			"data":    nil,
		})
		return
	}

	if idStr := c.Param("id"); idStr != "" && req.OldScheduleID == uuid.Nil {
		if parsedID, err := uuid.Parse(idStr); err == nil {
			req.OldScheduleID = parsedID
		}
	}

	res, err := h.scheduleUsecase.ChangeSchedulePermanent(c.Request.Context(), tenantID, &req)
	if err != nil {
		if errors.Is(err, usecase.ErrScheduleNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": err.Error(),
				"data":    nil,
			})
			return
		}
		if errors.Is(err, usecase.ErrInvalidEffectiveDate) {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error(), "data": nil})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to permanently change schedule: " + err.Error(),
			"data":    nil,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Permanent schedule change applied successfully",
		"data":    res,
	})
}

// 4. Temporary Tutor Change (Substitute Teacher)
// ChangeTutorTemporary godoc
// @Summary Assign substitute tutor
// @Description Assign a temporary substitute tutor for a single session
// @Tags Sessions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session ID (UUID)"
// @Param request body domain.SubstituteTutorRequest true "Substitute tutor payload"
// @Success 200 {object} domain.HTTPResponse{data=domain.SubstituteTutorResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/sessions/{id}/substitute-tutor [patch]
func (h *ScheduleHandler) ChangeTutorTemporary(c *gin.Context) {
	tenantID, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
		return
	}
	var req domain.SubstituteTutorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": err.Error(),
			"data":    nil,
		})
		return
	}

	if idStr := c.Param("id"); idStr != "" && req.SessionID == uuid.Nil {
		if parsedID, err := uuid.Parse(idStr); err == nil {
			req.SessionID = parsedID
		}
	}

	res, err := h.scheduleUsecase.ChangeTutorTemporary(c.Request.Context(), tenantID, &req)
	if err != nil {
		if errors.Is(err, usecase.ErrSessionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": err.Error(),
				"data":    nil,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to update substitute tutor: " + err.Error(),
			"data":    nil,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Substitute tutor assigned successfully",
		"data":    res,
	})
}

// 5. Permanent Tutor Change
// ChangeTutorPermanent godoc
// @Summary Permanently change tutor
// @Description Permanently reassign tutor for a schedule
// @Tags Schedules
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Schedule ID (UUID)"
// @Param request body domain.PermanentTutorChangeRequest true "Permanent tutor change payload"
// @Success 200 {object} domain.HTTPResponse{data=domain.PermanentTutorChangeResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/schedules/{id}/tutor-permanent [patch]
func (h *ScheduleHandler) ChangeTutorPermanent(c *gin.Context) {
	tenantID, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
		return
	}
	var req domain.PermanentTutorChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": err.Error(),
			"data":    nil,
		})
		return
	}

	if idStr := c.Param("id"); idStr != "" && req.ScheduleID == uuid.Nil {
		if parsedID, err := uuid.Parse(idStr); err == nil {
			req.ScheduleID = parsedID
		}
	}

	res, err := h.scheduleUsecase.ChangeTutorPermanent(c.Request.Context(), tenantID, &req)
	if err != nil {
		if errors.Is(err, usecase.ErrScheduleNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": err.Error(),
				"data":    nil,
			})
			return
		}
		if errors.Is(err, usecase.ErrInvalidEffectiveDate) {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error(), "data": nil})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to permanently change tutor: " + err.Error(),
			"data":    nil,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Permanent tutor change applied successfully",
		"data":    res,
	})
}

// 6. Get Session Attendees
// GetSessionAttendees godoc
// @Summary Get session attendees
// @Description Retrieve attendee list (enrolled students) for a specific session
// @Tags Sessions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session ID (UUID)"
// @Success 200 {object} domain.HTTPResponse{data=domain.SessionAttendeesResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/sessions/{id}/attendees [get]
func (h *ScheduleHandler) GetSessionAttendees(c *gin.Context) {
	tenantID, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
		return
	}
	sessionIDStr := c.Param("id")
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "Invalid session ID format",
			"data":    nil,
		})
		return
	}

	attendees, err := h.scheduleUsecase.GetSessionAttendees(c.Request.Context(), tenantID, sessionID)
	if err != nil {
		if errors.Is(err, usecase.ErrSessionNotFound) || errors.Is(err, usecase.ErrEnrollmentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": err.Error(),
				"data":    nil,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to get session attendees: " + err.Error(),
			"data":    nil,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Session attendees retrieved successfully",
		"data":    attendees,
	})
}
