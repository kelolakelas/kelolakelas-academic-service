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
	Create(ctx context.Context, tenantID, memberID uuid.UUID, req *domain.CreateReportRequest) (*domain.Report, error)
	Get(ctx context.Context, tenantID, id uuid.UUID) (*domain.Report, error)
	Update(ctx context.Context, tenantID, id uuid.UUID, req *domain.UpdateReportRequest) (*domain.Report, error)
	Delete(ctx context.Context, tenantID, id uuid.UUID) error
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
func (u *reportUsecase) Update(ctx context.Context, tenantID, id uuid.UUID, req *domain.UpdateReportRequest) (*domain.Report, error) {
	item, err := u.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	item.Title, item.EvaluationNotes, item.Score, item.UpdatedAt = req.Title, req.EvaluationNotes, req.Score, time.Now()
	if err := u.repo.Update(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}
func (u *reportUsecase) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	item, err := u.Get(ctx, tenantID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err != nil {
		return err
	}
	return u.repo.Delete(ctx, item.ID)
}
