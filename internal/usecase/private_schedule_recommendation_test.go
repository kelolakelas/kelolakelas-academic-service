package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

func TestPrivateRecommendationAcceptanceReplayAndGuards(t *testing.T) {
	tenant, parent, studentID, classID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	requested := domain.PrivateScheduleSlot{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}
	alternative := domain.PrivateScheduleSlot{DayOfWeek: 3, StartTime: "14:00:00", EndTime: "15:00:00"}
	request := &domain.PrivateScheduleRequest{ID: uuid.New(), TenantID: tenant, ParentID: parent, StudentID: studentID, ClassID: classID, ParentEmail: "parent@example.test", BillingCycle: "monthly", Slots: []domain.PrivateScheduleSlot{requested}, RecommendedSlots: []domain.PrivateScheduleSlot{alternative}, Status: "rejected"}
	rr := &approvalRequestRepo{request: request}
	er := &approvalEnrollmentRepo{}
	sr := &approvalScheduleRepo{}
	ss := &approvalSessionRepo{}
	b := &approvalBilling{response: billing.InvoiceResponse{TransactionID: uuid.New(), CheckoutSessionURL: "https://pay.example.test/checkout"}}
	class := &domain.Class{ID: classID, TenantID: tenant, Type: "private", IsPublished: true, EnrollmentStatus: "open", Price: 100}
	u := NewPrivateScheduleRequestUsecase(rr, &marketplaceStudentRepo{student: &domain.Student{ID: studentID, ParentID: parent}}, &marketplaceClassRepo{class: class}, marketplaceTx{}, er, sr, ss, b)
	if _, err := u.AcceptRecommendation(context.Background(), uuid.New(), request.ID); !errors.Is(err, domain.ErrPrivateRequestNotFound) {
		t.Fatalf("foreign parent: %v", err)
	}
	if _, err := u.Approve(context.Background(), tenant, request.ID); !errors.Is(err, domain.ErrPrivateRequestTransition) {
		t.Fatalf("tenant approve recommendation: %v", err)
	}
	request.RecommendedSlots = nil
	if _, err := u.AcceptRecommendation(context.Background(), parent, request.ID); !errors.Is(err, domain.ErrPrivateRequestTransition) {
		t.Fatalf("plain rejection: %v", err)
	}
	request.RecommendedSlots = []domain.PrivateScheduleSlot{alternative}
	request.Status = "declined"
	if _, err := u.AcceptRecommendation(context.Background(), parent, request.ID); !errors.Is(err, domain.ErrPrivateRequestTransition) {
		t.Fatalf("declined recommendation: %v", err)
	}
	request.Status = "rejected"
	class.IsPublished = false
	if _, err := u.AcceptRecommendation(context.Background(), parent, request.ID); !errors.Is(err, domain.ErrClassNotEnrollable) {
		t.Fatalf("unpublished class: %v", err)
	}
	class.IsPublished = true
	result, err := u.AcceptRecommendation(context.Background(), parent, request.ID)
	if err != nil || result == nil || result.Payment.CheckoutSessionURL != b.response.CheckoutSessionURL || request.Status != "approved" {
		t.Fatalf("accept result=%+v err=%v status=%s", result, err, request.Status)
	}
	if er.creates != 1 || len(sr.schedules) != 1 || sr.schedules[0].DayOfWeek != alternative.DayOfWeek || sr.schedules[0].Capacity != 1 || *sr.schedules[0].EnrollmentID != er.enrollment.ID || b.request.IdempotencyKey != "private-request:"+request.ID.String() || b.request.PaymentMethod != "" {
		t.Fatalf("enrollment creates=%d schedules=%+v invoice=%+v", er.creates, sr.schedules, b.request)
	}
	again, err := u.AcceptRecommendation(context.Background(), parent, request.ID)
	if err != nil || again.Enrollment.ID != result.Enrollment.ID || er.creates != 1 || b.calls != 1 || len(sr.schedules) != 1 {
		t.Fatalf("replay=%+v err=%v creates=%d invoices=%d", again, err, er.creates, b.calls)
	}
}

func TestPrivateRecommendationFeeFailureRetainsOffer(t *testing.T) {
	tenant, parent, studentID, classID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	request := &domain.PrivateScheduleRequest{ID: uuid.New(), TenantID: tenant, ParentID: parent, StudentID: studentID, ClassID: classID, ParentEmail: "parent@example.test", BillingCycle: "monthly", Slots: []domain.PrivateScheduleSlot{{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}}, RecommendedSlots: []domain.PrivateScheduleSlot{{DayOfWeek: 2, StartTime: "10:00:00", EndTime: "11:00:00"}}, Status: "rejected"}
	rr := &approvalRequestRepo{request: request}
	er := &approvalEnrollmentRepo{}
	b := &approvalBilling{err: billing.ErrPlatformFeeExceedsGross, response: billing.InvoiceResponse{TransactionID: uuid.New(), CheckoutSessionURL: "https://pay.example.test/checkout"}}
	u := NewPrivateScheduleRequestUsecase(rr, &marketplaceStudentRepo{student: &domain.Student{ID: studentID, ParentID: parent}}, &marketplaceClassRepo{class: &domain.Class{ID: classID, TenantID: tenant, Type: "private", IsPublished: true, EnrollmentStatus: "open", Price: 100}}, marketplaceTx{}, er, &approvalScheduleRepo{}, &approvalSessionRepo{}, b)
	if _, err := u.AcceptRecommendation(context.Background(), parent, request.ID); !errors.Is(err, domain.ErrPlatformFeeExceedsGross) || request.Status != "rejected" || er.enrollment.Status != "dropped" {
		t.Fatalf("fee failure=%v request=%s enrollment=%+v", err, request.Status, er.enrollment)
	}
	b.err = nil
	if _, err := u.AcceptRecommendation(context.Background(), parent, request.ID); err != nil || request.Status != "approved" || er.creates != 1 {
		t.Fatalf("fee retry=%v request=%s creates=%d", err, request.Status, er.creates)
	}
}
