package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

type PrivateScheduleRequestHandler struct {
	usecase usecase.PrivateScheduleRequestUsecase
}

func NewPrivateScheduleRequestHandler(u usecase.PrivateScheduleRequestUsecase) *PrivateScheduleRequestHandler {
	return &PrivateScheduleRequestHandler{usecase: u}
}

func privateRequestError(c *gin.Context, err error) {
	status, message := http.StatusInternalServerError, "Failed to process schedule request"
	switch {
	case errors.Is(err, domain.ErrPrivateRequestNotFound), errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, domain.ErrClassNotFound):
		status, message = http.StatusNotFound, "Schedule request or class not found"
	case errors.Is(err, domain.ErrStudentOwnership):
		status, message = http.StatusNotFound, "Student not found for parent"
	case errors.Is(err, domain.ErrPrivateRequestConflict), errors.Is(err, domain.ErrDuplicateEnrollment), errors.Is(err, domain.ErrPrivateRequestTransition):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, domain.ErrPrivateClassRequired), errors.Is(err, domain.ErrClassNotEnrollable), errors.Is(err, domain.ErrPrivateRequestSlots):
		status, message = http.StatusUnprocessableEntity, err.Error()
	case errors.Is(err, domain.ErrParentRequired):
		status, message = http.StatusForbidden, err.Error()
	default:
		if err != nil && (err.Error() == "invalid request status" || err.Error() == "reason is too long" || err.Error() == "note is too long" || err.Error() == "invalid billing cycle") {
			status, message = http.StatusBadRequest, err.Error()
		} else {
			logInternalError(c.Request.Context(), "private schedule request", err)
		}
	}
	c.JSON(status, gin.H{"status": "error", "message": message, "data": nil})
}

func privateRequestParent(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.GetString("user_id"))
	if !c.GetBool("is_parent") || err != nil || id == uuid.Nil {
		c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": "Parent authentication is required"})
		return uuid.Nil, false
	}
	return id, true
}
func privateRequestID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid schedule request ID"})
		return uuid.Nil, false
	}
	return id, true
}
func privateRequestScope(c *gin.Context) (*uuid.UUID, *uuid.UUID, bool) {
	if c.GetBool("is_parent") {
		id, ok := privateRequestParent(c)
		if !ok {
			return nil, nil, false
		}
		return nil, &id, true
	}
	tenantID, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
		return nil, nil, false
	}
	return &tenantID, nil, true
}

// Create godoc
// @Summary Request a weekly schedule for a private class
// @Description Parent-owned student only. One pending request per student/class; an existing pending or active enrollment also conflicts.
// @Tags Private schedule requests
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param class_id path string true "Class UUID"
// @Param request body domain.CreatePrivateScheduleRequest true "Weekly slots"
// @Success 201 {object} domain.HTTPResponse{data=domain.PrivateScheduleRequest}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Failure 422 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/catalog/classes/{class_id}/schedule-requests [post]
func (h *PrivateScheduleRequestHandler) Create(c *gin.Context) {
	parentID, ok := privateRequestParent(c)
	if !ok {
		return
	}
	classID, err := uuid.Parse(c.Param("class_id"))
	if err != nil || classID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid class ID"})
		return
	}
	var req domain.CreatePrivateScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid schedule request: " + err.Error()})
		return
	}
	req.ParentEmail = c.GetString("email")
	result, err := h.usecase.Create(c.Request.Context(), parentID, classID, &req)
	if err != nil {
		privateRequestError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "success", "data": result})
}

// List godoc
// @Summary List own or tenant private schedule requests
// @Tags Private schedule requests
// @Produce json
// @Security BearerAuth
// @x-permission {"permission":"enrollment:read","parent_tokens":"skipped"}
// @Param status query string false "pending, approved, rejected, declined or cancelled"
// @Success 200 {object} domain.HTTPResponse
// @Failure 400 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/schedule-requests [get]
func (h *PrivateScheduleRequestHandler) List(c *gin.Context) {
	tenant, parent, ok := privateRequestScope(c)
	if !ok {
		return
	}
	result, err := h.usecase.List(c.Request.Context(), tenant, parent, c.Query("status"))
	if err != nil {
		privateRequestError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": result})
}

// Get godoc
// @Summary Get own or tenant private schedule request
// @Tags Private schedule requests
// @Produce json
// @Security BearerAuth
// @x-permission {"permission":"enrollment:read","parent_tokens":"skipped"}
// @Param id path string true "Request UUID"
// @Success 200 {object} domain.HTTPResponse{data=domain.PrivateScheduleRequest}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/schedule-requests/{id} [get]
func (h *PrivateScheduleRequestHandler) Get(c *gin.Context) {
	tenant, parent, ok := privateRequestScope(c)
	if !ok {
		return
	}
	id, ok := privateRequestID(c)
	if !ok {
		return
	}
	result, err := h.usecase.Get(c.Request.Context(), id, tenant, parent)
	if err != nil {
		privateRequestError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": result})
}

// Approve godoc
// @Summary Approve a pending private schedule request in own tenant
// @Tags Private schedule requests
// @Produce json
// @Security BearerAuth
// @x-permission {"permission":"enrollment:update","parent_tokens":"denied"}
// @Param id path string true "Request UUID"
// @Success 200 {object} domain.HTTPResponse{data=domain.PublicEnrollmentResponse}
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Failure 422 {object} domain.ErrorResponse
// @Router /api/v1/schedule-requests/{id}/approve [post]
func (h *PrivateScheduleRequestHandler) Approve(c *gin.Context) {
	tenant, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
		return
	}
	id, ok := privateRequestID(c)
	if !ok {
		return
	}
	result, err := h.usecase.Approve(c.Request.Context(), tenant, id)
	if err != nil {
		if errors.Is(err, domain.ErrPlatformFeeExceedsGross) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"status": "error", "message": err.Error(), "code": domain.PlatformFeeExceedsGrossErrorCode, "data": nil})
			return
		}
		privateRequestError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": result})
}

// Reject godoc
// @Summary Reject a pending private schedule request in own tenant
// @Tags Private schedule requests
// @Accept json
// @Produce json
// @Security BearerAuth
// @x-permission {"permission":"enrollment:update","parent_tokens":"denied"}
// @Param id path string true "Request UUID"
// @Param request body domain.RejectPrivateScheduleRequest false "Optional reason and recommended slots"
// @Success 200 {object} domain.HTTPResponse{data=domain.PrivateScheduleRequest}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/schedule-requests/{id}/reject [post]
func (h *PrivateScheduleRequestHandler) Reject(c *gin.Context) {
	tenant, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
		return
	}
	id, ok := privateRequestID(c)
	if !ok {
		return
	}
	var req domain.RejectPrivateScheduleRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid rejection request"})
			return
		}
	}
	if req.Reason != nil {
		value := strings.TrimSpace(*req.Reason)
		req.Reason = &value
	}
	result, err := h.usecase.Reject(c.Request.Context(), tenant, id, &req)
	if err != nil {
		privateRequestError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": result})
}

// AcceptRecommendation godoc
// @Summary Accept a parent-owned private schedule recommendation
// @Tags Private schedule requests
// @Produce json
// @Security BearerAuth
// @Param id path string true "Request UUID"
// @Success 200 {object} domain.HTTPResponse{data=domain.PublicEnrollmentResponse}
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Failure 422 {object} domain.ErrorResponse
// @Router /api/v1/schedule-requests/{id}/recommendation/accept [post]
func (h *PrivateScheduleRequestHandler) AcceptRecommendation(c *gin.Context) {
	parent, ok := privateRequestParent(c)
	if !ok {
		return
	}
	id, ok := privateRequestID(c)
	if !ok {
		return
	}
	result, err := h.usecase.AcceptRecommendation(c.Request.Context(), parent, id)
	if err != nil {
		if errors.Is(err, domain.ErrPlatformFeeExceedsGross) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"status": "error", "message": err.Error(), "code": domain.PlatformFeeExceedsGrossErrorCode, "data": nil})
			return
		}
		privateRequestError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": result})
}

// DeclineRecommendation godoc
// @Summary Decline a parent-owned private schedule recommendation
// @Tags Private schedule requests
// @Produce json
// @Security BearerAuth
// @Param id path string true "Request UUID"
// @Success 200 {object} domain.HTTPResponse{data=domain.PrivateScheduleRequest}
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Router /api/v1/schedule-requests/{id}/recommendation/decline [post]
func (h *PrivateScheduleRequestHandler) DeclineRecommendation(c *gin.Context) {
	parent, ok := privateRequestParent(c)
	if !ok {
		return
	}
	id, ok := privateRequestID(c)
	if !ok {
		return
	}
	result, err := h.usecase.DeclineRecommendation(c.Request.Context(), parent, id)
	if err != nil {
		privateRequestError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": result})
}

// Cancel godoc
// @Summary Cancel a parent-owned pending private schedule request
// @Tags Private schedule requests
// @Produce json
// @Security BearerAuth
// @Param id path string true "Request UUID"
// @Success 200 {object} domain.HTTPResponse{data=domain.PrivateScheduleRequest}
// @Failure 404 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/schedule-requests/{id}/cancel [post]
func (h *PrivateScheduleRequestHandler) Cancel(c *gin.Context) {
	parent, ok := privateRequestParent(c)
	if !ok {
		return
	}
	id, ok := privateRequestID(c)
	if !ok {
		return
	}
	result, err := h.usecase.Cancel(c.Request.Context(), parent, id)
	if err != nil {
		privateRequestError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": result})
}
