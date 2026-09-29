package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

type PrivateScheduleRequestUsecase interface {
	Create(context.Context, uuid.UUID, uuid.UUID, *domain.CreatePrivateScheduleRequest) (*domain.PrivateScheduleRequest, error)
	Get(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID) (*domain.PrivateScheduleRequest, error)
	List(context.Context, *uuid.UUID, *uuid.UUID, string) ([]domain.PrivateScheduleRequest, error)
	Reject(context.Context, uuid.UUID, uuid.UUID, *domain.RejectPrivateScheduleRequest) (*domain.PrivateScheduleRequest, error)
	Cancel(context.Context, uuid.UUID, uuid.UUID) (*domain.PrivateScheduleRequest, error)
	DeclineRecommendation(context.Context, uuid.UUID, uuid.UUID) (*domain.PrivateScheduleRequest, error)
	Approve(context.Context, uuid.UUID, uuid.UUID) (*domain.PublicEnrollmentResponse, error)
	AcceptRecommendation(context.Context, uuid.UUID, uuid.UUID) (*domain.PublicEnrollmentResponse, error)
}

type privateScheduleRequestUsecase struct {
	repo        repository.PrivateScheduleRequestRepository
	students    repository.StudentRepository
	classes     repository.ClassRepository
	tx          repository.TransactionManager
	enrollments repository.EnrollmentRepository
	schedules   repository.ScheduleRepository
	sessions    repository.SessionRepository
	billing     billing.Client
}

func NewPrivateScheduleRequestUsecase(repo repository.PrivateScheduleRequestRepository, students repository.StudentRepository, classes repository.ClassRepository, tx repository.TransactionManager, enrollments repository.EnrollmentRepository, schedules repository.ScheduleRepository, sessions repository.SessionRepository, billingClient billing.Client) PrivateScheduleRequestUsecase {
	return &privateScheduleRequestUsecase{repo: repo, students: students, classes: classes, tx: tx, enrollments: enrollments, schedules: schedules, sessions: sessions, billing: billingClient}
}

func validatePrivateSlots(slots []domain.PrivateScheduleSlot) error {
	if len(slots) == 0 || len(slots) > 50 {
		return domain.ErrPrivateRequestSlots
	}
	grouped := make(map[int][][2]time.Time)
	for _, slot := range slots {
		if slot.DayOfWeek < 1 || slot.DayOfWeek > 7 {
			return domain.ErrPrivateRequestSlots
		}
		start, err := time.Parse("15:04:05", slot.StartTime)
		if err != nil || start.Format("15:04:05") != slot.StartTime {
			return domain.ErrPrivateRequestSlots
		}
		end, err := time.Parse("15:04:05", slot.EndTime)
		if err != nil || end.Format("15:04:05") != slot.EndTime || !end.After(start) {
			return domain.ErrPrivateRequestSlots
		}
		grouped[slot.DayOfWeek] = append(grouped[slot.DayOfWeek], [2]time.Time{start, end})
	}
	for _, day := range grouped {
		sort.Slice(day, func(i, j int) bool { return day[i][0].Before(day[j][0]) })
		for i := 1; i < len(day); i++ {
			if day[i][0].Before(day[i-1][1]) {
				return domain.ErrPrivateRequestSlots
			}
		}
	}
	return nil
}

// Approve and AcceptRecommendation share the same locked purchase and billing path.
func (u *privateScheduleRequestUsecase) Approve(ctx context.Context, tenantID, id uuid.UUID) (*domain.PublicEnrollmentResponse, error) {
	return u.purchase(ctx, tenantID, id, false)
}

func (u *privateScheduleRequestUsecase) AcceptRecommendation(ctx context.Context, parentID, id uuid.UUID) (*domain.PublicEnrollmentResponse, error) {
	return u.purchase(ctx, parentID, id, true)
}

// purchase serializes creation on the request row. Billing runs only after the
// transaction commits; retries reuse its stable key and enrollment.
func (u *privateScheduleRequestUsecase) purchase(ctx context.Context, actorID, id uuid.UUID, recommendation bool) (*domain.PublicEnrollmentResponse, error) {
	if actorID == uuid.Nil || id == uuid.Nil {
		return nil, domain.ErrPrivateRequestNotFound
	}
	key := "private-request:" + id.String()
	var request *domain.PrivateScheduleRequest
	var enrollment *domain.Enrollment
	var tenantID uuid.UUID
	prepare := func(txCtx context.Context) error {
		var err error
		if recommendation {
			request, err = u.repo.LockForParent(txCtx, id, actorID)
		} else {
			request, err = u.repo.LockForTenant(txCtx, id, actorID)
		}
		if err != nil {
			return err
		}
		tenantID = request.TenantID
		if recommendation {
			if len(request.RecommendedSlots) == 0 || request.Status != "rejected" && request.Status != "approved" {
				return domain.ErrPrivateRequestTransition
			}
		} else if len(request.RecommendedSlots) > 0 || request.Status != "pending" && request.Status != "approved" {
			return domain.ErrPrivateRequestTransition
		}
		if request.Status == "approved" {
			enrollment, err = u.enrollments.GetByIdempotencyKey(txCtx, request.ParentID, key)
			if err != nil {
				return err
			}
			if enrollment.Status == "dropped" && !platformFeeRejected(enrollment) {
				return domain.ErrPrivateRequestTransition
			}
			return nil
		}
		// A permanent fee rejection dropped the enrollment, but kept its
		// request-derived key and schedules. Retry the same purchase after the
		// pricing policy changes rather than allocating a second enrollment.
		if existing, lookupErr := u.enrollments.GetByIdempotencyKey(txCtx, request.ParentID, key); lookupErr == nil {
			if existing.Status == "pending" && existing.PaymentTransactionID == nil {
				enrollment = existing
				return u.repo.SetStatus(txCtx, id, tenantID, "approved")
			}
			if !platformFeeRejected(existing) {
				return domain.ErrPrivateRequestTransition
			}
			paymentRepo, ok := u.enrollments.(repository.EnrollmentPaymentRepository)
			if !ok {
				return errors.New("payment repository is unavailable")
			}
			if err := paymentRepo.RestoreRejectedEnrollment(txCtx, existing.ID); err != nil {
				return err
			}
			enrollment = existing
			enrollment.Status, enrollment.PaymentStatus = "pending", "pending"
			return u.repo.SetStatus(txCtx, id, tenantID, "approved")
		} else if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return lookupErr
		}
		class, err := u.classes.GetByID(txCtx, request.ClassID)
		if err != nil {
			return err
		}
		if class.TenantID != tenantID {
			return domain.ErrPrivateRequestNotFound
		}
		if class.Type != "private" {
			return domain.ErrPrivateClassRequired
		}
		if !class.IsPublished || class.EnrollmentStatus != "open" {
			return domain.ErrClassNotEnrollable
		}
		student, err := u.students.GetByID(txCtx, request.StudentID)
		if err != nil || student.ParentID != request.ParentID {
			return domain.ErrStudentOwnership
		}
		slots := request.Slots
		if recommendation {
			slots = request.RecommendedSlots
		}
		if err = validatePrivateSlots(slots); err != nil {
			return err
		}
		enrollment = &domain.Enrollment{ID: uuid.New(), TenantID: tenantID, StudentID: request.StudentID, ClassID: request.ClassID, Status: "pending", BillingCycle: request.BillingCycle, IdempotencyKey: &key, PaymentStatus: "pending", GrossAmount: class.Price}
		if err = u.enrollments.CreateIfCapacityAvailable(txCtx, enrollment); err != nil {
			return err
		}
		today := normalizeDate(time.Now())
		generatedUntil := endOfMonth(today)
		for _, slot := range slots {
			schedule := &domain.ClassSchedule{ID: uuid.New(), ClassID: request.ClassID, EnrollmentID: &enrollment.ID, Capacity: 1, DayOfWeek: slot.DayOfWeek, StartTime: slot.StartTime, EndTime: slot.EndTime, ValidFrom: &today, SessionsGeneratedUntil: &generatedUntil}
			if err = u.schedules.Create(txCtx, schedule); err != nil {
				return err
			}
			// Generation uses the existing schedule horizon; the worker continues it.
			if sessions := generateSessionsForSchedule(schedule, today); len(sessions) > 0 {
				if err = u.sessions.BatchCreate(txCtx, sessions); err != nil {
					return err
				}
			}
		}
		return u.repo.SetStatus(txCtx, id, tenantID, "approved")
	}
	if u.tx == nil {
		return nil, errors.New("approval transaction manager is required")
	}
	if err := u.tx.WithTransaction(ctx, prepare); err != nil {
		return nil, err
	}
	if platformFeeRejected(enrollment) {
		return nil, domain.ErrPlatformFeeExceedsGross
	}
	if enrollment.PaymentTransactionID != nil && enrollment.CheckoutSessionURL != nil {
		return (&enrollmentUsecase{}).publicEnrollmentResponse(enrollment)
	}
	// Never hold the request row lock across billing I/O. Billing deduplicates
	// concurrent calls with this key; both callers may safely persist its answer.
	class, err := u.classes.GetByID(ctx, request.ClassID)
	if err != nil {
		return nil, err
	}
	invoice, err := u.billing.GenerateInvoice(ctx, billing.InvoiceRequest{TenantID: tenantID, StudentID: request.StudentID, ClassID: request.ClassID, EnrollmentID: enrollment.ID, ParentID: request.ParentID, BillingCycle: request.BillingCycle, SubtotalAmount: enrollment.GrossAmount, IdempotencyKey: key, Title: class.Name, SenderEmail: request.ParentEmail, PrivateScheduleRequest: true})
	if err != nil {
		failure := (&enrollmentUsecase{enrollmentRepo: u.enrollments, txManager: u.tx}).invoiceFailure(ctx, enrollment.ID, err)
		if errors.Is(failure, domain.ErrPlatformFeeExceedsGross) {
			// No payable invoice exists. Keep the request open for a later attempt
			// after the pricing policy changes, without exposing a false approval.
			resetErr := u.tx.WithTransaction(ctx, func(txCtx context.Context) error {
				var lockErr error
				if recommendation {
					_, lockErr = u.repo.LockForParent(txCtx, id, actorID)
				} else {
					_, lockErr = u.repo.LockForTenant(txCtx, id, actorID)
				}
				if lockErr != nil {
					return lockErr
				}
				current, loadErr := u.enrollments.GetByIdempotencyKey(txCtx, request.ParentID, key)
				if loadErr != nil {
					return loadErr
				}
				if platformFeeRejected(current) {
					status := "pending"
					if recommendation {
						status = "rejected"
					}
					return u.repo.SetStatus(txCtx, id, tenantID, status)
				}
				return nil
			})
			if resetErr != nil {
				return nil, fmt.Errorf("reset rejected approval: %w", resetErr)
			}
		}
		return nil, failure
	}
	if invoice == nil || invoice.TransactionID == uuid.Nil {
		return nil, errors.New("billing returned invalid checkout details")
	}
	checkout, parseErr := url.Parse(invoice.CheckoutSessionURL)
	if parseErr != nil || checkout.Host == "" || checkout.User != nil || checkout.Scheme != "https" && checkout.Scheme != "http" {
		return nil, errors.New("billing returned invalid checkout details")
	}
	// A payment callback can activate the enrollment before this save. Only write
	// the two payment fields, never a stale copy of enrollment.Status.
	paymentRepo, ok := u.enrollments.(repository.EnrollmentPaymentRepository)
	if !ok {
		return nil, errors.New("payment repository is unavailable")
	}
	if err := paymentRepo.UpdatePaymentDetails(ctx, enrollment.ID, invoice.TransactionID, invoice.CheckoutSessionURL); err != nil {
		if !errors.Is(err, domain.ErrInvalidEnrollmentTransition) {
			return nil, err
		}
		// Another same-key approval may have persisted the identical invoice.
		// Never return a stale checkout URL or overwrite a settled state.
		current, loadErr := u.enrollments.GetByIdempotencyKey(ctx, request.ParentID, key)
		if loadErr != nil || current.PaymentTransactionID == nil || *current.PaymentTransactionID != invoice.TransactionID || current.CheckoutSessionURL == nil || *current.CheckoutSessionURL != invoice.CheckoutSessionURL {
			return nil, err
		}
		return (&enrollmentUsecase{}).publicEnrollmentResponse(current)
	}
	enrollment.PaymentTransactionID = &invoice.TransactionID
	enrollment.CheckoutSessionURL = &invoice.CheckoutSessionURL
	return (&enrollmentUsecase{}).publicEnrollmentResponse(enrollment)
}

func (u *privateScheduleRequestUsecase) Create(ctx context.Context, parentID, classID uuid.UUID, req *domain.CreatePrivateScheduleRequest) (*domain.PrivateScheduleRequest, error) {
	if parentID == uuid.Nil {
		return nil, domain.ErrParentRequired
	}
	if req == nil || req.StudentID == uuid.Nil || classID == uuid.Nil {
		return nil, domain.ErrPrivateRequestSlots
	}
	if req.BillingCycle != "monthly" && req.BillingCycle != "quarterly" && req.BillingCycle != "yearly" {
		return nil, errors.New("invalid billing cycle")
	}
	if err := validatePrivateSlots(req.Slots); err != nil {
		return nil, err
	}
	if req.Note != nil && len([]rune(*req.Note)) > 2000 {
		return nil, errors.New("note is too long")
	}
	student, err := u.students.GetByID(ctx, req.StudentID)
	if err != nil {
		if errors.Is(err, domain.ErrStudentNotFound) {
			return nil, domain.ErrStudentOwnership
		}
		return nil, err
	}
	if student.ParentID != parentID {
		return nil, domain.ErrStudentOwnership
	}
	class, err := u.classes.GetByID(ctx, classID)
	if err != nil {
		if errors.Is(err, domain.ErrClassNotFound) {
			return nil, err
		}
		return nil, err
	}
	if class.Type != "private" {
		return nil, domain.ErrPrivateClassRequired
	}
	if !class.IsPublished || class.EnrollmentStatus != "open" {
		return nil, domain.ErrClassNotEnrollable
	}
	request := &domain.PrivateScheduleRequest{ID: uuid.New(), TenantID: class.TenantID, ClassID: classID, StudentID: req.StudentID, ParentID: parentID, ParentEmail: strings.TrimSpace(req.ParentEmail), BillingCycle: req.BillingCycle, Slots: req.Slots, Note: req.Note, Status: "pending", CreatedAt: time.Now().UTC()}
	create := func(txCtx context.Context) error { return u.repo.Create(txCtx, request) }
	if u.tx != nil {
		err = u.tx.WithTransaction(ctx, create)
	} else {
		err = create(ctx)
	}
	if err != nil {
		return nil, err
	}
	return request, nil
}

func (u *privateScheduleRequestUsecase) Get(ctx context.Context, id uuid.UUID, tenantID, parentID *uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	if (tenantID == nil) == (parentID == nil) {
		return nil, domain.ErrPrivateRequestNotFound
	}
	return u.repo.Get(ctx, id, tenantID, parentID)
}
func (u *privateScheduleRequestUsecase) List(ctx context.Context, tenantID, parentID *uuid.UUID, status string) ([]domain.PrivateScheduleRequest, error) {
	if (tenantID == nil) == (parentID == nil) {
		return nil, domain.ErrPrivateRequestNotFound
	}
	if status != "" && status != "pending" && status != "approved" && status != "rejected" && status != "cancelled" && status != "declined" {
		return nil, errors.New("invalid request status")
	}
	return u.repo.List(ctx, tenantID, parentID, status)
}
func (u *privateScheduleRequestUsecase) Reject(ctx context.Context, tenantID, id uuid.UUID, req *domain.RejectPrivateScheduleRequest) (*domain.PrivateScheduleRequest, error) {
	if tenantID == uuid.Nil || id == uuid.Nil {
		return nil, domain.ErrPrivateRequestNotFound
	}
	if req == nil {
		req = &domain.RejectPrivateScheduleRequest{}
	}
	reason := req.Reason
	if reason != nil {
		trimmed := strings.TrimSpace(*reason)
		if len([]rune(trimmed)) > 2000 {
			return nil, errors.New("reason is too long")
		}
		reason = &trimmed
	}
	if req.RecommendedSlots != nil {
		if err := validatePrivateSlots(req.RecommendedSlots); err != nil {
			return nil, err
		}
		return u.repo.RejectWithRecommendation(ctx, id, tenantID, reason, req.RecommendedSlots)
	}
	return u.repo.Transition(ctx, id, &tenantID, nil, "rejected", reason)
}

func (u *privateScheduleRequestUsecase) DeclineRecommendation(ctx context.Context, parentID, id uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	if parentID == uuid.Nil || id == uuid.Nil {
		return nil, domain.ErrPrivateRequestNotFound
	}
	return u.repo.DeclineRecommendation(ctx, id, parentID)
}
func (u *privateScheduleRequestUsecase) Cancel(ctx context.Context, parentID, id uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	if parentID == uuid.Nil {
		return nil, domain.ErrParentRequired
	}
	return u.repo.Transition(ctx, id, nil, &parentID, "cancelled", nil)
}
