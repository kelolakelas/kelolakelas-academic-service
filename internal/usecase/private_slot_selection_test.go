package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
)

func TestPrivateSelectedApproval(t *testing.T) {
	for _, scenario := range []struct {
		name                                      string
		indexes                                   []int
		limit                                     int
		stale, foreignTenant, cancelled, bodyless bool
		want                                      error
	}{
		{name: "second of three", indexes: []int{1}, limit: 1},
		{name: "two canonical slots", indexes: []int{2, 0}, limit: 2},
		{name: "empty", limit: 2, want: domain.ErrPrivateSelection},
		{name: "duplicate", indexes: []int{1, 1}, limit: 2, want: domain.ErrPrivateSelection},
		{name: "foreign reference", indexes: []int{3}, limit: 2, want: domain.ErrPrivateSelection},
		{name: "negative", indexes: []int{-1}, limit: 2, want: domain.ErrPrivateSelection},
		{name: "too many", indexes: []int{0, 1, 2}, limit: 2, want: domain.ErrPrivateSlotLimitExceeded},
		{name: "stale", indexes: []int{1}, limit: 1, stale: true, want: domain.ErrPrivateSelectionStale},
		{name: "foreign tenant", indexes: []int{1}, limit: 1, foreignTenant: true, want: domain.ErrPrivateRequestNotFound},
		{name: "cancelled", indexes: []int{1}, limit: 1, cancelled: true, want: domain.ErrPrivateRequestTransition},
		{name: "bounded bodyless", limit: 1, bodyless: true, want: domain.ErrPrivateSelection},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			request := &domain.PrivateScheduleRequest{ID: uuid.New(), TenantID: uuid.New(), ParentID: uuid.New(), StudentID: uuid.New(), ClassID: uuid.New(), ParentEmail: "parent@example.test", BillingCycle: "monthly", Status: "pending", Slots: []domain.PrivateScheduleSlot{{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}, {DayOfWeek: 3, StartTime: "12:00:00", EndTime: "13:00:00"}, {DayOfWeek: 5, StartTime: "14:00:00", EndTime: "15:00:00"}}}
			class := &domain.Class{ID: request.ClassID, TenantID: request.TenantID, Type: "private", PrivateSlotLimit: &scenario.limit, Price: 100000, IsPublished: true, EnrollmentStatus: "open"}
			requests, enrollments, schedules, sessions := &approvalRequestRepo{request: request}, &approvalEnrollmentRepo{}, &approvalScheduleRepo{}, &approvalSessionRepo{}
			invoice := &approvalBilling{err: errors.New("invoice timeout"), response: billing.InvoiceResponse{TransactionID: uuid.New(), CheckoutSessionURL: "https://pay.example.test/selected"}}
			uc := NewPrivateScheduleRequestUsecase(requests, &marketplaceStudentRepo{student: &domain.Student{ID: request.StudentID, ParentID: request.ParentID}}, &marketplaceClassRepo{class: class}, marketplaceTx{}, enrollments, schedules, sessions, invoice)
			approval := &domain.ApprovePrivateScheduleRequest{SelectedSlotIndexes: scenario.indexes, ProposalSnapshot: append([]domain.PrivateScheduleSlot(nil), request.Slots...), ExpectedPrivateSlotLimit: &scenario.limit}
			if scenario.stale {
				approval.ProposalSnapshot[0].StartTime = "09:00:00"
			}
			if scenario.bodyless {
				approval = nil
			}
			actor := request.TenantID
			if scenario.foreignTenant {
				actor = uuid.New()
			}
			if scenario.cancelled {
				request.Status = "cancelled"
			}
			_, err := uc.Approve(context.Background(), actor, request.ID, approval)
			if scenario.want != nil {
				if !errors.Is(err, scenario.want) {
					t.Fatalf("got %v want %v", err, scenario.want)
				}
				if enrollments.creates != 0 || len(schedules.schedules) != 0 || invoice.calls != 0 || len(request.ApprovedSlots) != 0 {
					t.Fatal("invalid approval had side effects")
				}
				return
			}
			if err == nil || len(request.Slots) != 3 || len(request.ApprovedSlots) != len(scenario.indexes) || enrollments.creates != 1 {
				t.Fatal("decision was not persisted before invoice timeout", err)
			}
			for _, schedule := range schedules.schedules {
				found := false
				for _, slot := range request.ApprovedSlots {
					if schedule.DayOfWeek == slot.DayOfWeek && schedule.StartTime == slot.StartTime && schedule.EndTime == slot.EndTime {
						found = true
					}
				}
				if !found {
					t.Fatal("unselected schedule materialized")
				}
			}
			if len(schedules.schedules) != len(scenario.indexes) || len(sessions.sessions) == 0 || enrollments.enrollment.GrossAmount != class.Price || invoice.request.SubtotalAmount != class.Price {
				t.Fatal("subset artifacts or fixed price incorrect")
			}
			different := *approval
			different.SelectedSlotIndexes = []int{0, 1, 2}
			if _, err := uc.Approve(context.Background(), actor, request.ID, &different); !errors.Is(err, domain.ErrPrivateSelectionConflict) {
				t.Fatal("changed replay accepted", err)
			}
			invoice.err = nil
			class.PrivateSlotLimit = nil
			for _, retry := range []*domain.ApprovePrivateScheduleRequest{approval, nil} {
				if _, err := uc.Approve(context.Background(), actor, request.ID, retry); err != nil {
					t.Fatal("recovery failed", err)
				}
			}
			if enrollments.creates != 1 || len(schedules.schedules) != len(scenario.indexes) {
				t.Fatal("recovery duplicated artifacts")
			}
		})
	}
}
