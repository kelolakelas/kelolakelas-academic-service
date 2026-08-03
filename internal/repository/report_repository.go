package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type reportRepository struct {
	db *gorm.DB
}

func NewReportRepository(db *gorm.DB) ReportRepository {
	return &reportRepository{db: db}
}

func (r *reportRepository) Create(ctx context.Context, report *domain.Report) error {
	return r.db.WithContext(ctx).Create(report).Error
}

func (r *reportRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Report, error) {
	var item domain.Report
	err := r.db.WithContext(ctx).Preload("Enrollment").First(&item, "id = ?", id).Error
	return &item, err
}

func (r *reportRepository) GetByIDForTenant(ctx context.Context, tenantID, id uuid.UUID) (*domain.Report, error) {
	var item domain.Report
	err := r.db.WithContext(ctx).
		Preload("Enrollment").
		Where("tenant_id = ? AND id = ?", tenantID, id).
		First(&item).Error
	return &item, err
}

func (r *reportRepository) List(ctx context.Context, tenantID uuid.UUID, query domain.ReportQuery) ([]domain.Report, int64, error) {
	db := r.db.WithContext(ctx).Where("reports.tenant_id = ?", tenantID)

	if query.EnrollmentID != nil {
		db = db.Where("reports.enrollment_id = ?", *query.EnrollmentID)
	}
	if query.ReporterID != nil {
		db = db.Where("reports.reporter_id = ?", *query.ReporterID)
	}
	if query.StudentID != nil {
		db = db.Joins("JOIN enrollments e ON e.id = reports.enrollment_id").
			Where("e.student_id = ?", *query.StudentID)
	}
	if query.DateFrom != nil {
		db = db.Where("reports.created_at >= ?", *query.DateFrom)
	}
	if query.DateTo != nil {
		db = db.Where("reports.created_at <= ?", *query.DateTo)
	}
	if query.Search != "" {
		db = db.Where(
			"reports.title ILIKE ? OR reports.evaluation_notes ILIKE ?",
			"%"+query.Search+"%",
			"%"+query.Search+"%",
		)
	}

	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var items []domain.Report
	err := db.Preload("Enrollment").
		Order("reports.created_at DESC").
		Limit(query.PageSize).
		Offset((query.Page - 1) * query.PageSize).
		Find(&items).Error
	return items, total, err
}

func (r *reportRepository) Update(ctx context.Context, report *domain.Report) error {
	return r.db.WithContext(ctx).Save(report).Error
}

func (r *reportRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&domain.Report{}, "id = ?", id).Error
}
