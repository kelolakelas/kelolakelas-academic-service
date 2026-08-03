package handler

import (
	"errors"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
)

type AttendanceHandler struct{ usecase usecase.AttendanceUsecase }

func NewAttendanceHandler(u usecase.AttendanceUsecase) *AttendanceHandler {
	return &AttendanceHandler{usecase: u}
}
func attendanceTenant(c *gin.Context) (uuid.UUID, error) { return uuid.Parse(c.GetString("tenant_id")) }
func parseAttendanceQuery(c *gin.Context) (domain.AttendanceQuery, error) {
	q := domain.AttendanceQuery{Page: 1, PageSize: 20}
	for key, target := range map[string]*int{"page": &q.Page, "page_size": &q.PageSize} {
		if v := c.Query(key); v != "" {
			n, e := strconv.Atoi(v)
			if e != nil || n < 1 || (key == "page_size" && n > 100) {
				return q, errors.New("invalid pagination")
			}
			*target = n
		}
	}
	for key, target := range map[string]**uuid.UUID{"enrollment_id": &q.EnrollmentID, "student_id": &q.StudentID, "schedule_id": &q.ScheduleID} {
		if v := c.Query(key); v != "" {
			id, e := uuid.Parse(v)
			if e != nil {
				return q, errors.New("invalid " + key)
			}
			*target = &id
		}
	}
	if v := c.Query("status"); v != "" {
		switch v {
		case "present", "absent", "late", "excused":
			q.Status = v
		default:
			return q, domain.ErrInvalidAttendanceStatus
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
	if q.DateFrom != nil && q.DateTo != nil && q.DateFrom.After(*q.DateTo) {
		return q, errors.New("date_from must not be after date_to")
	}
	return q, nil
}

// @Summary List attendance
// @Tags Attendance
// @Produce json
// @Security BearerAuth
// @Success 200 {object} domain.HTTPResponse{data=domain.AttendanceListResponse}
// @Router /api/v1/attendance [get]
func (h *AttendanceHandler) List(c *gin.Context) {
	tenant, e := attendanceTenant(c)
	if e != nil {
		c.JSON(401, gin.H{"status": "error", "message": "Invalid tenant context", "data": nil})
		return
	}
	q, e := parseAttendanceQuery(c)
	if e != nil {
		c.JSON(400, gin.H{"status": "error", "message": e.Error(), "data": nil})
		return
	}
	r, e := h.usecase.List(c.Request.Context(), tenant, q)
	if e != nil {
		c.JSON(500, gin.H{"status": "error", "message": "Failed to fetch attendance", "data": nil})
		return
	}
	c.JSON(200, gin.H{"status": "success", "message": "Attendance fetched successfully", "data": r})
}

// @Summary Create attendance
// @Tags Attendance
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body domain.CreateAttendanceRequest true "Attendance payload"
// @Success 201 {object} domain.HTTPResponse{data=domain.Attendance}
// @Router /api/v1/attendance [post]
func (h *AttendanceHandler) Create(c *gin.Context) {
	tenant, e := attendanceTenant(c)
	if e != nil {
		c.JSON(401, gin.H{"status": "error", "message": "Invalid tenant context", "data": nil})
		return
	}
	uid, e := uuid.Parse(c.GetString("member_id"))
	if e != nil {
		c.JSON(401, gin.H{"status": "error", "message": "Invalid user context", "data": nil})
		return
	}
	var req domain.CreateAttendanceRequest
	if e = c.ShouldBindJSON(&req); e != nil {
		c.JSON(400, gin.H{"status": "error", "message": e.Error(), "data": nil})
		return
	}
	r, e := h.usecase.Create(c.Request.Context(), tenant, uid, &req)
	if errors.Is(e, domain.ErrAttendanceForbidden) {
		c.JSON(403, gin.H{"status": "error", "message": "Tutor is not assigned to this session", "data": nil})
		return
	}
	if errors.Is(e, domain.ErrAttendanceDuplicate) {
		c.JSON(409, gin.H{"status": "error", "message": "Attendance already exists", "data": nil})
		return
	}
	if e != nil {
		c.JSON(400, gin.H{"status": "error", "message": "Invalid attendance", "data": nil})
		return
	}
	c.JSON(201, gin.H{"status": "success", "message": "Attendance created successfully", "data": r})
}

// @Summary Get attendance
// @Tags Attendance
// @Produce json
// @Security BearerAuth
// @Param id path string true "Attendance UUID"
// @Success 200 {object} domain.HTTPResponse{data=domain.Attendance}
// @Router /api/v1/attendance/{id} [get]
func (h *AttendanceHandler) Get(c *gin.Context) {
	tenant, e := attendanceTenant(c)
	if e != nil {
		c.JSON(401, gin.H{"status": "error", "message": "Invalid tenant context", "data": nil})
		return
	}
	id, e := uuid.Parse(c.Param("id"))
	if e != nil {
		c.JSON(400, gin.H{"status": "error", "message": "Invalid attendance ID", "data": nil})
		return
	}
	r, e := h.usecase.Get(c.Request.Context(), tenant, id)
	if errors.Is(e, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"status": "error", "message": "Attendance not found", "data": nil})
		return
	}
	if e != nil {
		c.JSON(500, gin.H{"status": "error", "message": "Failed to fetch attendance", "data": nil})
		return
	}
	c.JSON(200, gin.H{"status": "success", "message": "Attendance fetched successfully", "data": r})
}

// @Summary Update attendance
// @Tags Attendance
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Attendance UUID"
// @Param request body domain.UpdateAttendanceRequest true "Attendance payload"
// @Success 200 {object} domain.HTTPResponse{data=domain.Attendance}
// @Router /api/v1/attendance/{id} [patch]
func (h *AttendanceHandler) Update(c *gin.Context) {
	tenant, e := attendanceTenant(c)
	if e != nil {
		c.JSON(401, gin.H{"status": "error", "message": "Invalid tenant context", "data": nil})
		return
	}
	id, e := uuid.Parse(c.Param("id"))
	if e != nil {
		c.JSON(400, gin.H{"status": "error", "message": "Invalid attendance ID", "data": nil})
		return
	}
	var req domain.UpdateAttendanceRequest
	if e = c.ShouldBindJSON(&req); e != nil {
		c.JSON(400, gin.H{"status": "error", "message": e.Error(), "data": nil})
		return
	}
	memberID, e := uuid.Parse(c.GetString("member_id"))
	if e != nil {
		c.JSON(401, gin.H{"status": "error", "message": "Invalid member context", "data": nil})
		return
	}
	r, e := h.usecase.Update(c.Request.Context(), tenant, memberID, id, &req)
	if errors.Is(e, domain.ErrAttendanceForbidden) {
		c.JSON(403, gin.H{"status": "error", "message": "Tutor is not assigned to this session", "data": nil})
		return
	}
	if errors.Is(e, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"status": "error", "message": "Attendance not found", "data": nil})
		return
	}
	if e != nil {
		c.JSON(500, gin.H{"status": "error", "message": "Failed to update attendance", "data": nil})
		return
	}
	c.JSON(200, gin.H{"status": "success", "message": "Attendance updated successfully", "data": r})
}
