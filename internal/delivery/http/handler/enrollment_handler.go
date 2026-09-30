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
		if errors.Is(err, domain.ErrScheduleNotFound) || errors.Is(err, domain.ErrScheduleClassMismatch) || errors.Is(err, domain.ErrScheduleEnded) {
			status = http.StatusUnprocessableEntity
		}
		message := err.Error()
		if status == http.StatusInternalServerError {
			logInternalError(c.Request.Context(), "assign enrollment schedule", err)
			message = "Failed to assign schedule"
		}
		c.JSON(status, gin.H{"status": "error", "message": message, "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Schedule assigned successfully", "data": res})
}

// Create godoc
// @Summary Enroll a student in a class
// @Description A tenant-member token must carry `tenant_id` equal to the path and hold `enrollment:create`; a parent token skips the permission check and is enrolled through the public-enrollment flow (known errors answer 422). Direct private checkout answers 422 with `code` `private_schedule_request_required`; submit a schedule request instead. A 409 with `code` `duplicate_enrollment` means the student already has a pending or active enrollment in this class; a 409 without `code` is an Idempotency-Key reused with a different request. A 422 with `code` `platform_fee_exceeds_gross` means billing refused the invoice because the platform fee exceeds the payment amount: the attempt holds no seat, and a replay with the same Idempotency-Key answers the same 422.
// @Tags Enrollments
// @Accept json
// @Produce json
// @Security BearerAuth
// @x-permission {"permission":"enrollment:create","parent_tokens":"skipped"}
// @Param tenant_id path string true "Tenant ID"
// @Param Idempotency-Key header string true "Unique request key"
// @Param request body domain.EnrollStudentRequest true "Enrollment request"
// @Success 201 {object} domain.HTTPResponse{data=domain.EnrollmentResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Failure 422 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Failure 503 {object} domain.ErrorResponse
// @Router /api/v1/tenants/{tenant_id}/enrollments [post]
func (h *EnrollmentHandler) Create(c *gin.Context) {
	pathTenantID, err := uuid.Parse(c.Param("tenant_id"))
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
		// KEL-75: the email claim from the verified token is the only source for the
		// billing contact; the request body and headers are never consulted. The
		// middleware has already normalised the value (TrimSpace, case preserved).
		publicReq := &domain.PublicEnrollmentRequest{StudentID: req.StudentID, BillingCycle: req.BillingCycle, ScheduleID: req.ScheduleID, PaymentMethod: req.PaymentMethod, SenderEmail: c.GetString("email")}
		result, enrollErr := h.enrollmentUsecase.EnrollPublic(c.Request.Context(), parentID, req.ClassID, publicReq, key)
		if enrollErr != nil {
			status := http.StatusUnprocessableEntity
			message := enrollErr.Error()
			if errors.Is(enrollErr, domain.ErrInvalidPaymentMethod) {
				status = http.StatusBadRequest
			}
			if errors.Is(enrollErr, domain.ErrPrivateCheckout) {
				message = domain.ErrPrivateCheckout.Error()
			}
			if catalogEnrollmentErrorStatus(enrollErr) == http.StatusInternalServerError {
				status = http.StatusInternalServerError
				logInternalError(c.Request.Context(), "create parent tenant enrollment", enrollErr)
				message = "Failed to create enrollment"
			}
			body := gin.H{"status": "error", "message": message, "data": nil}
			if code := enrollmentErrorCode(enrollErr); code != "" {
				body["code"] = code
			}
			c.JSON(status, body)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"status": "success", "message": "Enrollment created and invoice generated", "data": result})
		return
	}
	// Tenant-scoped callers may only enroll inside their own tenant, so the path
	// segment has to match the verified JWT claim before any use case runs.
	claimTenantID, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
		return
	}
	if claimTenantID != pathTenantID {
		c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": "Tenant mismatch: enrollment is not allowed for another tenant", "data": nil})
		return
	}
	res, err := h.enrollmentUsecase.EnrollStudent(c.Request.Context(), pathTenantID, &req)
	if err != nil {
		status := http.StatusInternalServerError
		message := "Failed to create enrollment"
		body := gin.H{"status": "error"}
		if errors.Is(err, domain.ErrInvalidPaymentMethod) {
			status, message = http.StatusBadRequest, domain.ErrInvalidPaymentMethod.Error()
		} else if errors.Is(err, domain.ErrIdempotencyConflict) {
			status = http.StatusConflict
		} else if errors.Is(err, domain.ErrDuplicateEnrollment) {
			status, message = http.StatusConflict, domain.ErrDuplicateEnrollment.Error()
			body["code"] = domain.DuplicateEnrollmentErrorCode
		} else if errors.Is(err, domain.ErrPlatformFeeExceedsGross) {
			status, message = http.StatusUnprocessableEntity, domain.ErrPlatformFeeExceedsGross.Error()
			body["code"] = domain.PlatformFeeExceedsGrossErrorCode
		} else if errors.Is(err, domain.ErrPrivateCheckout) {
			status, message = http.StatusUnprocessableEntity, domain.ErrPrivateCheckout.Error()
			body["code"] = domain.PrivateCheckoutErrorCode
		} else {
			logInternalError(c.Request.Context(), "create tenant enrollment", err)
		}
		body["message"] = message
		c.JSON(status, body)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "success", "message": "Enrollment created and invoice generated", "data": res})
}

// CreateCatalogEnrollment godoc
// @Summary Enroll a parent-owned student in a public class
// @Description Creates a pending group enrollment and generates a billing invoice. Direct private checkout answers 422 with code `private_schedule_request_required`; submit a schedule request instead. The tenant is resolved from the selected class. Retrying with the same Idempotency-Key returns the same enrollment. A 409 with `code` `duplicate_enrollment` means the student already has a pending or active enrollment in this class (including a concurrent request that won the race); a 409 without `code` is a full schedule or an Idempotency-Key reused with a different request. A 422 with `code` `platform_fee_exceeds_gross` means billing refused the invoice because the platform fee exceeds the payment amount: the attempt holds no seat and does not block a new attempt, and a replay with the same Idempotency-Key answers the same 422. A dropped or completed enrollment does not block a new one.
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
	// KEL-75: the billing contact comes from the verified token's email claim, never
	// from client input, so it is injected after the body has been bound. The
	// middleware has already normalised the value (TrimSpace, case preserved).
	req.SenderEmail = c.GetString("email")
	result, err := h.enrollmentUsecase.EnrollPublic(c.Request.Context(), parentID, classID, &req, key)
	if err != nil {
		status := catalogEnrollmentErrorStatus(err)
		message := err.Error()
		if status == http.StatusInternalServerError {
			logInternalError(c.Request.Context(), "create catalog enrollment", err)
			message = "Failed to create enrollment"
		}
		body := gin.H{"status": "error", "message": message, "data": nil}
		if code := enrollmentErrorCode(err); code != "" {
			body["code"] = code
		}
		c.JSON(status, body)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "success", "message": "Enrollment created and invoice generated", "data": result})
}

// Cancel godoc
// @Summary Cancel a parent-owned pending enrollment
// @Description Lets a parent withdraw an enrollment that has not been paid yet. The related unpaid billing transaction is marked `cancelled` before the enrollment moves to `dropped`, so the seat returns to the catalog. An enrollment that belongs to another parent is answered as not found, an already cancelled enrollment is answered successfully, and an enrollment that is active or has already settled is refused.
// @Tags Enrollments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Enrollment ID (UUID)"
// @Success 200 {object} domain.HTTPResponse{data=domain.EnrollmentResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/enrollments/{id}/cancel [post]
func (h *EnrollmentHandler) Cancel(c *gin.Context) {
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
	res, err := h.enrollmentUsecase.CancelPendingEnrollment(c.Request.Context(), parentID, enrollmentID)
	if err != nil {
		status := http.StatusInternalServerError
		message := "Failed to cancel enrollment"
		switch {
		case errors.Is(err, usecase.ErrEnrollmentNotFound):
			status, message = http.StatusNotFound, "Enrollment not found"
		case errors.Is(err, domain.ErrInvalidEnrollmentTransition):
			status, message = http.StatusConflict, "Enrollment can no longer be cancelled"
		case errors.Is(err, domain.ErrParentRequired):
			status, message = http.StatusForbidden, "Parent authentication is required"
		default:
			logInternalError(c.Request.Context(), "cancel enrollment", err)
		}
		c.JSON(status, gin.H{"status": "error", "message": message, "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Enrollment cancelled successfully", "data": res})
}

func catalogEnrollmentErrorStatus(err error) int {
	switch {
	case errors.Is(err, domain.ErrInvalidPaymentMethod):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrIdempotencyConflict), errors.Is(err, domain.ErrScheduleFull), errors.Is(err, domain.ErrDuplicateEnrollment):
		return http.StatusConflict
	case errors.Is(err, domain.ErrPrivateCheckout), errors.Is(err, domain.ErrStudentOwnership), errors.Is(err, domain.ErrClassNotEnrollable), errors.Is(err, domain.ErrScheduleClassMismatch), errors.Is(err, domain.ErrScheduleRequired), errors.Is(err, domain.ErrScheduleEnded), errors.Is(err, domain.ErrPlatformFeeExceedsGross):
		return http.StatusUnprocessableEntity
	case errors.Is(err, domain.ErrClassNotFound), errors.Is(err, domain.ErrStudentNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrParentRequired):
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

// enrollmentErrorCode returns the machine-readable `code` for enrollment errors that
// share an HTTP status with other errors and must stay distinguishable: the
// duplicate enrollment (409) and billing's platform fee rejection (422). A full
// schedule and an idempotency conflict keep their existing 409 body without a code.
func enrollmentErrorCode(err error) string {
	switch {
	case errors.Is(err, domain.ErrPrivateCheckout):
		return domain.PrivateCheckoutErrorCode
	case errors.Is(err, domain.ErrDuplicateEnrollment):
		return domain.DuplicateEnrollmentErrorCode
	case errors.Is(err, domain.ErrPlatformFeeExceedsGross):
		return domain.PlatformFeeExceedsGrossErrorCode
	}
	return ""
}

func NewEnrollmentHandler(enrollmentUsecase usecase.EnrollmentUsecase) *EnrollmentHandler {
	return &EnrollmentHandler{
		enrollmentUsecase: enrollmentUsecase,
	}
}

// ReleaseInternal godoc
// @Summary Release the seat of an enrollment whose payment failed or expired
// @Description Internal service-to-service endpoint that transitions a pending enrollment to `dropped` so its schedule seat becomes available again. The transition is idempotent and never revokes an active enrollment.
// @Tags Enrollments
// @Accept json
// @Produce json
// @Security InternalServiceCredential
// @Param id path string true "Enrollment ID (UUID)"
// @Success 200 {object} domain.HTTPResponse{data=domain.EnrollmentResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /internal/enrollments/{id}/release [put]
func (h *EnrollmentHandler) ReleaseInternal(c *gin.Context) {
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

	res, err := h.enrollmentUsecase.ReleaseEnrollment(c.Request.Context(), enrollmentID)
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
		message := "Failed to release enrollment"
		if errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
			status = http.StatusConflict
			message = "Failed to release enrollment: " + err.Error()
		} else {
			logInternalError(c.Request.Context(), "release enrollment", err)
		}
		c.JSON(status, gin.H{
			"status":  "error",
			"message": message,
			"data":    nil,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Enrollment released successfully",
		"data":    res,
	})
}

// ActivateInternal godoc
// @Summary Activate enrollment after confirmed payment
// @Description Internal service-to-service endpoint for payment-confirmed enrollment activation.
// @Tags Enrollments
// @Accept json
// @Produce json
// @Security InternalServiceCredential
// @Param id path string true "Enrollment ID (UUID)"
// @Success 200 {object} domain.HTTPResponse{data=domain.EnrollmentResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
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
		message := "Failed to update enrollment status"
		if errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
			status = http.StatusConflict
			message = "Failed to update enrollment status: " + err.Error()
		} else {
			logInternalError(c.Request.Context(), "activate enrollment", err)
		}
		c.JSON(status, gin.H{
			"status":  "error",
			"message": message,
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

// writeInternalTransitionError answers the shared error table of the suspend,
// resume and end endpoints. The caller's retry policy depends on the
// distinction: 404 and 409 are permanent answers for this enrollment state,
// everything else is an outage the caller may retry.
func writeInternalTransitionError(c *gin.Context, operation string, err error) {
	if errors.Is(err, usecase.ErrEnrollmentNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"status":  "error",
			"message": err.Error(),
			"data":    nil,
		})
		return
	}
	status := http.StatusInternalServerError
	message := "Failed to " + operation + " enrollment"
	switch {
	case errors.Is(err, domain.ErrInvalidEnrollmentTransition),
		errors.Is(err, domain.ErrScheduleFull),
		errors.Is(err, domain.ErrEnrollmentSuspendedConflict):
		status = http.StatusConflict
		message = message + ": " + err.Error()
	default:
		logInternalError(c.Request.Context(), operation+" enrollment", err)
	}
	c.JSON(status, gin.H{
		"status":  "error",
		"message": message,
		"data":    nil,
	})
}

// SuspendInternal godoc
// @Summary Suspend an active enrollment and free its seat
// @Description Internal service-to-service endpoint that moves an `active` enrollment to `suspended` (KEL-149). A suspended enrollment holds no seat: the schedule capacity and catalog availability stop counting it, the student disappears from upcoming session attendees, and attendance can no longer be recorded for it. The transition is idempotent — repeating it answers the suspended enrollment unchanged. A pending, completed or dropped enrollment answers 409 because suspension is defined only from active.
// @Tags Enrollments
// @Accept json
// @Produce json
// @Security InternalServiceCredential
// @Param id path string true "Enrollment ID (UUID)"
// @Success 200 {object} domain.HTTPResponse{data=domain.EnrollmentResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /internal/enrollments/{id}/suspend [put]
func (h *EnrollmentHandler) SuspendInternal(c *gin.Context) {
	enrollmentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "Invalid enrollment ID format",
			"data":    nil,
		})
		return
	}
	res, err := h.enrollmentUsecase.SuspendEnrollment(c.Request.Context(), enrollmentID)
	if err != nil {
		writeInternalTransitionError(c, "suspend", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Enrollment suspended successfully",
		"data":    res,
	})
}

// ResumeInternal godoc
// @Summary Resume a suspended enrollment by reclaiming its seat
// @Description Internal service-to-service endpoint that returns a `suspended` enrollment to `active` (KEL-149). The seat is reclaimed under the schedule lock with the same capacity and duplicate checks a new enrollment passes, so a full schedule answers 409 and the enrollment stays suspended. Repeating the call on an already active enrollment answers it unchanged. A pending, completed or dropped enrollment answers 409.
// @Tags Enrollments
// @Accept json
// @Produce json
// @Security InternalServiceCredential
// @Param id path string true "Enrollment ID (UUID)"
// @Success 200 {object} domain.HTTPResponse{data=domain.EnrollmentResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /internal/enrollments/{id}/resume [put]
func (h *EnrollmentHandler) ResumeInternal(c *gin.Context) {
	enrollmentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "Invalid enrollment ID format",
			"data":    nil,
		})
		return
	}
	res, err := h.enrollmentUsecase.ResumeEnrollment(c.Request.Context(), enrollmentID)
	if err != nil {
		writeInternalTransitionError(c, "resume", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Enrollment resumed successfully",
		"data":    res,
	})
}

// EndInternal godoc
// @Summary End an active or suspended enrollment permanently
// @Description Internal service-to-service endpoint that moves an `active` or `suspended` enrollment to `dropped` (KEL-149), the same terminal state a parent cancellation uses, so the seat is freed permanently and the student may enroll again. The transition is idempotent. A pending enrollment answers 409: it has no seat of its own and must go through the parent cancellation or the payment-failure release.
// @Tags Enrollments
// @Accept json
// @Produce json
// @Security InternalServiceCredential
// @Param id path string true "Enrollment ID (UUID)"
// @Success 200 {object} domain.HTTPResponse{data=domain.EnrollmentResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /internal/enrollments/{id}/end [put]
func (h *EnrollmentHandler) EndInternal(c *gin.Context) {
	enrollmentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "Invalid enrollment ID format",
			"data":    nil,
		})
		return
	}
	res, err := h.enrollmentUsecase.EndEnrollment(c.Request.Context(), enrollmentID)
	if err != nil {
		writeInternalTransitionError(c, "end", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Enrollment ended successfully",
		"data":    res,
	})
}
