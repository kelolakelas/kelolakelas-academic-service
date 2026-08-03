package handler

import (
	"errors"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"gorm.io/gorm"
)

type ReportHandler struct{ usecase usecase.ReportUsecase }

func NewReportHandler(u usecase.ReportUsecase) *ReportHandler { return &ReportHandler{usecase: u} }
func reportTenant(c *gin.Context) (uuid.UUID, error)          { return uuid.Parse(c.GetString("tenant_id")) }
func parseReportQuery(c *gin.Context) (domain.ReportQuery, error) {
	q := domain.ReportQuery{Page: 1, PageSize: 20, Search: c.Query("search")}
	for key, target := range map[string]*int{"page": &q.Page, "page_size": &q.PageSize} {
		if v := c.Query(key); v != "" {
			n, e := strconv.Atoi(v)
			if e != nil || n < 1 || (key == "page_size" && n > 100) {
				return q, errors.New("invalid pagination")
			}
			*target = n
		}
	}
	for key, target := range map[string]**uuid.UUID{"enrollment_id": &q.EnrollmentID, "student_id": &q.StudentID, "reporter_id": &q.ReporterID} {
		if v := c.Query(key); v != "" {
			id, e := uuid.Parse(v)
			if e != nil {
				return q, errors.New("invalid " + key)
			}
			*target = &id
		}
	}
	for key, target := range map[string]**time.Time{"date_from": &q.DateFrom, "date_to": &q.DateTo} {
		if v := c.Query(key); v != "" {
			d, e := time.Parse("2006-01-02", v)
			if e != nil {
				return q, errors.New("invalid " + key)
			}
			*target = &d
		}
	}
	return q, nil
}

// @Summary List reports
// @Tags Reports
// @Produce json
// @Security BearerAuth
// @Success 200 {object} domain.HTTPResponse{data=domain.ReportListResponse}
// @Router /api/v1/reports [get]
func (h *ReportHandler) List(c *gin.Context) {
	tenant, e := reportTenant(c)
	if e != nil {
		c.JSON(401, gin.H{"status": "error", "message": "Invalid tenant context", "data": nil})
		return
	}
	q, e := parseReportQuery(c)
	if e != nil {
		c.JSON(400, gin.H{"status": "error", "message": e.Error(), "data": nil})
		return
	}
	r, e := h.usecase.List(c.Request.Context(), tenant, q)
	if e != nil {
		c.JSON(500, gin.H{"status": "error", "message": "Failed to fetch reports", "data": nil})
		return
	}
	c.JSON(200, gin.H{"status": "success", "message": "Reports fetched successfully", "data": r})
}

// @Summary Create report
// @Tags Reports
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body domain.CreateReportRequest true "Report payload"
// @Success 201 {object} domain.HTTPResponse{data=domain.Report}
// @Router /api/v1/reports [post]
func (h *ReportHandler) Create(c *gin.Context) {
	tenant, e := reportTenant(c)
	if e != nil {
		c.JSON(401, gin.H{"status": "error", "message": "Invalid tenant context", "data": nil})
		return
	}
	reporter, e := uuid.Parse(c.GetString("member_id"))
	if e != nil {
		c.JSON(401, gin.H{"status": "error", "message": "Invalid user context", "data": nil})
		return
	}
	var req domain.CreateReportRequest
	if e = c.ShouldBindJSON(&req); e != nil {
		c.JSON(400, gin.H{"status": "error", "message": e.Error(), "data": nil})
		return
	}
	r, e := h.usecase.Create(c.Request.Context(), tenant, reporter, &req)
	if errors.Is(e, domain.ErrReportForbidden) {
		c.JSON(403, gin.H{"status": "error", "message": "Tutor is not assigned to this enrollment", "data": nil})
		return
	}
	if e != nil {
		c.JSON(400, gin.H{"status": "error", "message": "Invalid report", "data": nil})
		return
	}
	c.JSON(201, gin.H{"status": "success", "message": "Report created successfully", "data": r})
}

// @Summary Get report
// @Tags Reports
// @Produce json
// @Security BearerAuth
// @Param id path string true "Report UUID"
// @Success 200 {object} domain.HTTPResponse{data=domain.Report}
// @Router /api/v1/reports/{id} [get]
func (h *ReportHandler) Get(c *gin.Context) {
	tenant, e := reportTenant(c)
	if e != nil {
		c.JSON(401, gin.H{"status": "error", "message": "Invalid tenant context", "data": nil})
		return
	}
	id, e := uuid.Parse(c.Param("id"))
	if e != nil {
		c.JSON(400, gin.H{"status": "error", "message": "Invalid report ID", "data": nil})
		return
	}
	r, e := h.usecase.Get(c.Request.Context(), tenant, id)
	if errors.Is(e, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"status": "error", "message": "Report not found", "data": nil})
		return
	}
	if e != nil {
		c.JSON(500, gin.H{"status": "error", "message": "Failed to fetch report", "data": nil})
		return
	}
	c.JSON(200, gin.H{"status": "success", "message": "Report fetched successfully", "data": r})
}

// @Summary Update report
// @Tags Reports
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Report UUID"
// @Param request body domain.UpdateReportRequest true "Report payload"
// @Success 200 {object} domain.HTTPResponse{data=domain.Report}
// @Router /api/v1/reports/{id} [patch]
func (h *ReportHandler) Update(c *gin.Context) {
	tenant, e := reportTenant(c)
	if e != nil {
		c.JSON(401, gin.H{"status": "error", "message": "Invalid tenant context", "data": nil})
		return
	}
	id, e := uuid.Parse(c.Param("id"))
	if e != nil {
		c.JSON(400, gin.H{"status": "error", "message": "Invalid report ID", "data": nil})
		return
	}
	var req domain.UpdateReportRequest
	if e = c.ShouldBindJSON(&req); e != nil {
		c.JSON(400, gin.H{"status": "error", "message": e.Error(), "data": nil})
		return
	}
	r, e := h.usecase.Update(c.Request.Context(), tenant, id, &req)
	if errors.Is(e, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"status": "error", "message": "Report not found", "data": nil})
		return
	}
	if e != nil {
		c.JSON(500, gin.H{"status": "error", "message": "Failed to update report", "data": nil})
		return
	}
	c.JSON(200, gin.H{"status": "success", "message": "Report updated successfully", "data": r})
}

// @Summary Delete report
// @Tags Reports
// @Produce json
// @Security BearerAuth
// @Param id path string true "Report UUID"
// @Success 200 {object} domain.HTTPResponse
// @Router /api/v1/reports/{id} [delete]
func (h *ReportHandler) Delete(c *gin.Context) {
	tenant, e := reportTenant(c)
	if e != nil {
		c.JSON(401, gin.H{"status": "error", "message": "Invalid tenant context", "data": nil})
		return
	}
	id, e := uuid.Parse(c.Param("id"))
	if e != nil {
		c.JSON(400, gin.H{"status": "error", "message": "Invalid report ID", "data": nil})
		return
	}
	e = h.usecase.Delete(c.Request.Context(), tenant, id)
	if errors.Is(e, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"status": "error", "message": "Report not found", "data": nil})
		return
	}
	if e != nil {
		c.JSON(500, gin.H{"status": "error", "message": "Failed to delete report", "data": nil})
		return
	}
	c.JSON(200, gin.H{"status": "success", "message": "Report deleted successfully", "data": nil})
}
