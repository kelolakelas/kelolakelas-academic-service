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
	// KEL-140: Model pins the table so Count works on a real database; the
	// tenant predicate is unchanged. (Found by the KEL-140 AC4 regression
	// test: without a table the count query fails outside sqlite mocks.)
	db := r.db.WithContext(ctx).Model(&domain.Report{}).Where("reports.tenant_id = ?", tenantID)

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
		search := "%" + escapeLikePattern(query.Search) + "%"
		db = db.Where(
			"reports.title ILIKE ? ESCAPE '\\' OR reports.evaluation_notes ILIKE ? ESCAPE '\\'",
			search,
			search,
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

// ListForParent lists reports across every tenant that belong to the
// parent's children (KEL-140): a report is included only when its enrollment
// is held by a student whose parent_id is the caller. The tenant claim is
// never consulted. Client-supplied enrollment, student, or reporter filters
// narrow the parent's own rows and can never widen them to another parent's
// children.
func (r *reportRepository) ListForParent(ctx context.Context, parentID uuid.UUID, query domain.ReportQuery) ([]domain.Report, int64, error) {
	db := r.db.WithContext(ctx).Model(&domain.Report{}).
		Joins("JOIN enrollments e_own ON e_own.id = reports.enrollment_id").
		Joins("JOIN students s_own ON s_own.id = e_own.student_id").
		Where("s_own.parent_id = ?", parentID)

	if query.EnrollmentID != nil {
		db = db.Where("reports.enrollment_id = ?", *query.EnrollmentID)
	}

	if query.ReporterID != nil {
		db = db.Where("reports.reporter_id = ?", *query.ReporterID)
	}

	if query.StudentID != nil {
		db = db.Where("e_own.student_id = ?", *query.StudentID)
	}

	if query.DateFrom != nil {
		db = db.Where("reports.created_at >= ?", *query.DateFrom)
	}

	if query.DateTo != nil {
		db = db.Where("reports.created_at <= ?", *query.DateTo)
	}

	if query.Search != "" {
		search := "%" + escapeLikePattern(query.Search) + "%"
		db = db.Where(
			"reports.title ILIKE ? ESCAPE '\\' OR reports.evaluation_notes ILIKE ? ESCAPE '\\'",
			search,
			search,
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

// GetForParent resolves one report for a parent (KEL-140), or
// gorm.ErrRecordNotFound when the report does not belong to the parent's
// children. The caller answers not-found (404), never forbidden, so ids do
// not leak across parents.
func (r *reportRepository) GetForParent(ctx context.Context, parentID, id uuid.UUID) (*domain.Report, error) {
	var item domain.Report
	err := r.db.WithContext(ctx).
		Preload("Enrollment").
		Joins("JOIN enrollments e ON e.id = reports.enrollment_id").
		Joins("JOIN students s ON s.id = e.student_id").
		Where("reports.id = ? AND s.parent_id = ?", id, parentID).
		First(&item).Error
	return &item, err
}

func (r *reportRepository) Update(ctx context.Context, report *domain.Report) error {
	return r.db.WithContext(ctx).Save(report).Error
}

func (r *reportRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&domain.Report{}, "id = ?", id).Error
}
