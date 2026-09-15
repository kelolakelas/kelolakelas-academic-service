package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

type EnrollmentHandler struct {
	enrollmentUsecase usecase.EnrollmentUsecase
}

// AssignSchedule godoc
// @Summary Assign an enrollment to a schedule
// @Description Assigns a parent-owned pending enrollment to a tenant-owned schedule transactionally.
// @Tags Enrollments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Enrollment ID (UUID)"
// @Param request body domain.AssignEnrollmentScheduleRequest true "Schedule assignment"
// @Success 200 {object} domain.HTTPResponse{data=domain.EnrollmentResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Router /api/v1/enrollments/{id}/schedule [patch]
func (h *EnrollmentHandler) AssignSchedule(c *gin.Context) {
	parentID, err := uuid.Parse(c.GetString("user_id"))
	if err != nil || !c.GetBool("is_parent") {
		c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": "Parent authentication is required", "data": nil})
		return
	}
	enrollmentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid enrollment ID", "data": nil})
		return
	}
	var req domain.AssignEnrollmentScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error(), "data": nil})
		return
	}
	res, err := h.enrollmentUsecase.AssignSchedule(c.Request.Context(), parentID, enrollmentID, req.ScheduleID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, domain.ErrScheduleFull) || errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
			status = http.StatusConflict
		}
		if errors.Is(err, domain.ErrScheduleNotFound) || errors.Is(err, domain.ErrScheduleClassMismatch) {
			status = http.StatusUnprocessableEntity
		}
		c.JSON(status, gin.H{"status": "error", "message": err.Error(), "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Schedule assigned successfully", "data": res})
}

// Create godoc
// @Summary Enroll a student in a class
// @Tags Enrollments
// @Accept json
// @Produce json
// @Param tenant_id path string true "Tenant ID"
// @Param request body domain.EnrollStudentRequest true "Enrollment request"
// @Success 201 {object} domain.HTTPResponse{data=domain.EnrollmentResponse}
// @Router /api/v1/tenants/{tenant_id}/enrollments [post]
func (h *EnrollmentHandler) Create(c *gin.Context) {
	tenantID, err := uuid.Parse(c.Param("tenant_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid tenant ID format"})
		return
	}
	var req domain.EnrollStudentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	req.IdempotencyKey = c.GetHeader("Idempotency-Key")
	if c.GetBool("is_parent") {
		parentID, parseErr := uuid.Parse(c.GetString("user_id"))
		if parseErr != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Invalid parent context", "data": nil})
			return
		}
		key := c.GetHeader("Idempotency-Key")
		result, enrollErr := h.enrollmentUsecase.EnrollPublic(c.Request.Context(), parentID, req.ClassID, &domain.PublicEnrollmentRequest{StudentID: req.StudentID, BillingCycle: req.BillingCycle, ScheduleID: req.ScheduleID}, key)
		if enrollErr != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"status": "error", "message": enrollErr.Error(), "data": nil})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"status": "success", "message": "Enrollment created and invoice generated", "data": result})
		return
	}
	res, err := h.enrollmentUsecase.EnrollStudent(c.Request.Context(), tenantID, &req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, domain.ErrIdempotencyConflict) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"status": "error", "message": "Failed to create enrollment"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "success", "message": "Enrollment created and invoice generated", "data": res})
}

// CreateCatalogEnrollment godoc
// @Summary Enroll a parent-owned student in a public class
// @Description Creates a pending enrollment and generates a billing invoice. The tenant is resolved from the selected class.
// @Tags Enrollments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param class_id path string true "Class UUID"
// @Param Idempotency-Key header string true "Unique request key"
// @Param request body domain.PublicEnrollmentRequest true "Enrollment request"
// @Success 201 {object} domain.HTTPResponse{data=domain.PublicEnrollmentResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Failure 422 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/catalog/classes/{class_id}/enrollments [post]
func (h *EnrollmentHandler) CreateCatalogEnrollment(c *gin.Context) {
	parentID, err := uuid.Parse(c.GetString("user_id"))
	if err != nil || !c.GetBool("is_parent") {
		c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": "Parent authentication is required", "data": nil})
		return
	}
	classID, err := uuid.Parse(c.Param("class_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid class ID", "data": nil})
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if key == "" || len(key) > 255 {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Idempotency-Key is required", "data": nil})
		return
	}
	var req domain.PublicEnrollmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid enrollment request", "data": nil})
		return
	}
	result, err := h.enrollmentUsecase.EnrollPublic(c.Request.Context(), parentID, classID, &req, key)
	if err != nil {
		status := catalogEnrollmentErrorStatus(err)
		c.JSON(status, gin.H{"status": "error", "message": err.Error(), "data": nil})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "success", "message": "Enrollment created and invoice generated", "data": result})
}

func catalogEnrollmentErrorStatus(err error) int {
	switch {
	case errors.Is(err, domain.ErrIdempotencyConflict), errors.Is(err, domain.ErrScheduleFull):
		return http.StatusConflict
	case errors.Is(err, domain.ErrStudentOwnership), errors.Is(err, domain.ErrClassNotEnrollable), errors.Is(err, domain.ErrScheduleClassMismatch), errors.Is(err, domain.ErrScheduleRequired):
		return http.StatusUnprocessableEntity
	case errors.Is(err, domain.ErrClassNotFound), errors.Is(err, domain.ErrStudentNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrParentRequired):
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

func NewEnrollmentHandler(enrollmentUsecase usecase.EnrollmentUsecase) *EnrollmentHandler {
	return &EnrollmentHandler{
		enrollmentUsecase: enrollmentUsecase,
	}
}

// ActivateInternal godoc
// @Summary Activate enrollment after confirmed payment
// @Description Internal service-to-service endpoint for payment-confirmed enrollment activation.
// @Tags Enrollments
// @Accept json
// @Produce json
// @Param id path string true "Enrollment ID (UUID)"
// @Success 200 {object} domain.HTTPResponse{data=domain.EnrollmentResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /internal/enrollments/{id}/activate [put]
func (h *EnrollmentHandler) ActivateInternal(c *gin.Context) {
	idParam := c.Param("id")
	enrollmentID, err := uuid.Parse(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "Invalid enrollment ID format",
			"data":    nil,
		})
		return
	}

	res, err := h.enrollmentUsecase.ActivateEnrollment(c.Request.Context(), enrollmentID)
	if err != nil {
		if errors.Is(err, usecase.ErrEnrollmentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": err.Error(),
				"data":    nil,
			})
			return
		}
		status := http.StatusInternalServerError
		if errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{
			"status":  "error",
			"message": "Failed to update enrollment status: " + err.Error(),
			"data":    nil,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Enrollment activated successfully",
		"data":    res,
	})
}
