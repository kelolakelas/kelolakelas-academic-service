package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/paymentlink"
	"io"
	"net/http"
)

func (h *PrivateScheduleRequestHandler) WithPaymentLink(u usecase.PrivatePaymentLinkUsecase) *PrivateScheduleRequestHandler {
	h.paymentLink = u
	return h
}

// SendPaymentLink godoc
// @Summary Send an existing private payment link to the request conversation
// @Description Requires live enrollment:update and chat:manage. Reads current billing transaction with caller authorization (billing:read required by billing); never generates an invoice. Retry is idempotent per request and transaction.
// @Tags Private schedule requests
// @Produce json
// @Security BearerAuth
// @x-permission {"permission":"enrollment:update","parent_tokens":"denied"}
// @Param id path string true "Request UUID"
// @Success 200 {object} domain.HTTPResponse{data=paymentlink.Message}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 409 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Failure 503 {object} domain.ErrorResponse
// @Router /api/v1/schedule-requests/{id}/send-payment-link [post]
func (h *PrivateScheduleRequestHandler) SendPaymentLink(c *gin.Context) {
	tenant, err := tenantIDFromContext(c)
	if err != nil {
		writeTenantError(c, err)
		return
	}
	id, ok := privateRequestID(c)
	if !ok {
		return
	}
	if c.Request.Body != nil {
		var b [1]byte
		n, e := c.Request.Body.Read(b[:])
		if n != 0 || (e != nil && e != io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "This endpoint accepts no request body", "data": nil})
			return
		}
	}
	if h.paymentLink == nil {
		err = paymentlink.ErrUnavailable
	} else {
		result, sendErr := h.paymentLink.SendPaymentLink(c.Request.Context(), tenant, id, c.GetHeader("Authorization"))
		err = sendErr
		if err == nil {
			c.JSON(http.StatusOK, domain.HTTPResponse{Status: "success", Data: result})
			return
		}
	}
	switch {
	case errors.Is(err, usecase.ErrPaymentLinkInactive):
		c.JSON(http.StatusConflict, gin.H{"status": "error", "message": err.Error(), "data": nil})
	case errors.Is(err, paymentlink.ErrUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "Payment services are unavailable", "data": nil})
	default:
		privateRequestError(c, err)
	}
}
