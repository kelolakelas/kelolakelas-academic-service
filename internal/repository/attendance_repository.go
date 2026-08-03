package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type attendanceRepository struct {
	db *gorm.DB
}

func NewAttendanceRepository(db *gorm.DB) AttendanceRepository {
	return &attendanceRepository{db: db}
}

func (r *attendanceRepository) Create(ctx context.Context, attendance *domain.Attendance) error {
	return r.db.WithContext(ctx).Create(attendance).Error
}

func (r *attendanceRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Attendance, error) {
	var item domain.Attendance
	err := r.db.WithContext(ctx).Preload("Session").First(&item, "id = ?", id).Error
	return &item, err
}

func (r *attendanceRepository) GetByIDForTenant(ctx context.Context, tenantID, id uuid.UUID) (*domain.Attendance, error) {
	var item domain.Attendance
	err := r.db.WithContext(ctx).
		Table("attendances a").
		Joins("JOIN class_sessions cs ON cs.id = a.session_id").
		Joins("JOIN classes c ON c.id = cs.class_id").
		Where("a.id = ? AND c.tenant_id = ?", id, tenantID).
		Select("a.*").
		Preload("Session").
		First(&item).Error
	return &item, err
}

func (r *attendanceRepository) GetByUnique(ctx context.Context, enrollmentID, sessionID uuid.UUID, date time.Time) (*domain.Attendance, error) {
	var item domain.Attendance
	err := r.db.WithContext(ctx).
		Where("enrollment_id = ? AND session_id = ? AND date = ?", enrollmentID, sessionID, date).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &item, err
}

func (r *attendanceRepository) List(ctx context.Context, tenantID uuid.UUID, query domain.AttendanceQuery) ([]domain.Attendance, int64, error) {
	db := r.db.WithContext(ctx).
		Table("attendances a").
		Joins("JOIN class_sessions cs ON cs.id = a.session_id").
		Joins("JOIN classes c ON c.id = cs.class_id").
		Where("c.tenant_id = ?", tenantID)

	if query.EnrollmentID != nil {
		db = db.Where("a.enrollment_id = ?", *query.EnrollmentID)
	}
	if query.StudentID != nil {
		db = db.Joins("JOIN enrollments e ON e.id = a.enrollment_id").
			Where("e.student_id = ?", *query.StudentID)
	}
	if query.ScheduleID != nil {
		db = db.Joins("JOIN class_schedules sch ON sch.id = cs.schedule_id").
			Where("sch.id = ?", *query.ScheduleID)
	}
	if query.Status != "" {
		db = db.Where("a.status = ?", query.Status)
	}
	if query.DateFrom != nil {
		db = db.Where("a.date >= ?", *query.DateFrom)
	}
	if query.DateTo != nil {
		db = db.Where("a.date <= ?", *query.DateTo)
	}

	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var items []domain.Attendance
	err := db.Select("a.*").
		Preload("Session").
		Order("a.date DESC").
		Limit(query.PageSize).
		Offset((query.Page - 1) * query.PageSize).
		Find(&items).Error
	return items, total, err
}

func (r *attendanceRepository) Update(ctx context.Context, attendance *domain.Attendance) error {
	return r.db.WithContext(ctx).Save(attendance).Error
}
