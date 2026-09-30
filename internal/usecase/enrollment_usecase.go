package usecase

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
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
	ReleaseEnrollment(ctx context.Context, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error)
	SuspendEnrollment(ctx context.Context, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error)
	ResumeEnrollment(ctx context.Context, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error)
	EndEnrollment(ctx context.Context, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error)
	CancelPendingEnrollment(ctx context.Context, parentID, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error)
	List(ctx context.Context, tenantID, parentID *uuid.UUID, query domain.EnrollmentQuery) (*domain.EnrollmentListResponse, error)
	GetByID(ctx context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID) (*domain.EnrollmentResponse, error)
	AssignSchedule(ctx context.Context, parentID, enrollmentID, scheduleID uuid.UUID) (*domain.EnrollmentResponse, error)
}

func (u *enrollmentUsecase) AssignSchedule(ctx context.Context, parentID, enrollmentID, scheduleID uuid.UUID) (*domain.EnrollmentResponse, error) {
	enrollment, err := u.enrollmentRepo.GetByIDForAccess(ctx, nil, &parentID, enrollmentID)
	if err != nil {
		return nil, err
	}
	assign := func(txCtx context.Context) error {
		return u.enrollmentRepo.AssignSchedule(txCtx, enrollmentID, scheduleID)
	}
	if u.txManager != nil {
		if err := u.txManager.WithTransaction(ctx, assign); err != nil {
			return nil, err
		}
	} else if err := assign(ctx); err != nil {
		return nil, err
	}
	enrollment.ScheduleID = &scheduleID
	return enrollmentResponse(enrollment), nil
}

// CancelPendingEnrollment lets a parent withdraw an unpaid enrollment and gives the
// seat back. The parent scope is applied by the repository, so an enrollment that
// belongs to another parent is indistinguishable from one that does not exist and is
// answered with the same not-found error — the endpoint is not an existence oracle
// for other parents' enrollments.
//
// The billing withdrawal runs before the seat is dropped, and that order is the
// safety property of this method. If the transaction has already settled, billing
// refuses and the enrollment is left untouched, so a seat is never revoked for money
// the parent really paid. The reverse order could drop a seat and only then discover
// the payment, which no retry could undo.
//
// Only a pending enrollment is cancellable. An enrollment that is already `dropped`
// is answered with a conflict like any other finished state: cancellation is defined
// as "this request moved the enrollment out of pending", and a request that changes
// nothing has not cancelled anything. The withdrawal above still runs first, so an
// invoice that was paid after the seat was released is refused by billing and the
// parent is never told a paid hold was cancelled.
func (u *enrollmentUsecase) CancelPendingEnrollment(ctx context.Context, parentID, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error) {
	if parentID == uuid.Nil {
		return nil, domain.ErrParentRequired
	}
	visible, err := u.enrollmentRepo.GetByIDForAccess(ctx, nil, &parentID, enrollmentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrEnrollmentNotFound
		}
		return nil, fmt.Errorf("failed to fetch enrollment: %w", err)
	}
	// An enrollment that already started cannot be withdrawn by the parent. This is
	// checked before the withdrawal so an active enrollment never has its invoice
	// torn down. A suspended enrollment (KEL-149) also already started: it was
	// active before billing parked it, so the parent cannot cancel it either and
	// its invoice must stay untouched while it is parked.
	if visible.Status == domain.EnrollmentStatusActive || visible.Status == domain.EnrollmentStatusSuspended || visible.Status == domain.EnrollmentStatusCompleted {
		return nil, domain.ErrInvalidEnrollmentTransition
	}
	// The withdrawal always runs, even for an enrollment that is already `dropped`.
	// A dropped enrollment can still hold a payable invoice, and if that invoice was
	// paid in the meantime billing must refuse rather than let the parent believe the
	// seat was cancelled while payment was actually taken.
	if err := u.withdrawEnrollmentPayment(ctx, visible); err != nil {
		return nil, err
	}

	load := u.enrollmentRepo.GetByID
	if lockingRepo, ok := u.enrollmentRepo.(repository.EnrollmentLockingRepository); ok {
		load = lockingRepo.GetByIDForUpdate
	}
	var enrollment *domain.Enrollment
	if u.txManager != nil {
		err = u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
			enrollment, err = load(txCtx, enrollmentID)
			if err != nil || enrollment.Status != "pending" {
				return err
			}
			enrollment.Status = "dropped"
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
	if enrollment.Status != "pending" {
		return nil, domain.ErrInvalidEnrollmentTransition
	}
	if u.txManager == nil {
		enrollment.Status = "dropped"
		enrollment.UpdatedAt = time.Now()
		if err := u.enrollmentRepo.Update(ctx, enrollment); err != nil {
			return nil, fmt.Errorf("failed to update enrollment status: %w", err)
		}
	}
	return enrollmentResponse(enrollment), nil
}

// withdrawEnrollmentPayment tells billing to stop collecting for an enrollment. A
// `pending` enrollment whose invoice creation never completed has no transaction to
// withdraw, which billing reports as not found: that is not a failure, because there
// is nothing to pay and the seat still has to be freed.
func (u *enrollmentUsecase) withdrawEnrollmentPayment(ctx context.Context, enrollment *domain.Enrollment) error {
	if u.billingClient == nil {
		return nil
	}
	if _, err := u.billingClient.CancelEnrollmentPayment(ctx, enrollment.ID); err != nil {
		if errors.Is(err, billing.ErrTransactionNotFound) {
			return nil
		}
		if errors.Is(err, billing.ErrTransactionNotCancellable) {
			return domain.ErrInvalidEnrollmentTransition
		}
		return fmt.Errorf("withdraw enrollment payment: %w", err)
	}
	return nil
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
	// Calls outside HTTP binding must not reserve a seat for a rejected channel.
	switch req.PaymentMethod {
	case "", "VC", "VA", "BC", "SP", "NQ":
	default:
		return nil, domain.ErrInvalidPaymentMethod
	}
	if parentID == uuid.Nil {
		return nil, domain.ErrParentRequired
	}
	if idempotencyKey == "" {
		return nil, errors.New("idempotency key is required")
	}
	// A matching replay must still reject a private class, including historic
	// enrollments; do not regenerate an invoice for a private enrollment.
	if existing, err := u.enrollmentRepo.GetByIdempotencyKey(ctx, parentID, idempotencyKey); err == nil {
		if existing.ClassID != classID || existing.StudentID != req.StudentID || existing.BillingCycle != req.BillingCycle {
			return nil, domain.ErrIdempotencyConflict
		}
		class, classErr := u.classRepo.GetByID(ctx, classID)
		if classErr != nil {
			return nil, classErr
		}
		if class.Type == "private" {
			return nil, domain.ErrPrivateCheckout
		}
		if platformFeeRejected(existing) {
			return nil, domain.ErrPlatformFeeExceedsGross
		}
		if existing.PaymentTransactionID == nil {
			_, studentErr := u.studentRepo.GetByID(ctx, existing.StudentID)
			class, classErr := u.classRepo.GetByID(ctx, existing.ClassID)
			if studentErr != nil || classErr != nil {
				return nil, fmt.Errorf("recover enrollment dependencies")
			}
			invoice, invoiceErr := u.billingClient.GenerateInvoice(ctx, billing.InvoiceRequest{TenantID: existing.TenantID, StudentID: existing.StudentID, ClassID: existing.ClassID, EnrollmentID: existing.ID, ParentID: parentID, BillingCycle: existing.BillingCycle, SubtotalAmount: class.Price, IdempotencyKey: idempotencyKey, Title: class.Name, SenderEmail: req.SenderEmail, PaymentMethod: req.PaymentMethod})
			if invoiceErr != nil {
				return nil, u.invoiceFailure(ctx, existing.ID, invoiceErr)
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
	checkoutClass, err := u.classRepo.GetByID(ctx, classID)
	if err != nil {
		return nil, err
	}
	if checkoutClass.Type == "private" {
		return nil, domain.ErrPrivateCheckout
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
	if class.Type == "group" && req.ScheduleID == nil {
		return nil, domain.ErrScheduleRequired
	}
	key := idempotencyKey
	enrollment := &domain.Enrollment{ID: uuid.New(), TenantID: class.TenantID, StudentID: student.ID, ClassID: class.ID, ScheduleID: req.ScheduleID, Status: "pending", BillingCycle: req.BillingCycle, IdempotencyKey: &key, PaymentStatus: "pending", GrossAmount: class.Price}
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
					if platformFeeRejected(existing) {
						return nil, domain.ErrPlatformFeeExceedsGross
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
				if platformFeeRejected(existing) {
					return nil, domain.ErrPlatformFeeExceedsGross
				}
				return u.publicEnrollmentResponse(existing)
			}
		}
		return nil, err
	}
	invoice, err := u.billingClient.GenerateInvoice(ctx, billing.InvoiceRequest{TenantID: class.TenantID, StudentID: student.ID, ClassID: class.ID, EnrollmentID: enrollment.ID, ParentID: parentID, BillingCycle: req.BillingCycle, SubtotalAmount: class.Price, IdempotencyKey: idempotencyKey, Title: class.Name, SenderEmail: req.SenderEmail, PaymentMethod: req.PaymentMethod})
	if err != nil {
		return nil, u.invoiceFailure(ctx, enrollment.ID, err)
	}
	enrollment.PaymentTransactionID = &invoice.TransactionID
	enrollment.CheckoutSessionURL = &invoice.CheckoutSessionURL
	enrollment.PaymentStatus = "pending"
	if err := u.enrollmentRepo.Update(ctx, enrollment); err != nil {
		return nil, err
	}
	return u.publicEnrollmentResponse(enrollment)
}

// platformFeeRejected reports whether an enrollment was dropped because billing
// refused its invoice for the platform fee (see invoiceFailure).
func platformFeeRejected(enrollment *domain.Enrollment) bool {
	return enrollment.Status == "dropped" && enrollment.PaymentStatus == domain.PaymentStatusPlatformFeeRejected
}

// invoiceFailure turns a failed invoice request into the error the caller answers.
// Most failures are transient and keep the pending enrollment, so a same-key retry
// can still request the invoice. A platform fee rejection is permanent: billing
// wrote no transaction and would refuse the same invoice again, so the enrollment
// created for the attempt is dropped. It then holds no seat in the capacity count
// and no longer blocks a new enrollment for the same student and class (both count
// only pending and active rows), while its payment status keeps a same-key replay
// answering the rejection instead of a success.
func (u *enrollmentUsecase) invoiceFailure(ctx context.Context, enrollmentID uuid.UUID, invoiceErr error) error {
	if !errors.Is(invoiceErr, billing.ErrPlatformFeeExceedsGross) {
		return fmt.Errorf("generate enrollment invoice: %w", invoiceErr)
	}
	if err := u.dropPlatformFeeRejected(ctx, enrollmentID); err != nil {
		// The rejection stands, but the seat could not be released. Answer an
		// internal error so it is logged; the enrollment still has no transaction,
		// so a same-key retry asks billing again and retries the release.
		return fmt.Errorf("release enrollment after platform fee rejection: %w", err)
	}
	return domain.ErrPlatformFeeExceedsGross
}

// dropPlatformFeeRejected moves the enrollment from pending to dropped under a row
// lock. An enrollment that already left pending is left as it is.
func (u *enrollmentUsecase) dropPlatformFeeRejected(ctx context.Context, enrollmentID uuid.UUID) error {
	load := u.enrollmentRepo.GetByID
	if lockingRepo, ok := u.enrollmentRepo.(repository.EnrollmentLockingRepository); ok {
		load = lockingRepo.GetByIDForUpdate
	}
	drop := func(txCtx context.Context) error {
		enrollment, err := load(txCtx, enrollmentID)
		if err != nil {
			return err
		}
		if enrollment.Status != "pending" {
			return nil
		}
		enrollment.Status = "dropped"
		enrollment.PaymentStatus = domain.PaymentStatusPlatformFeeRejected
		enrollment.UpdatedAt = time.Now()
		return u.enrollmentRepo.Update(txCtx, enrollment)
	}
	if u.txManager != nil {
		return u.txManager.WithTransaction(ctx, drop)
	}
	return drop(ctx)
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
	switch req.PaymentMethod {
	case "", "VC", "VA", "BC", "SP", "NQ":
	default:
		return nil, domain.ErrInvalidPaymentMethod
	}
	if req.IdempotencyKey == "" {
		return nil, errors.New("idempotency key is required")
	}
	if lockingRepo, ok := u.enrollmentRepo.(repository.EnrollmentLockingRepository); ok {
		if existing, lookupErr := lockingRepo.GetByIdempotencyKeyForTenant(ctx, tenantID, req.IdempotencyKey); lookupErr == nil {
			if existing.StudentID != req.StudentID || existing.ClassID != req.ClassID || existing.BillingCycle != req.BillingCycle {
				return nil, domain.ErrIdempotencyConflict
			}
			class, classErr := u.classRepo.GetByID(ctx, req.ClassID)
			if classErr != nil {
				return nil, classErr
			}
			if class.Type == "private" {
				return nil, domain.ErrPrivateCheckout
			}
			if platformFeeRejected(existing) {
				return nil, domain.ErrPlatformFeeExceedsGross
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
	if class.Type == "private" {
		return nil, domain.ErrPrivateCheckout
	}
	if !class.IsPublished || class.EnrollmentStatus != "open" {
		return nil, domain.ErrClassNotEnrollable
	}
	key := req.IdempotencyKey
	enrollment := &domain.Enrollment{ID: uuid.New(), TenantID: tenantID, StudentID: req.StudentID, ClassID: req.ClassID, ScheduleID: req.ScheduleID, Status: "pending", BillingCycle: req.BillingCycle, IdempotencyKey: &key, GrossAmount: class.Price, PaymentStatus: "pending"}
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
	invoice, err := u.billingClient.GenerateInvoice(ctx, billing.InvoiceRequest{TenantID: tenantID, StudentID: req.StudentID, ClassID: req.ClassID, EnrollmentID: enrollment.ID, ParentID: student.ParentID, BillingCycle: req.BillingCycle, SubtotalAmount: class.Price, IdempotencyKey: req.IdempotencyKey, Title: class.Name, PaymentMethod: req.PaymentMethod})
	if err != nil {
		return nil, u.invoiceFailure(ctx, enrollment.ID, err)
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

// ReleaseEnrollment frees the seat held by an enrollment whose payment failed or
// expired. Only a `pending` enrollment is moved to `dropped`, so the seat that the
// capacity check counts is returned to the catalog while an already active
// enrollment is never revoked by a late failure notification.
func (u *enrollmentUsecase) ReleaseEnrollment(ctx context.Context, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error) {
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
			enrollment.Status = "dropped"
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

	// Releasing a seat is terminal but idempotent: repeating the notification is a
	// no-op, and a late failure never revokes an enrollment that is already active.
	if enrollment.Status == "dropped" || enrollment.Status == "active" {
		return enrollmentResponse(enrollment), nil
	}
	if enrollment.Status != "pending" {
		return nil, domain.ErrInvalidEnrollmentTransition
	}

	if u.txManager == nil {
		enrollment.Status = "dropped"
		enrollment.UpdatedAt = time.Now()
		if err := u.enrollmentRepo.Update(ctx, enrollment); err != nil {
			return nil, fmt.Errorf("failed to update enrollment status: %w", err)
		}
	}

	return enrollmentResponse(enrollment), nil
}

// SuspendEnrollment parks an active enrollment (KEL-149): the status moves to
// `suspended`, which every seat-counting predicate excludes, so the schedule
// slot becomes available to other students while the enrollment itself keeps
// its history and can be resumed or ended later. The transition is idempotent
// — repeating it answers the suspended enrollment unchanged — and refuses
// every other starting state, because a pending enrollment has no seat to give
// up and a terminal one cannot be revived into suspension.
func (u *enrollmentUsecase) SuspendEnrollment(ctx context.Context, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error) {
	load := u.enrollmentRepo.GetByID
	if lockingRepo, ok := u.enrollmentRepo.(repository.EnrollmentLockingRepository); ok {
		load = lockingRepo.GetByIDForUpdate
	}
	var enrollment *domain.Enrollment
	var err error
	if u.txManager != nil {
		err = u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
			enrollment, err = load(txCtx, enrollmentID)
			if err != nil || enrollment.Status != domain.EnrollmentStatusActive {
				return err
			}
			enrollment.Status = domain.EnrollmentStatusSuspended
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
	if enrollment.Status == domain.EnrollmentStatusSuspended {
		return enrollmentResponse(enrollment), nil
	}
	if enrollment.Status != domain.EnrollmentStatusActive {
		return nil, domain.ErrInvalidEnrollmentTransition
	}
	if u.txManager == nil {
		enrollment.Status = domain.EnrollmentStatusSuspended
		enrollment.UpdatedAt = time.Now()
		if err := u.enrollmentRepo.Update(ctx, enrollment); err != nil {
			return nil, fmt.Errorf("failed to update enrollment status: %w", err)
		}
	}
	return enrollmentResponse(enrollment), nil
}

// ResumeEnrollment returns a suspended enrollment to `active` by reclaiming the
// seat it gave up (KEL-149). The seat is reclaimed under the enrollment row
// lock and, for schedule-based enrollments, the schedule lock, re-running the
// capacity and duplicate checks a new signup passes. When no seat is available
// the answer is a conflict (ErrScheduleFull or ErrEnrollmentSuspendedConflict)
// and the enrollment stays suspended; repeating the call changes nothing.
func (u *enrollmentUsecase) ResumeEnrollment(ctx context.Context, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error) {
	lockingRepo, ok := u.enrollmentRepo.(repository.EnrollmentLockingRepository)
	if !ok {
		return nil, fmt.Errorf("enrollment repository cannot resume under capacity")
	}
	var enrollment *domain.Enrollment
	var err error
	if u.txManager != nil {
		err = u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
			enrollment, err = lockingRepo.GetByIDForUpdate(txCtx, enrollmentID)
			if err != nil || enrollment.Status != domain.EnrollmentStatusSuspended {
				return err
			}
			return lockingRepo.ResumeUnderCapacity(txCtx, enrollment)
		})
	} else {
		enrollment, err = lockingRepo.GetByIDForUpdate(ctx, enrollmentID)
		if err == nil && enrollment.Status == domain.EnrollmentStatusSuspended {
			if resumeErr := lockingRepo.ResumeUnderCapacity(ctx, enrollment); resumeErr != nil {
				return nil, resumeErr
			}
		}
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrEnrollmentNotFound
		}
		return nil, err
	}
	if enrollment.Status == domain.EnrollmentStatusActive {
		return enrollmentResponse(enrollment), nil
	}
	// The only state that reaches here without an error is an enrollment that
	// was never suspended, so the request cannot resume anything.
	return nil, domain.ErrInvalidEnrollmentTransition
}

// EndEnrollment terminates an active or suspended enrollment (KEL-149) by
// moving it to `dropped`, the same terminal state a parent cancellation or an
// expired payment uses, which frees the seat permanently and lets the student
// enroll again. The transition is idempotent: repeating it answers the dropped
// enrollment unchanged. Every other starting state is refused — a pending
// enrollment must go through the parent cancellation or the payment-failure
// release, which also unwind billing.
func (u *enrollmentUsecase) EndEnrollment(ctx context.Context, enrollmentID uuid.UUID) (*domain.EnrollmentResponse, error) {
	load := u.enrollmentRepo.GetByID
	if lockingRepo, ok := u.enrollmentRepo.(repository.EnrollmentLockingRepository); ok {
		load = lockingRepo.GetByIDForUpdate
	}
	var enrollment *domain.Enrollment
	var err error
	if u.txManager != nil {
		err = u.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
			enrollment, err = load(txCtx, enrollmentID)
			if err != nil || (enrollment.Status != domain.EnrollmentStatusActive && enrollment.Status != domain.EnrollmentStatusSuspended) {
				return err
			}
			enrollment.Status = domain.EnrollmentStatusDropped
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
	if enrollment.Status == domain.EnrollmentStatusDropped {
		return enrollmentResponse(enrollment), nil
	}
	if enrollment.Status != domain.EnrollmentStatusActive && enrollment.Status != domain.EnrollmentStatusSuspended {
		return nil, domain.ErrInvalidEnrollmentTransition
	}
	if u.txManager == nil {
		enrollment.Status = domain.EnrollmentStatusDropped
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
		BillingCycle: enrollment.BillingCycle, ScheduleID: enrollment.ScheduleID,
		Schedule: enrollmentScheduleSummary(enrollment),
	}
}

// enrollmentScheduleSummary maps the preloaded schedule into the response, or
// returns nil when there is nothing trustworthy to show: no schedule on the
// enrollment (private class), a schedule the loader did not return (soft-deleted,
// or a write path that did not preload it), or a schedule whose id or class does
// not match the enrollment. The last check keeps a stale or inconsistent row from
// leaking another class's slot into this enrollment's response.
func enrollmentScheduleSummary(enrollment *domain.Enrollment) *domain.EnrollmentScheduleSummary {
	schedule := enrollment.Schedule
	if enrollment.ScheduleID == nil || schedule == nil {
		return nil
	}
	if schedule.ID != *enrollment.ScheduleID || schedule.ClassID != enrollment.ClassID || schedule.DeletedAt.Valid {
		return nil
	}
	summary := &domain.EnrollmentScheduleSummary{DayOfWeek: schedule.DayOfWeek, StartTime: schedule.StartTime, EndTime: schedule.EndTime}
	if schedule.Location != nil {
		if location := strings.TrimSpace(*schedule.Location); location != "" {
			summary.Location = &location
		}
	}
	return summary
}
