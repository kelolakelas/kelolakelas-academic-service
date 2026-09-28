package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

type approvalRequestRepo struct {
	repository.PrivateScheduleRequestRepository
	request *domain.PrivateScheduleRequest
}

func (r *approvalRequestRepo) LockForTenant(_ context.Context, _, tenant uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	if r.request.TenantID != tenant {
		return nil, domain.ErrPrivateRequestNotFound
	}
	return r.request, nil
}
func (r *approvalRequestRepo) SetStatus(_ context.Context, _, _ uuid.UUID, status string) error {
	r.request.Status = status
	return nil
}

type approvalEnrollmentRepo struct {
	repository.EnrollmentRepository
	enrollment        *domain.Enrollment
	creates, payments int
}

func (r *approvalEnrollmentRepo) GetByIdempotencyKey(_ context.Context, _ uuid.UUID, _ string) (*domain.Enrollment, error) {
	if r.enrollment == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return r.enrollment, nil
}
func (r *approvalEnrollmentRepo) CreateIfCapacityAvailable(_ context.Context, e *domain.Enrollment) error {
	r.creates++
	r.enrollment = e
	return nil
}
func (r *approvalEnrollmentRepo) UpdatePaymentDetails(_ context.Context, _, transaction uuid.UUID, url string) error {
	r.payments++
	r.enrollment.PaymentTransactionID = &transaction
	r.enrollment.CheckoutSessionURL = &url
	return nil
}
func (r *approvalEnrollmentRepo) RestoreRejectedEnrollment(_ context.Context, _ uuid.UUID) error {
	r.enrollment.Status = "pending"
	r.enrollment.PaymentStatus = "pending"
	return nil
}
func (r *approvalEnrollmentRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Enrollment, error) {
	return r.enrollment, nil
}
func (r *approvalEnrollmentRepo) Update(_ context.Context, e *domain.Enrollment) error {
	r.enrollment = e
	return nil
}

type approvalScheduleRepo struct {
	repository.ScheduleRepository
	schedules []*domain.ClassSchedule
}

func (r *approvalScheduleRepo) Create(_ context.Context, schedule *domain.ClassSchedule) error {
	r.schedules = append(r.schedules, schedule)
	return nil
}

type approvalSessionRepo struct {
	repository.SessionRepository
	sessions []*domain.ClassSession
}

func (r *approvalSessionRepo) BatchCreate(_ context.Context, sessions []*domain.ClassSession) error {
	r.sessions = append(r.sessions, sessions...)
	return nil
}

type approvalBilling struct {
	calls    int
	err      error
	request  billing.InvoiceRequest
	response billing.InvoiceResponse
}

func (b *approvalBilling) GenerateInvoice(_ context.Context, req billing.InvoiceRequest) (*billing.InvoiceResponse, error) {
	b.calls++
	b.request = req
	if b.err != nil {
		return nil, b.err
	}
	return &b.response, nil
}
func (*approvalBilling) CancelEnrollmentPayment(context.Context, uuid.UUID) (*billing.CancelResponse, error) {
	return nil, nil
}

func TestPrivateApprovalSuccessReplayAndFailures(t *testing.T) {
	tenant, parent, studentID, classID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, tc := range []struct {
		name       string
		firstError error
		wantError  error
	}{
		{name: "success"},
		{name: "billing timeout", firstError: errors.New("timeout")},
		{name: "fee rejection", firstError: billing.ErrPlatformFeeExceedsGross, wantError: domain.ErrPlatformFeeExceedsGross},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := &domain.PrivateScheduleRequest{ID: uuid.New(), TenantID: tenant, ParentID: parent, StudentID: studentID, ClassID: classID, ParentEmail: "parent@example.test", BillingCycle: "monthly", Slots: []domain.PrivateScheduleSlot{{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}, {DayOfWeek: 2, StartTime: "11:00:00", EndTime: "12:00:00"}}, Status: "pending"}
			rr := &approvalRequestRepo{request: request}
			er := &approvalEnrollmentRepo{}
			sr := &approvalScheduleRepo{}
			ss := &approvalSessionRepo{}
			b := &approvalBilling{err: tc.firstError, response: billing.InvoiceResponse{TransactionID: uuid.New(), CheckoutSessionURL: "https://pay.example.test/checkout"}}
			class := &domain.Class{ID: classID, TenantID: tenant, Type: "private", IsPublished: true, EnrollmentStatus: "open", Price: 100}
			u := NewPrivateScheduleRequestUsecase(rr, &marketplaceStudentRepo{student: &domain.Student{ID: studentID, ParentID: parent}}, &marketplaceClassRepo{class: class}, marketplaceTx{}, er, sr, ss, b)
			result, err := u.Approve(context.Background(), tenant, request.ID)
			if tc.firstError != nil {
				if err == nil || tc.wantError != nil && !errors.Is(err, tc.wantError) {
					t.Fatalf("invoice error=%v", err)
				}
				if tc.wantError != nil && (request.Status != "pending" || er.enrollment.Status != "dropped") {
					t.Fatalf("rejection state=%s enrollment=%s", request.Status, er.enrollment.Status)
				}
				b.err = nil
				result, err = u.Approve(context.Background(), tenant, request.ID)
			}
			if err != nil || result == nil || result.Enrollment.ID != er.enrollment.ID || result.Payment.CheckoutSessionURL != b.response.CheckoutSessionURL || request.Status != "approved" {
				t.Fatalf("approve result=%+v error=%v request=%s", result, err, request.Status)
			}
			if er.creates != 1 || len(sr.schedules) != 2 || b.request.SenderEmail != request.ParentEmail || b.request.IdempotencyKey != "private-request:"+request.ID.String() {
				t.Fatalf("creates=%d schedules=%d invoice=%+v", er.creates, len(sr.schedules), b.request)
			}
			for _, s := range sr.schedules {
				if s.Capacity != 1 || *s.EnrollmentID != er.enrollment.ID {
					t.Fatalf("schedule=%+v", s)
				}
			}
			before := b.calls
			again, err := u.Approve(context.Background(), tenant, request.ID)
			if err != nil || again.Enrollment.ID != result.Enrollment.ID || b.calls != before || er.creates != 1 || len(sr.schedules) != 2 {
				t.Fatalf("replay=%+v err=%v invoice calls=%d", again, err, b.calls)
			}
			if _, err := u.Approve(context.Background(), uuid.New(), request.ID); !errors.Is(err, domain.ErrPrivateRequestNotFound) {
				t.Fatalf("foreign tenant=%v", err)
			}
			request.Status = "rejected"
			if _, err := u.Approve(context.Background(), tenant, request.ID); !errors.Is(err, domain.ErrPrivateRequestTransition) {
				t.Fatalf("rejected=%v", err)
			}
		})
	}
}
