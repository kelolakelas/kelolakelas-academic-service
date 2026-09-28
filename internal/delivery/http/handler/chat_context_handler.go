package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type ChatContextReader interface {
	ScheduleRequest(context.Context, uuid.UUID) (*domain.ScheduleRequestChatContext, error)
	Report(context.Context, uuid.UUID) (*domain.ReportChatContext, error)
}

type ChatContextHandler struct{ reader ChatContextReader }

func NewChatContextHandler(reader ChatContextReader) *ChatContextHandler {
	return &ChatContextHandler{reader: reader}
}

// ScheduleRequest godoc
// @Summary Get internal schedule request chat context
// @Tags Chat Context
// @Produce json
// @Security InternalServiceCredential
// @Param id path string true "Schedule request ID (UUID)"
// @Success 200 {object} domain.HTTPResponse{data=domain.ScheduleRequestChatContext}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /internal/chat-context/schedule-requests/{id} [get]
func (h *ChatContextHandler) ScheduleRequest(c *gin.Context) {
	id, ok := chatContextID(c)
	if !ok {
		return
	}
	item, err := h.reader.ScheduleRequest(c.Request.Context(), id)
	if !chatContextError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Schedule request context retrieved successfully", "data": item})
}

// Report godoc
// @Summary Get internal report chat context
// @Tags Chat Context
// @Produce json
// @Security InternalServiceCredential
// @Param id path string true "Report ID (UUID)"
// @Success 200 {object} domain.HTTPResponse{data=domain.ReportChatContext}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /internal/chat-context/reports/{id} [get]
func (h *ChatContextHandler) Report(c *gin.Context) {
	id, ok := chatContextID(c)
	if !ok {
		return
	}
	item, err := h.reader.Report(c.Request.Context(), id)
	if !chatContextError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Report context retrieved successfully", "data": item})
}

func chatContextID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid context ID format", "data": nil})
		return uuid.Nil, false
	}
	return id, true
}

func chatContextError(c *gin.Context, err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, domain.ErrChatContextNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "Chat context not found", "data": nil})
		return false
	}
	slog.ErrorContext(c.Request.Context(), "Failed to retrieve chat context", "error", err)
	c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to retrieve chat context", "data": nil})
	return false
}
