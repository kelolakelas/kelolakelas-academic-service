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
// @x-permission {"permission":"schedule:delete","parent_tokens":"denied"}
// @Param id path string true "Schedule ID (UUID)"
// @Success 200 {object} domain.HTTPResponse
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Failure 503 {object} domain.ErrorResponse
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
			logInternalError(c.Request.Context(), "delete schedule", err)
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
// @x-permission {"permission":"schedule:create","parent_tokens":"denied"}
// @Param request body domain.CreateInitialSchedulesRequest true "Create initial schedules request"
// @Success 201 {object} domain.HTTPResponse{data=domain.CreateInitialSchedulesResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 503 {object} domain.ErrorResponse
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
		if errors.Is(err, usecase.ErrEnrollmentRequired) || errors.Is(err, usecase.ErrInvalidEnrollmentClass) || errors.Is(err, usecase.ErrInvalidEnrollmentTenant) || errors.Is(err, usecase.ErrPrivateScheduleCapacity) {
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  "error",
				"message": err.Error(),
				"data":    nil,
			})
			return
		}
		logInternalError(c.Request.Context(), "create initial schedules", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to create initial schedules",
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
// @Description One-off reschedule or make-up class for an existing session. `session_id` in the body is required and is the session that changes; the `{id}` path segment is accepted for compatibility and never overrides it. `/api/v1/sessions/reschedule` is the same operation without the path segment.
// @Tags Sessions
// @Accept json
// @Produce json
// @Security BearerAuth
// @x-permission {"permission":"schedule:update","parent_tokens":"denied"}
// @Param id path string true "Session ID (UUID)"
// @Param request body domain.RescheduleSessionRequest true "Reschedule session payload"
// @Success 200 {object} domain.HTTPResponse{data=domain.RescheduleSessionResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 503 {object} domain.ErrorResponse
// @Router /api/v1/sessions/{id}/reschedule [post]
// @Router /api/v1/sessions/reschedule [post]
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
		logInternalError(c.Request.Context(), "reschedule session", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to reschedule session",
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
// @Description Apply permanent day/time schedule change and regenerate upcoming sessions. `old_schedule_id` in the body is required and is the schedule that changes; the `{id}` path segment is accepted for compatibility and never overrides it. `/api/v1/schedules/permanent` is the same operation without the path segment.
// @Tags Schedules
// @Accept json
// @Produce json
// @Security BearerAuth
// @x-permission {"permission":"schedule:update","parent_tokens":"denied"}
// @Param id path string true "Schedule ID (UUID)"
// @Param request body domain.PermanentScheduleChangeRequest true "Permanent schedule change payload"
// @Success 200 {object} domain.HTTPResponse{data=domain.PermanentScheduleChangeResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 503 {object} domain.ErrorResponse
// @Router /api/v1/schedules/{id}/permanent [put]
// @Router /api/v1/schedules/permanent [put]
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
		logInternalError(c.Request.Context(), "permanently change schedule", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to permanently change schedule",
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
// @Description Assign a temporary substitute tutor for a single session. `session_id` in the body is required and is the session that changes; the `{id}` path segment is accepted for compatibility and never overrides it. `/api/v1/sessions/substitute-tutor` is the same operation without the path segment.
// @Tags Sessions
// @Accept json
// @Produce json
// @Security BearerAuth
// @x-permission {"permission":"schedule:update","parent_tokens":"denied"}
// @Param id path string true "Session ID (UUID)"
// @Param request body domain.SubstituteTutorRequest true "Substitute tutor payload"
// @Success 200 {object} domain.HTTPResponse{data=domain.SubstituteTutorResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 503 {object} domain.ErrorResponse
// @Router /api/v1/sessions/{id}/substitute-tutor [patch]
// @Router /api/v1/sessions/substitute-tutor [patch]
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
		// KEL-135: the substitute tutor must be an active member of the calling
		// tenant (validation, 400); an unverifiable membership answer is
		// reported as 503 so it cannot be mistaken for an eligible tutor.
		if errors.Is(err, usecase.ErrSubstituteTutorNotEligible) {
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  "error",
				"message": err.Error(),
				"data":    nil,
			})
			return
		}
		if errors.Is(err, usecase.ErrSubstituteTutorUnavailable) {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":  "error",
				"message": err.Error(),
				"data":    nil,
			})
			return
		}
		logInternalError(c.Request.Context(), "update substitute tutor", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to update substitute tutor",
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
// @Description Permanently reassign tutor for a schedule. `schedule_id` in the body is required and is the schedule that changes; the `{id}` path segment is accepted for compatibility and never overrides it. The operation answers on PATCH and PUT, with or without the path segment.
// @Tags Schedules
// @Accept json
// @Produce json
// @Security BearerAuth
// @x-permission {"permission":"schedule:update","parent_tokens":"denied"}
// @Param id path string true "Schedule ID (UUID)"
// @Param request body domain.PermanentTutorChangeRequest true "Permanent tutor change payload"
// @Success 200 {object} domain.HTTPResponse{data=domain.PermanentTutorChangeResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 503 {object} domain.ErrorResponse
// @Router /api/v1/schedules/{id}/tutor-permanent [patch]
// @Router /api/v1/schedules/tutor-permanent [patch]
// @Router /api/v1/schedules/{id}/tutor-permanent [put]
// @Router /api/v1/schedules/tutor-permanent [put]
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
		logInternalError(c.Request.Context(), "permanently change tutor", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to permanently change tutor",
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
// @x-permission {"permission":"schedule:read","parent_tokens":"skipped"}
// @Param id path string true "Session ID (UUID)"
// @Success 200 {object} domain.HTTPResponse{data=domain.SessionAttendeesResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/sessions/{id}/attendees [get]
func (h *ScheduleHandler) GetSessionAttendees(c *gin.Context) {
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
	// KEL-140: a parent reads only the parent's own children attending the
	// session, across every tenant. The tenant claim is ignored even when
	// present (AC 3); another parent's session answers 404, never 403.
	if c.GetBool("is_parent") {
		parentID, err := authenticatedUserID(c)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"status":  "error",
				"message": "Invalid user context",
				"data":    nil,
			})
			return
		}
		attendees, err := h.scheduleUsecase.GetSessionAttendeesForParent(c.Request.Context(), *parentID, sessionID)
		if err != nil {
			if errors.Is(err, usecase.ErrSessionNotFound) || errors.Is(err, usecase.ErrEnrollmentNotFound) {
				c.JSON(http.StatusNotFound, gin.H{
					"status":  "error",
					"message": err.Error(),
					"data":    nil,
				})
				return
			}
			logInternalError(c.Request.Context(), "get session attendees", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  "error",
				"message": "Failed to get session attendees",
				"data":    nil,
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":  "success",
			"message": "Session attendees retrieved successfully",
			"data":    attendees,
		})
		return
	}
	tenantID, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
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
		logInternalError(c.Request.Context(), "get session attendees", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to get session attendees",
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
