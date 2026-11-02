package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

func TestPrivateSlotLimitPayloads(t *testing.T) {
	for _, raw := range []string{`0`, `-1`, `1.5`, `"2"`, `2147483648`} {
		for _, kind := range []string{"simple", "composite", "update"} {
			t.Run(kind+raw, func(t *testing.T) {
				var limit *int
				var err error
				switch kind {
				case "simple":
					var req domain.CreateClassRequest
					err = json.Unmarshal([]byte(`{"private_slot_limit":`+raw+`}`), &req)
					limit = req.PrivateSlotLimit
				case "composite":
					var req domain.CreateClassWithCategoryRequest
					err = json.Unmarshal([]byte(`{"class":{"private_slot_limit":`+raw+`}}`), &req)
					limit = req.Class.PrivateSlotLimit
				case "update":
					var req domain.UpdateClassRequest
					err = json.Unmarshal([]byte(`{"private_slot_limit":`+raw+`}`), &req)
					limit = req.PrivateSlotLimit
				}
				if err == nil && domain.ValidatePrivateSlotLimit("private", limit) == nil {
					t.Fatal("invalid value accepted")
				}
			})
		}
	}
	for _, value := range []int{1, 3, 2147483647} {
		if err := domain.ValidatePrivateSlotLimit("private", &value); err != nil {
			t.Fatal(err)
		}
		if err := domain.ValidatePrivateSlotLimit("group", &value); !errors.Is(err, domain.ErrInvalidPrivateSlotLimit) {
			t.Fatal(err)
		}
	}
	if *domain.NewPrivateSlotLimit("private", nil) != 1 || domain.NewPrivateSlotLimit("group", nil) != nil {
		t.Fatal("create defaults")
	}
}

func TestPrivateSlotLimitDecisionAndMaterializedRetry(t *testing.T) {
	for _, recommendation := range []bool{false, true} {
		for _, invoiceError := range []error{nil, errors.New("invoice timeout"), billing.ErrPlatformFeeExceedsGross} {
			t.Run(uuid.NewString(), func(t *testing.T) {
				tenant, parent, student, classID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
				limit := 1
				slots := []domain.PrivateScheduleSlot{{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}, {DayOfWeek: 3, StartTime: "10:00:00", EndTime: "11:00:00"}}
				request := &domain.PrivateScheduleRequest{ID: uuid.New(), TenantID: tenant, ParentID: parent, StudentID: student, ClassID: classID, ParentEmail: "parent@example.test", BillingCycle: "monthly", Slots: slots, Status: "pending"}
				if recommendation {
					request.Status, request.RecommendedSlots = "rejected", slots
				}
				requests, enrollments, schedules, sessions := &approvalRequestRepo{request: request}, &approvalEnrollmentRepo{}, &approvalScheduleRepo{}, &approvalSessionRepo{}
				invoices := &approvalBilling{err: invoiceError, response: billing.InvoiceResponse{TransactionID: uuid.New(), CheckoutSessionURL: "https://pay.example.test/checkout"}}
				class := &domain.Class{ID: classID, TenantID: tenant, Type: "private", PrivateSlotLimit: &limit, IsPublished: true, EnrollmentStatus: "open", Price: 100}
				uc := NewPrivateScheduleRequestUsecase(requests, &marketplaceStudentRepo{student: &domain.Student{ID: student, ParentID: parent}}, &marketplaceClassRepo{class: class}, marketplaceTx{}, enrollments, schedules, sessions, invoices)
				decide := func() error {
					if recommendation {
						_, err := uc.AcceptRecommendation(context.Background(), parent, request.ID)
						return err
					}
					approval := &domain.ApprovePrivateScheduleRequest{SelectedSlotIndexes: []int{0, 1}, ProposalSnapshot: slots, ExpectedPrivateSlotLimit: &limit}
					_, err := uc.Approve(context.Background(), tenant, request.ID, approval)
					return err
				}
				if err := decide(); !errors.Is(err, domain.ErrPrivateSlotLimitExceeded) {
					t.Fatalf("cap: %v", err)
				}
				if enrollments.creates != 0 || len(schedules.schedules) != 0 || len(sessions.sessions) != 0 || invoices.calls != 0 {
					t.Fatal("partial purchase")
				}
				limit = 2
				if err := decide(); invoiceError == nil && err != nil {
					t.Fatal(err)
				}
				if enrollments.creates != 1 || len(schedules.schedules) != 2 || len(sessions.sessions) < 2 {
					t.Fatal("slot/session semantics")
				}
				limit = 1
				invoices.err = nil
				if err := decide(); err != nil {
					t.Fatalf("materialized replay after limit reduction: %v", err)
				}
				if enrollments.creates != 1 || len(schedules.schedules) != 2 {
					t.Fatal("retry changed purchase")
				}
			})
		}
	}
}

func TestPrivateSlotLimitRecommendationCreation(t *testing.T) {
	tenant, classID := uuid.New(), uuid.New()
	limit := 1
	request := &domain.PrivateScheduleRequest{ID: uuid.New(), TenantID: tenant, ClassID: classID, Status: "pending"}
	repo := &requestRepoStub{created: request}
	class := &domain.Class{ID: classID, TenantID: tenant, Type: "private", PrivateSlotLimit: &limit}
	uc := NewPrivateScheduleRequestUsecase(repo, nil, &marketplaceClassRepo{class: class}, marketplaceTx{}, nil, nil, nil, nil)
	slots := []domain.PrivateScheduleSlot{{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}, {DayOfWeek: 2, StartTime: "10:00:00", EndTime: "11:00:00"}}
	if _, err := uc.Reject(context.Background(), tenant, request.ID, &domain.RejectPrivateScheduleRequest{RecommendedSlots: slots}); !errors.Is(err, domain.ErrPrivateSlotLimitExceeded) {
		t.Fatal(err)
	}
	if request.Status != "pending" || request.RecommendedSlots != nil {
		t.Fatal("offer changed")
	}
	limit = 2
	if _, err := uc.Reject(context.Background(), tenant, request.ID, &domain.RejectPrivateScheduleRequest{RecommendedSlots: slots}); err != nil {
		t.Fatal(err)
	}
}
