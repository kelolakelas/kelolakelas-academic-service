package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

// PreviewVoucher godoc
// @Summary Preview a voucher for parent group checkout
// @Description Uses the published group class tenant and price; never reserves a voucher or seat.
// @Tags Enrollments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param class_id path string true "Class ID (UUID)"
// @Param request body domain.VoucherPreviewRequest true "Voucher code"
// @Success 200 {object} domain.HTTPResponse{data=billing.VoucherPreviewResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 422 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/catalog/classes/{class_id}/voucher-preview [post]
func (h *EnrollmentHandler) PreviewVoucher(c *gin.Context) {
	parentID, err := uuid.Parse(c.GetString("user_id"))
	if err != nil || parentID == uuid.Nil || !c.GetBool("is_parent") {
		c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": "Parent authentication is required", "data": nil})
		return
	}
	classID, err := uuid.Parse(c.Param("class_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid class ID", "data": nil})
		return
	}
	var req domain.VoucherPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid voucher preview request", "data": nil})
		return
	}
	preview, ok := h.enrollmentUsecase.(usecase.VoucherPreviewUsecase)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Voucher preview unavailable", "data": nil})
		return
	}
	result, err := preview.PreviewVoucher(c.Request.Context(), parentID, classID, req.VoucherCode)
	if err != nil {
		status := catalogEnrollmentErrorStatus(err)
		message := err.Error()
		if status == http.StatusInternalServerError {
			logInternalError(c.Request.Context(), "preview voucher", err)
			message = "Failed to preview voucher"
		}
		body := gin.H{"status": "error", "message": message, "data": nil}
		if code := enrollmentErrorCode(err); code != "" {
			body["code"] = code
		}
		c.JSON(status, body)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Voucher preview", "data": result})
}
