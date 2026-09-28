package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type requestRepoStub struct {
	created       *domain.PrivateScheduleRequest
	createErr     error
	transitionErr error
	status        string
}

func (r *requestRepoStub) Create(_ context.Context, v *domain.PrivateScheduleRequest) error {
	r.created = v
	return r.createErr
}
func (r *requestRepoStub) Get(_ context.Context, _ uuid.UUID, _, _ *uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	return r.created, nil
}
func (r *requestRepoStub) List(_ context.Context, _, _ *uuid.UUID, _ string) ([]domain.PrivateScheduleRequest, error) {
	return nil, nil
}
func (r *requestRepoStub) Transition(_ context.Context, _ uuid.UUID, _, _ *uuid.UUID, status string, _ *string) (*domain.PrivateScheduleRequest, error) {
	r.status = status
	return r.created, r.transitionErr
}
func (r *requestRepoStub) RejectWithRecommendation(_ context.Context, _, _ uuid.UUID, reason *string, slots []domain.PrivateScheduleSlot) (*domain.PrivateScheduleRequest, error) {
	r.created.RecommendedSlots = slots
	r.created.RejectionReason = reason
	r.created.Status = "rejected"
	r.status = "rejected"
	return r.created, r.transitionErr
}
func (r *requestRepoStub) DeclineRecommendation(_ context.Context, _, _ uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	r.created.Status = "declined"
	r.status = r.created.Status
	return r.created, r.transitionErr
}
func (r *requestRepoStub) LockForParent(_ context.Context, _, _ uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	return r.created, nil
}
func (r *requestRepoStub) LockForTenant(_ context.Context, _, _ uuid.UUID) (*domain.PrivateScheduleRequest, error) {
	return r.created, nil
}
func (r *requestRepoStub) SetStatus(_ context.Context, _, _ uuid.UUID, status string) error {
	r.status = status
	return nil
}

func TestPrivateScheduleRequestValidationAndTransitions(t *testing.T) {
	parent, studentID, classID := uuid.New(), uuid.New(), uuid.New()
	class := &domain.Class{ID: classID, TenantID: uuid.New(), Type: "private", IsPublished: true, EnrollmentStatus: "open"}
	student := &domain.Student{ID: studentID, ParentID: parent}
	repo := &requestRepoStub{}
	u := NewPrivateScheduleRequestUsecase(repo, &marketplaceStudentRepo{student: student}, &marketplaceClassRepo{class: class}, nil, nil, nil, nil, nil)
	request := func() *domain.CreatePrivateScheduleRequest {
		return &domain.CreatePrivateScheduleRequest{StudentID: studentID, BillingCycle: "monthly", Slots: []domain.PrivateScheduleSlot{{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}}, ParentEmail: "verified@example.test"}
	}
	if _, err := u.Create(context.Background(), parent, classID, request()); err != nil || repo.created.Status != "pending" || repo.created.ParentEmail != "verified@example.test" {
		t.Fatalf("create=%+v error=%v", repo.created, err)
	}
	for name, slots := range map[string][]domain.PrivateScheduleSlot{
		"empty": {}, "reversed": {{DayOfWeek: 1, StartTime: "11:00:00", EndTime: "10:00:00"}},
		"duplicate": {{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}, {DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}},
		"overlap":   {{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}, {DayOfWeek: 1, StartTime: "10:30:00", EndTime: "12:00:00"}},
		"invalid":   {{DayOfWeek: 8, StartTime: "10:00:00", EndTime: "11:00:00"}},
	} {
		t.Run(name, func(t *testing.T) {
			req := request()
			req.Slots = slots
			if _, err := u.Create(context.Background(), parent, classID, req); !errors.Is(err, domain.ErrPrivateRequestSlots) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	if _, err := u.Create(context.Background(), uuid.New(), classID, request()); !errors.Is(err, domain.ErrStudentOwnership) {
		t.Fatalf("foreign parent=%v", err)
	}
	class.Type = "group"
	if _, err := u.Create(context.Background(), parent, classID, request()); !errors.Is(err, domain.ErrPrivateClassRequired) {
		t.Fatalf("group=%v", err)
	}
	class.Type = "private"
	class.IsPublished = false
	if _, err := u.Create(context.Background(), parent, classID, request()); !errors.Is(err, domain.ErrClassNotEnrollable) {
		t.Fatalf("hidden=%v", err)
	}
	class.IsPublished = true
	class.EnrollmentStatus = "closed"
	if _, err := u.Create(context.Background(), parent, classID, request()); !errors.Is(err, domain.ErrClassNotEnrollable) {
		t.Fatalf("closed=%v", err)
	}
	repo.createErr = domain.ErrPrivateRequestConflict
	class.EnrollmentStatus = "open"
	if _, err := u.Create(context.Background(), parent, classID, request()); !errors.Is(err, domain.ErrPrivateRequestConflict) {
		t.Fatalf("duplicate=%v", err)
	}
	repo.createErr = domain.ErrDuplicateEnrollment
	if _, err := u.Create(context.Background(), parent, classID, request()); !errors.Is(err, domain.ErrDuplicateEnrollment) {
		t.Fatalf("enrollment=%v", err)
	}
	repo.createErr = nil
	recommendation := []domain.PrivateScheduleSlot{{DayOfWeek: 2, StartTime: "10:00:00", EndTime: "11:00:00"}}
	for _, slots := range [][]domain.PrivateScheduleSlot{{}, {{DayOfWeek: 2, StartTime: "11:00:00", EndTime: "10:00:00"}}} {
		if _, err := u.Reject(context.Background(), class.TenantID, repo.created.ID, &domain.RejectPrivateScheduleRequest{RecommendedSlots: slots}); !errors.Is(err, domain.ErrPrivateRequestSlots) {
			t.Fatalf("invalid recommendation %v: %v", slots, err)
		}
	}
	reason := " another time "
	if _, err := u.Reject(context.Background(), class.TenantID, repo.created.ID, &domain.RejectPrivateScheduleRequest{Reason: &reason, RecommendedSlots: recommendation}); err != nil || repo.created.Status != "rejected" || repo.created.RejectionReason == nil || *repo.created.RejectionReason != "another time" || len(repo.created.RecommendedSlots) != 1 {
		t.Fatalf("recommendation=%+v error=%v", repo.created, err)
	}
	if _, err := u.DeclineRecommendation(context.Background(), parent, repo.created.ID); err != nil || repo.status != "declined" {
		t.Fatalf("decline=%v status=%s", err, repo.status)
	}
	if _, err := u.Reject(context.Background(), class.TenantID, uuid.New(), nil); err != nil || repo.status != "rejected" {
		t.Fatalf("reject=%v status=%s", err, repo.status)
	}
	if _, err := u.Cancel(context.Background(), parent, uuid.New()); err != nil || repo.status != "cancelled" {
		t.Fatalf("cancel=%v status=%s", err, repo.status)
	}
	repo.transitionErr = domain.ErrPrivateRequestTransition
	if _, err := u.Cancel(context.Background(), parent, uuid.New()); !errors.Is(err, domain.ErrPrivateRequestTransition) {
		t.Fatalf("decided cancel=%v", err)
	}
}

func TestDirectPrivateCheckoutNeverInvoices(t *testing.T) {
	parent, studentID, classID := uuid.New(), uuid.New(), uuid.New()
	class := &domain.Class{ID: classID, TenantID: uuid.New(), Type: "private", IsPublished: true, EnrollmentStatus: "open"}
	billing := &marketplaceBilling{}
	u := NewEnrollmentUsecase(&marketplaceEnrollmentRepo{}, &marketplaceStudentRepo{student: &domain.Student{ID: studentID, ParentID: parent}}, &marketplaceClassRepo{class: class}, billing, marketplaceTx{})
	_, err := u.EnrollPublic(context.Background(), parent, classID, &domain.PublicEnrollmentRequest{StudentID: studentID, BillingCycle: "monthly"}, "new-key")
	if !errors.Is(err, domain.ErrPrivateCheckout) || billing.calls != 0 {
		t.Fatalf("private checkout=%v billing calls=%d", err, billing.calls)
	}
}
