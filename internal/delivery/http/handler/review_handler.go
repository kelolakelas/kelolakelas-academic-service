package handler

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"gorm.io/gorm"
)

type ReviewHandler struct {
	repo    domain.ReviewRepository
	catalog usecase.CatalogUsecase
}

func NewReviewHandler(repo domain.ReviewRepository, catalog usecase.CatalogUsecase) *ReviewHandler {
	return &ReviewHandler{repo: repo, catalog: catalog}
}

// UpsertReview godoc
// @Summary Create or replace the parent's review for an eligible enrollment
// @Tags Reviews
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Enrollment UUID"
// @Param body body domain.ReviewRequest true "Rating and optional comment (max 2000 characters)"
// @Success 200 {object} domain.HTTPResponse
// @Failure 400 {object} domain.ErrorResponse
// @Failure 403 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/enrollments/{id}/review [put]
func (h *ReviewHandler) Upsert(c *gin.Context) {
	parentID, err := uuid.Parse(c.GetString("user_id"))
	if !c.GetBool("is_parent") || err != nil {
		c.JSON(http.StatusForbidden, gin.H{"status": "error", "message": "Parent authentication is required", "data": nil})
		return
	}
	enrollmentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid enrollment ID", "data": nil})
		return
	}
	var req domain.ReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Rating < 1 || req.Rating > 5 || utf8.RuneCountInString(req.Comment) > domain.MaxReviewCommentLength {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Rating must be 1-5 and comment at most 2000 characters", "data": nil})
		return
	}
	if err := h.repo.Upsert(c.Request.Context(), parentID, enrollmentID, req.Rating, req.Comment); err != nil {
		if errors.Is(err, domain.ErrReviewNotEligible) {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "Eligible enrollment not found", "data": nil})
			return
		}
		logInternalError(c.Request.Context(), "upsert review", err)
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to save review", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Review saved", "data": nil})
}

// ListClassReviews godoc
// @Summary List public class reviews
// @Tags Reviews
// @Produce json
// @Param id path string true "Class UUID"
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Items per page" default(20)
// @Success 200 {object} domain.HTTPResponse{data=domain.ReviewListResponse}
// @Failure 400 {object} domain.ErrorResponse
// @Failure 404 {object} domain.ErrorResponse
// @Failure 503 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /api/v1/catalog/classes/{id}/reviews [get]
func (h *ReviewHandler) List(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "Class not found", "data": nil})
		return
	}
	page, pageSize := 1, 20
	for key, target := range map[string]*int{"page": &page, "page_size": &pageSize} {
		if c.Query(key) != "" {
			value, err := strconv.Atoi(c.Query(key))
			limit := domain.MaxPageSize
			if key == "page" {
				limit = domain.MaxPage
			}
			if err != nil || value < 1 || value > limit {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Invalid pagination", "data": nil})
				return
			}
			*target = value
		}
	}
	if _, err := h.catalog.GetClass(c.Request.Context(), id); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, domain.ErrCatalogClosed) {
			status = http.StatusNotFound
		}
		if errors.Is(err, domain.ErrCatalogPolicyUnavailable) {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, gin.H{"status": "error", "message": "Class not available", "data": nil})
		return
	}
	items, total, err := h.repo.List(c.Request.Context(), id, page, pageSize)
	if err != nil {
		logInternalError(c.Request.Context(), "list class reviews", err)
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Failed to fetch reviews", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Reviews fetched", "data": domain.ReviewListResponse{
		Items: items, Pagination: domain.Pagination{Page: page, PageSize: pageSize, TotalItems: total, TotalPages: int(math.Ceil(float64(total) / float64(pageSize)))},
	}})
}
