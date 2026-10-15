package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

func TestKEL133PrivatePaymentMethods(t *testing.T) {
	for _, method := range []string{"", "VC", "VA", "BC", "SP", "NQ", "INVALID"} {
		for _, recommendation := range []bool{false, true} {
			t.Run(method+map[bool]string{false: "/approve", true: "/accept"}[recommendation], func(t *testing.T) {
				tenant, parent, student, classID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
				slot := domain.PrivateScheduleSlot{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}
				r := &domain.PrivateScheduleRequest{ID: uuid.New(), TenantID: tenant, ParentID: parent, StudentID: student, ClassID: classID, BillingCycle: "monthly", PaymentMethod: method, Status: "pending", Slots: []domain.PrivateScheduleSlot{slot}}
				if recommendation {
					r.Status = "rejected"
					r.RecommendedSlots = []domain.PrivateScheduleSlot{slot}
				}
				er, sr := &approvalEnrollmentRepo{}, &approvalScheduleRepo{}
				b := &approvalBilling{err: errors.New("provider timeout"), response: billing.InvoiceResponse{TransactionID: uuid.New(), CheckoutSessionURL: "https://pay.example.test/invoice"}}
				u := NewPrivateScheduleRequestUsecase(&approvalRequestRepo{request: r}, &marketplaceStudentRepo{student: &domain.Student{ID: student, ParentID: parent}}, &marketplaceClassRepo{class: &domain.Class{ID: classID, TenantID: tenant, Type: "private", IsPublished: true, EnrollmentStatus: "open", Price: 100}}, marketplaceTx{}, er, sr, &approvalSessionRepo{}, b)
				purchase := func() (*domain.PublicEnrollmentResponse, error) {
					if recommendation {
						return u.AcceptRecommendation(context.Background(), parent, r.ID)
					}
					return u.Approve(context.Background(), tenant, r.ID)
				}
				_, err := purchase()
				if method == "INVALID" {
					if !errors.Is(err, domain.ErrInvalidPaymentMethod) || er.creates != 0 || b.calls != 0 || len(sr.schedules) != 0 {
						t.Fatalf("invalid method side effects: %v", err)
					}
					return
				}
				want := method
				if want == "" {
					want = "VC"
				}
				if err == nil || b.request.PaymentMethod != want {
					t.Fatalf("timeout: %v method=%s", err, b.request.PaymentMethod)
				}
				b.err = nil
				result, err := purchase()
				if err != nil || result == nil || b.request.PaymentMethod != want {
					t.Fatalf("retry: %v method=%s", err, b.request.PaymentMethod)
				}
				calls := b.calls
				again, err := purchase()
				if err != nil || again.Payment.TransactionID != result.Payment.TransactionID || b.calls != calls || er.creates != 1 || len(sr.schedules) != 1 {
					t.Fatalf("replay: %v creates=%d schedules=%d", err, er.creates, len(sr.schedules))
				}
			})
		}
	}
}

func TestKEL133InvalidCreateBeforeDependencies(t *testing.T) {
	u := &privateScheduleRequestUsecase{}
	_, err := u.Create(context.Background(), uuid.New(), uuid.New(), &domain.CreatePrivateScheduleRequest{StudentID: uuid.New(), PaymentMethod: "INVALID"})
	if !errors.Is(err, domain.ErrInvalidPaymentMethod) {
		t.Fatalf("got %v", err)
	}
}
