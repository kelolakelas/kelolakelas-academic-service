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
	EnrollPublic(ctx context.Context, parentID, classID uuid.UUID, req *domain.PublicEnrollmentRequest, idempotencyKey string) (*domain.PublicEnrollmentResponse, error)
	ActivateEnrollment(ctx context.Context, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error)
	List(ctx context.Context, tenantID, parentID *uuid.UUID, query domain.EnrollmentQuery) (*domain.EnrollmentListResponse, error)
	GetByID(ctx context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID) (*domain.EnrollmentResponse, error)
}

type enrollmentUsecase struct {
	enrollmentRepo repository.EnrollmentRepository
	studentRepo    repository.StudentRepository
	classRepo      repository.ClassRepository
	billingClient  billing.Client
	txManager      repository.TransactionManager
}

func NewEnrollmentUsecase(enrollmentRepo repository.EnrollmentRepository, studentRepo repository.StudentRepository, classRepo repository.ClassRepository, billingClient billing.Client, txManagers ...repository.TransactionManager) EnrollmentUsecase {
	var txManager repository.TransactionManager
	if len(txManagers) > 0 {
		txManager = txManagers[0]
	}
	return &enrollmentUsecase{
		enrollmentRepo: enrollmentRepo,
		studentRepo:    studentRepo, classRepo: classRepo, billingClient: billingClient, txManager: txManager,
	}
}

func (u *enrollmentUsecase) EnrollPublic(ctx context.Context, parentID, classID uuid.UUID, req *domain.PublicEnrollmentRequest, idempotencyKey string) (*domain.PublicEnrollmentResponse, error) {
	if parentID == uuid.Nil {
		return nil, domain.ErrParentRequired
	}
	if idempotencyKey == "" {
		return nil, errors.New("idempotency key is required")
	}
	if existing, err := u.enrollmentRepo.GetByIdempotencyKey(ctx, parentID, idempotencyKey); err == nil {
		if existing.ClassID != classID || existing.StudentID != req.StudentID || existing.BillingCycle != req.BillingCycle {
			return nil, domain.ErrIdempotencyConflict
		}
		if existing.PaymentTransactionID == nil {
			_, studentErr := u.studentRepo.GetByID(ctx, existing.StudentID)
			class, classErr := u.classRepo.GetByID(ctx, existing.ClassID)
			if studentErr != nil || classErr != nil {
				return nil, fmt.Errorf("recover enrollment dependencies")
			}
			invoice, invoiceErr := u.billingClient.GenerateInvoice(ctx, billing.InvoiceRequest{TenantID: existing.TenantID, StudentID: existing.StudentID, ClassID: existing.ClassID, EnrollmentID: existing.ID, ParentID: parentID, BillingCycle: existing.BillingCycle, SubtotalAmount: class.Price, IdempotencyKey: idempotencyKey, Title: class.Name})
			if invoiceErr != nil {
				return nil, fmt.Errorf("generate enrollment invoice: %w", invoiceErr)
			}
			existing.PaymentTransactionID = &invoice.TransactionID
			existing.CheckoutSessionURL = &invoice.CheckoutSessionURL
			existing.PaymentStatus = "pending"
			if updateErr := u.enrollmentRepo.Update(ctx, existing); updateErr != nil {
				return nil, updateErr
			}
		}
		return u.publicEnrollmentResponse(existing)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	student, err := u.studentRepo.GetByID(ctx, req.StudentID)
	if err != nil {
		return nil, domain.ErrStudentNotFound
	}
	if student.ParentID != parentID {
		return nil, domain.ErrStudentOwnership
	}
	class, err := u.classRepo.GetByID(ctx, classID)
	if err != nil {
		return nil, domain.ErrClassNotFound
	}
	if !class.IsPublished || class.EnrollmentStatus != "open" {
		return nil, domain.ErrClassNotEnrollable
	}
	key := idempotencyKey
	enrollment := &domain.Enrollment{ID: uuid.New(), TenantID: class.TenantID, StudentID: student.ID, ClassID: class.ID, Status: "pending", BillingCycle: req.BillingCycle, IdempotencyKey: &key, PaymentStatus: "pending", GrossAmount: class.Price}
	create := func(txCtx context.Context) error {
		return u.enrollmentRepo.CreateIfCapacityAvailable(txCtx, enrollment)
	}
	if u.txManager != nil {
		if err := u.txManager.WithTransaction(ctx, create); err != nil {
			if lockingRepo, ok := u.enrollmentRepo.(repository.EnrollmentLockingRepository); ok {
				if existing, lookupErr := lockingRepo.GetByIdempotencyKeyAny(ctx, idempotencyKey); lookupErr == nil {
					if existing.StudentID != req.StudentID || existing.ClassID != classID || existing.BillingCycle != req.BillingCycle {
						return nil, domain.ErrIdempotencyConflict
					}
					return u.publicEnrollmentResponse(existing)
				}
			}
			return nil, err
		}
	} else if err := create(ctx); err != nil {
		if lockingRepo, ok := u.enrollmentRepo.(repository.EnrollmentLockingRepository); ok {
			if existing, lookupErr := lockingRepo.GetByIdempotencyKeyAny(ctx, idempotencyKey); lookupErr == nil {
				if existing.StudentID != req.StudentID || existing.ClassID != classID || existing.BillingCycle != req.BillingCycle {
					return nil, domain.ErrIdempotencyConflict
				}
				return u.publicEnrollmentResponse(existing)
			}
		}
		return nil, err
	}
	invoice, err := u.billingClient.GenerateInvoice(ctx, billing.InvoiceRequest{TenantID: class.TenantID, StudentID: student.ID, ClassID: class.ID, EnrollmentID: enrollment.ID, ParentID: parentID, BillingCycle: req.BillingCycle, SubtotalAmount: class.Price, IdempotencyKey: idempotencyKey, Title: class.Name})
	if err != nil {
		return nil, fmt.Errorf("generate enrollment invoice: %w", err)
	}
	enrollment.PaymentTransactionID = &invoice.TransactionID
	enrollment.CheckoutSessionURL = &invoice.CheckoutSessionURL
	enrollment.PaymentStatus = "pending"
	if err := u.enrollmentRepo.Update(ctx, enrollment); err != nil {
		return nil, err
	}
	return u.publicEnrollmentResponse(enrollment)
}

func (u *enrollmentUsecase) publicEnrollmentResponse(enrollment *domain.Enrollment) (*domain.PublicEnrollmentResponse, error) {
	response := &domain.PublicEnrollmentResponse{Enrollment: enrollmentResponse(enrollment), Payment: &domain.PaymentResponse{GrossAmount: enrollment.GrossAmount, Status: enrollment.PaymentStatus}}
	if enrollment.PaymentTransactionID != nil {
		response.Payment.TransactionID = *enrollment.PaymentTransactionID
	}
	if enrollment.CheckoutSessionURL != nil {
		response.Payment.CheckoutSessionURL = *enrollment.CheckoutSessionURL
	}
	return response, nil
}

func (u *enrollmentUsecase) EnrollStudent(ctx context.Context, tenantID uuid.UUID, req *domain.EnrollStudentRequest) (*domain.EnrollmentResponse, error) {
	if req.IdempotencyKey == "" {
		return nil, errors.New("idempotency key is required")
	}
	if lockingRepo, ok := u.enrollmentRepo.(repository.EnrollmentLockingRepository); ok {
		if existing, lookupErr := lockingRepo.GetByIdempotencyKeyForTenant(ctx, tenantID, req.IdempotencyKey); lookupErr == nil {
			if existing.StudentID != req.StudentID || existing.ClassID != req.ClassID || existing.BillingCycle != req.BillingCycle {
				return nil, domain.ErrIdempotencyConflict
			}
			return enrollmentResponse(existing), nil
		} else if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return nil, lookupErr
		}
	}
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
	if !class.IsPublished || class.EnrollmentStatus != "open" {
		return nil, domain.ErrClassNotEnrollable
	}
	key := req.IdempotencyKey
	enrollment := &domain.Enrollment{ID: uuid.New(), TenantID: tenantID, StudentID: req.StudentID, ClassID: req.ClassID, Status: "pending", BillingCycle: req.BillingCycle, IdempotencyKey: &key, GrossAmount: class.Price, PaymentStatus: "pending"}
	create := func(txCtx context.Context) error {
		return u.enrollmentRepo.CreateIfCapacityAvailable(txCtx, enrollment)
	}
	if u.txManager != nil {
		if err := u.txManager.WithTransaction(ctx, create); err != nil {
			return nil, fmt.Errorf("create enrollment: %w", err)
		}
	} else if err := create(ctx); err != nil {
		return nil, fmt.Errorf("create enrollment: %w", err)
	}
	invoice, err := u.billingClient.GenerateInvoice(ctx, billing.InvoiceRequest{TenantID: tenantID, StudentID: req.StudentID, ClassID: req.ClassID, EnrollmentID: enrollment.ID, ParentID: student.ParentID, BillingCycle: req.BillingCycle, SubtotalAmount: class.Price, IdempotencyKey: req.IdempotencyKey, Title: class.Name})
	if err != nil {
		return nil, fmt.Errorf("generate enrollment invoice: %w", err)
	}
	enrollment.PaymentTransactionID = &invoice.TransactionID
	enrollment.CheckoutSessionURL = &invoice.CheckoutSessionURL
	enrollment.PaymentStatus = "pending"
	if err := u.enrollmentRepo.Update(ctx, enrollment); err != nil {
		return nil, fmt.Errorf("save enrollment payment details: %w", err)
	}
	return enrollmentResponse(enrollment), nil
}

func (u *enrollmentUsecase) ActivateEnrollment(ctx context.Context, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error) {
	load := u.enrollmentRepo.GetByID
	if lockingRepo, ok := u.enrollmentRepo.(repository.EnrollmentLockingRepository); ok {
		load = lockingRepo.GetByIDForUpdate
	}
	var enrollment *domain.Enrollment
	var err error
	if u.txManager != nil {
		err = u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
			enrollment, err = load(txCtx, enrollmentID)
			if err != nil || enrollment.Status != "pending" {
				return err
			}
			enrollment.Status = "active"
			enrollment.UpdatedAt = time.Now()
			return u.enrollmentRepo.Update(txCtx, enrollment)
		})
	} else {
		enrollment, err = load(ctx, enrollmentID)
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrEnrollmentNotFound
		}
		return nil, fmt.Errorf("failed to fetch enrollment: %w", err)
	}
	if enrollment.Status == "active" {
		return enrollmentResponse(enrollment), nil
	}
	if enrollment.Status != "pending" {
		return nil, domain.ErrInvalidEnrollmentTransition
	}

	if u.txManager == nil {
		enrollment.Status = "active"
		enrollment.UpdatedAt = time.Now()
		if err := u.enrollmentRepo.Update(ctx, enrollment); err != nil {
			return nil, fmt.Errorf("failed to update enrollment status: %w", err)
		}
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
		BillingCycle: enrollment.BillingCycle,
	}
}
