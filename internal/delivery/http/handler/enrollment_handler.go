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
	res, err := h.enrollmentUsecase.EnrollStudent(c.Request.Context(), tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "success", "message": "Enrollment created and invoice generated", "data": res})
}

func NewEnrollmentHandler(enrollmentUsecase usecase.EnrollmentUsecase) *EnrollmentHandler {
	return &EnrollmentHandler{
		enrollmentUsecase: enrollmentUsecase,
	}
}

// UpdateStatus godoc
// @Summary Update enrollment status
// @Description Update status of an enrollment (e.g. from pending to active upon payment)
// @Tags Enrollments
// @Accept json
// @Produce json
// @Param id path string true "Enrollment ID (UUID)"
// @Param request body domain.UpdateEnrollmentStatusRequest true "Status update payload"
// @Success 200 {object} domain.HTTPResponse{data=domain.EnrollmentResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/enrollments/{id}/status [put]
func (h *EnrollmentHandler) UpdateStatus(c *gin.Context) {
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

	var req domain.UpdateEnrollmentStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": err.Error(),
			"data":    nil,
		})
		return
	}

	res, err := h.enrollmentUsecase.UpdateEnrollmentStatus(c.Request.Context(), enrollmentID, req.Status)
	if err != nil {
		if errors.Is(err, usecase.ErrEnrollmentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"status":  "error",
				"message": err.Error(),
				"data":    nil,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": "Failed to update enrollment status: " + err.Error(),
			"data":    nil,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Enrollment status updated successfully",
		"data":    res,
	})
}
