package usecase

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

type EnrollmentUsecase interface {
	EnrollStudent(ctx context.Context, tenantID uuid.UUID, req *domain.EnrollStudentRequest) (*domain.EnrollmentResponse, error)
	UpdateEnrollmentStatus(ctx context.Context, enrollmentID uuid.UUID, status string) (*domain.EnrollmentResponse, error)
	List(ctx context.Context, tenantID, parentID *uuid.UUID, query domain.EnrollmentQuery) (*domain.EnrollmentListResponse, error)
	GetByID(ctx context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID) (*domain.EnrollmentResponse, error)
}

type enrollmentUsecase struct {
	enrollmentRepo repository.EnrollmentRepository
	studentRepo    repository.StudentRepository
	classRepo      repository.ClassRepository
	billingClient  billing.Client
}

func NewEnrollmentUsecase(enrollmentRepo repository.EnrollmentRepository, studentRepo repository.StudentRepository, classRepo repository.ClassRepository, billingClient billing.Client) EnrollmentUsecase {
	return &enrollmentUsecase{
		enrollmentRepo: enrollmentRepo,
		studentRepo:    studentRepo, classRepo: classRepo, billingClient: billingClient,
	}
}

func (u *enrollmentUsecase) EnrollStudent(ctx context.Context, tenantID uuid.UUID, req *domain.EnrollStudentRequest) (*domain.EnrollmentResponse, error) {
	student, err := u.studentRepo.GetByID(ctx, req.StudentID)
	if err != nil {
		return nil, fmt.Errorf("student not found: %w", err)
	}
	class, err := u.classRepo.GetByID(ctx, req.ClassID)
	if err != nil {
		return nil, fmt.Errorf("class not found: %w", err)
	}
	if class.TenantID != tenantID {
		return nil, fmt.Errorf("class does not belong to tenant")
	}
	duplicate, err := u.enrollmentRepo.ExistsActive(ctx, req.StudentID, req.ClassID)
	if err != nil {
		return nil, fmt.Errorf("check enrollment: %w", err)
	}
	if duplicate {
		return nil, fmt.Errorf("active enrollment already exists")
	}
	enrollment := &domain.Enrollment{ID: uuid.New(), TenantID: tenantID, StudentID: req.StudentID, ClassID: req.ClassID, Status: "pending"}
	if err := u.enrollmentRepo.Create(ctx, enrollment); err != nil {
		return nil, fmt.Errorf("create enrollment: %w", err)
	}
	if _, err := u.billingClient.GenerateInvoice(ctx, billing.InvoiceRequest{TenantID: tenantID, StudentID: req.StudentID, ClassID: req.ClassID, EnrollmentID: enrollment.ID, ParentID: student.ParentID, BillingCycle: req.BillingCycle, SubtotalAmount: class.Price, PlatformFee: req.PlatformFee, Title: class.Name}); err != nil {
		return nil, fmt.Errorf("generate enrollment invoice: %w", err)
	}
	return enrollmentResponse(enrollment), nil
}

func (u *enrollmentUsecase) UpdateEnrollmentStatus(ctx context.Context, enrollmentID uuid.UUID, status string) (*domain.EnrollmentResponse, error) {
	if status != "pending" && status != "active" && status != "completed" && status != "dropped" {
		return nil, domain.ErrInvalidEnrollmentStatus
	}
	enrollment, err := u.enrollmentRepo.GetByID(ctx, enrollmentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrEnrollmentNotFound
		}
		return nil, fmt.Errorf("failed to fetch enrollment: %w", err)
	}

	enrollment.Status = status
	enrollment.UpdatedAt = time.Now()

	if err := u.enrollmentRepo.Update(ctx, enrollment); err != nil {
		return nil, fmt.Errorf("failed to update enrollment status: %w", err)
	}

	return enrollmentResponse(enrollment), nil
}

func (u *enrollmentUsecase) List(ctx context.Context, tenantID, parentID *uuid.UUID, query domain.EnrollmentQuery) (*domain.EnrollmentListResponse, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 || query.PageSize > 100 {
		query.PageSize = 20
	}
	items, total, err := u.enrollmentRepo.List(ctx, tenantID, parentID, query)
	if err != nil {
		return nil, err
	}
	responses := make([]*domain.EnrollmentResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, enrollmentResponse(item))
	}
	return &domain.EnrollmentListResponse{
		Items: responses,
		Pagination: domain.Pagination{
			Page:       query.Page,
			PageSize:   query.PageSize,
			TotalItems: total,
			TotalPages: int(math.Ceil(float64(total) / float64(query.PageSize))),
		},
	}, nil
}

func (u *enrollmentUsecase) GetByID(ctx context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID) (*domain.EnrollmentResponse, error) {
	item, err := u.enrollmentRepo.GetByIDForAccess(ctx, tenantID, parentID, id)
	if err != nil {
		return nil, err
	}
	return enrollmentResponse(item), nil
}

func enrollmentResponse(enrollment *domain.Enrollment) *domain.EnrollmentResponse {
	return &domain.EnrollmentResponse{
		ID: enrollment.ID, TenantID: enrollment.TenantID, StudentID: enrollment.StudentID,
		ClassID: enrollment.ClassID, Status: enrollment.Status, JoinedAt: enrollment.JoinedAt,
		UpdatedAt: enrollment.UpdatedAt, Class: enrollment.Class, Student: enrollment.Student,
	}
}
