package usecase

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

type ReportUsecase interface {
	List(ctx context.Context, tenantID uuid.UUID, query domain.ReportQuery) (*domain.ReportListResponse, error)
	// ListForParent lists reports across every tenant that belong to the
	// parent's children (KEL-140). The tenant claim is never consulted.
	ListForParent(ctx context.Context, parentID uuid.UUID, query domain.ReportQuery) (*domain.ReportListResponse, error)
	Create(ctx context.Context, tenantID, memberID uuid.UUID, req *domain.CreateReportRequest) (*domain.Report, error)
	Get(ctx context.Context, tenantID, id uuid.UUID) (*domain.Report, error)
	// GetForParent resolves one report for a parent (KEL-140), or
	// gorm.ErrRecordNotFound when the report does not belong to the parent.
	GetForParent(ctx context.Context, parentID, id uuid.UUID) (*domain.Report, error)
	// Update and Delete take the caller's member claim so a tutor who does not
	// teach the report's class is rejected with ErrReportForbidden, exactly
	// like Create (KEL-135).
	Update(ctx context.Context, tenantID, memberID, id uuid.UUID, req *domain.UpdateReportRequest) (*domain.Report, error)
	Delete(ctx context.Context, tenantID, memberID, id uuid.UUID) error
}
type reportUsecase struct {
	repo        repository.ReportRepository
	enrollments repository.EnrollmentRepository
}

func NewReportUsecase(repo repository.ReportRepository, enrollments repository.EnrollmentRepository) ReportUsecase {
	return &reportUsecase{repo: repo, enrollments: enrollments}
}
func (u *reportUsecase) List(ctx context.Context, tenantID uuid.UUID, query domain.ReportQuery) (*domain.ReportListResponse, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 || query.PageSize > 100 {
		query.PageSize = 20
	}
	items, total, err := u.repo.List(ctx, tenantID, query)
	if err != nil {
		return nil, err
	}
	return &domain.ReportListResponse{Items: items, Pagination: domain.Pagination{Page: query.Page, PageSize: query.PageSize, TotalItems: total, TotalPages: int(math.Ceil(float64(total) / float64(query.PageSize)))}}, nil
}
func (u *reportUsecase) Create(ctx context.Context, tenantID, memberID uuid.UUID, req *domain.CreateReportRequest) (*domain.Report, error) {
	enrollment, err := u.enrollments.GetByIDForAccess(ctx, &tenantID, nil, req.EnrollmentID)
	if err != nil {
		return nil, err
	}
	assigned, err := u.enrollments.IsTutorForEnrollment(ctx, enrollment.ID, memberID)
	if err != nil {
		return nil, err
	}
	if !assigned {
		return nil, domain.ErrReportForbidden
	}
	item := &domain.Report{ID: uuid.New(), TenantID: tenantID, EnrollmentID: enrollment.ID, ReporterID: memberID, Title: req.Title, EvaluationNotes: req.EvaluationNotes, Score: req.Score}
	if err := u.repo.Create(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}
func (u *reportUsecase) Get(ctx context.Context, tenantID, id uuid.UUID) (*domain.Report, error) {
	return u.repo.GetByIDForTenant(ctx, tenantID, id)
}

// ListForParent lists reports across every tenant that belong to the
// parent's children (KEL-140). Pagination defaults match List.
func (u *reportUsecase) ListForParent(ctx context.Context, parentID uuid.UUID, query domain.ReportQuery) (*domain.ReportListResponse, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 || query.PageSize > 100 {
		query.PageSize = 20
	}
	items, total, err := u.repo.ListForParent(ctx, parentID, query)
	if err != nil {
		return nil, err
	}
	return &domain.ReportListResponse{Items: items, Pagination: domain.Pagination{Page: query.Page, PageSize: query.PageSize, TotalItems: total, TotalPages: int(math.Ceil(float64(total) / float64(query.PageSize)))}}, nil
}

// GetForParent resolves one report for a parent (KEL-140). A report of
// another parent's child answers gorm.ErrRecordNotFound (404 upstream).
func (u *reportUsecase) GetForParent(ctx context.Context, parentID, id uuid.UUID) (*domain.Report, error) {
	return u.repo.GetForParent(ctx, parentID, id)
}
func (u *reportUsecase) Update(ctx context.Context, tenantID, memberID, id uuid.UUID, req *domain.UpdateReportRequest) (*domain.Report, error) {
	item, err := u.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	// KEL-135: same assignment rule as Create — only a tutor teaching the
	// report's class may change it. The check reads the stored enrollment, so
	// a caller cannot retarget the report to a class they teach.
	assigned, err := u.enrollments.IsTutorForEnrollment(ctx, item.EnrollmentID, memberID)
	if err != nil {
		return nil, err
	}
	if !assigned {
		return nil, domain.ErrReportForbidden
	}
	item.Title, item.EvaluationNotes, item.Score, item.UpdatedAt = req.Title, req.EvaluationNotes, req.Score, time.Now()
	if err := u.repo.Update(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}
func (u *reportUsecase) Delete(ctx context.Context, tenantID, memberID, id uuid.UUID) error {
	item, err := u.Get(ctx, tenantID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err != nil {
		return err
	}
	// KEL-135: same assignment rule as Create — only a tutor teaching the
	// report's class may delete it.
	assigned, err := u.enrollments.IsTutorForEnrollment(ctx, item.EnrollmentID, memberID)
	if err != nil {
		return err
	}
	if !assigned {
		return domain.ErrReportForbidden
	}
	return u.repo.Delete(ctx, item.ID)
}
