package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/privatechat"
	"gorm.io/gorm"
	"net/http"
	"strconv"
)

type PrivateChatHandler struct{ Events *usecase.PrivateChatEvents }

// Event godoc
// @Summary Read a persisted private request chat event
// @Tags Internal private request chat
// @Produce json
// @Security InternalServiceCredential
// @Param id path string true "Event UUID"
// @Success 200 {object} domain.HTTPResponse{data=domain.PrivateChatProjection}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 503 {object} domain.ErrorResponse
// @Router /internal/private-request-chat/events/{id} [get]
func (h *PrivateChatHandler) Event(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		c.JSON(400, gin.H{"status": "error", "message": "Invalid event ID"})
		return
	}
	result, err := h.Events.Projection(c.Request.Context(), id)
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, domain.ErrChatContextNotFound) {
		c.JSON(404, gin.H{"status": "error", "message": "Event subject not found"})
		return
	}
	if err != nil {
		code := "event_dependency_unavailable"
		if errors.Is(err, privatechat.ErrMissingInvoice) {
			code = "invoice_missing"
		}
		if errors.Is(err, domain.ErrPrivateChatNotReady) {
			code = "invoice_or_decision_not_ready"
		}
		c.JSON(503, gin.H{"status": "error", "message": "Event or invoice not ready", "code": code})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": result})
}

// Diagnostics godoc
// @Summary List private chat deliveries by status
// @Tags Internal private request chat
// @Produce json
// @Security InternalServiceCredential
// @Param status query string false "Delivery status; default pending/sending/failed"
// @Param page query int false "Page of 100 rows, default 1"
// @Success 200 {object} domain.HTTPResponse
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 503 {object} domain.ErrorResponse
// @Router /internal/private-request-chat/deliveries [get]
func (h *PrivateChatHandler) Diagnostics(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	status := c.Query("status")
	if status != "" && status != "pending" && status != "sending" && status != "failed" && status != "sent" && status != "superseded" {
		c.JSON(400, gin.H{"status": "error", "message": "Invalid delivery status"})
		return
	}
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 || page > 100000 {
		c.JSON(400, gin.H{"status": "error", "message": "Invalid page"})
		return
	}
	rows, err := h.Events.Store.DiagnosticsPage(c.Request.Context(), status, page)
	if err != nil {
		c.JSON(503, gin.H{"status": "error", "message": "Deliveries unavailable"})
		return
	}
	c.JSON(200, gin.H{"status": "success", "data": rows})
}

// Requeue godoc
// @Summary Re-drive one failed private chat delivery
// @Tags Internal private request chat
// @Produce json
// @Security InternalServiceCredential
// @Param id path string true "Event UUID"
// @Success 200 {object} domain.HTTPResponse
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Router /internal/private-request-chat/deliveries/{id}/requeue [post]
func (h *PrivateChatHandler) Requeue(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		c.JSON(400, gin.H{"status": "error", "message": "Invalid event ID"})
		return
	}
	if err = h.Events.Store.Requeue(c.Request.Context(), id); err != nil {
		c.JSON(404, gin.H{"status": "error", "message": "Failed event not found"})
		return
	}
	c.JSON(200, gin.H{"status": "success", "data": gin.H{"event_id": id}})
}
