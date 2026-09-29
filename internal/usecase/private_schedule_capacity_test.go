package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

// KEL-132: a private-class schedule serves exactly one student, so any capacity
// other than 1 must be rejected at the use case boundary (never silent-clamped),
// while group schedules keep accepting any valid capacity. The stubs below model
// the ownership checks of the real repositories: a class or enrollment is only
// visible to its owning tenant.
type privateCapacityClassRepo struct {
	repository.ClassRepository
	classes map[uuid.UUID]*domain.Class
}

func (m *privateCapacityClassRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Class, error) {
	class, ok := m.classes[id]
	if !ok || class == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return class, nil
}

type privateCapacityEnrollmentRepo struct {
	repository.EnrollmentRepository
	enrollments map[uuid.UUID]*domain.Enrollment
}

func (m *privateCapacityEnrollmentRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Enrollment, error) {
	enrollment, ok := m.enrollments[id]
	if !ok || enrollment == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return enrollment, nil
}

type privateCapacityScheduleRepo struct {
	repository.ScheduleRepository
	created []*domain.ClassSchedule
}

func (m *privateCapacityScheduleRepo) Create(_ context.Context, schedule *domain.ClassSchedule) error {
	m.created = append(m.created, schedule)
	return nil
}

type privateCapacitySessionRepo struct {
	repository.SessionRepository
	batches int
}

func (m *privateCapacitySessionRepo) BatchCreate(_ context.Context, _ []*domain.ClassSession) error {
	m.batches++
	return nil
}

type privateCapacityFixture struct {
	tenant      uuid.UUID
	otherTenant uuid.UUID
	privateID   uuid.UUID
	groupID     uuid.UUID
	enrollment  *domain.Enrollment
	schedules   *privateCapacityScheduleRepo
	sessions    *privateCapacitySessionRepo
	usecase     ScheduleUsecase
}

func newPrivateCapacityFixture() *privateCapacityFixture {
	tenant, otherTenant := uuid.New(), uuid.New()
	privateID, groupID := uuid.New(), uuid.New()
	enrollment := &domain.Enrollment{ID: uuid.New(), TenantID: tenant, StudentID: uuid.New(), ClassID: privateID, Status: "active"}
	schedules := &privateCapacityScheduleRepo{}
	sessions := &privateCapacitySessionRepo{}
	u := NewScheduleUsecase(
		marketplaceTx{},
		&privateCapacityClassRepo{classes: map[uuid.UUID]*domain.Class{
			privateID: {ID: privateID, TenantID: tenant, Type: "private"},
			groupID:   {ID: groupID, TenantID: tenant, Type: "group"},
		}},
		schedules,
		sessions,
		&privateCapacityEnrollmentRepo{enrollments: map[uuid.UUID]*domain.Enrollment{enrollment.ID: enrollment}},
	)
	return &privateCapacityFixture{tenant: tenant, otherTenant: otherTenant, privateID: privateID, groupID: groupID, enrollment: enrollment, schedules: schedules, sessions: sessions, usecase: u}
}

func privateCapacityItem(capacity int, enrollmentID *uuid.UUID) domain.ScheduleItemRequest {
	return domain.ScheduleItemRequest{EnrollmentID: enrollmentID, Capacity: capacity, DayOfWeek: 1, StartTime: "09:00", EndTime: "10:00"}
}

func TestPrivateScheduleCapacityIsRejected(t *testing.T) {
	for _, capacity := range []int{0, 2, 5, 2147483647} {
		t.Run("", func(t *testing.T) {
			f := newPrivateCapacityFixture()
			res, err := f.usecase.CreateInitialSchedules(context.Background(), f.tenant, &domain.CreateInitialSchedulesRequest{
				ClassID:   f.privateID,
				Schedules: []domain.ScheduleItemRequest{privateCapacityItem(capacity, &f.enrollment.ID)},
			})
			if !errors.Is(err, ErrPrivateScheduleCapacity) || res != nil {
				t.Fatalf("res=%+v err=%v want ErrPrivateScheduleCapacity", res, err)
			}
			if len(f.schedules.created) != 0 || f.sessions.batches != 0 {
				t.Fatalf("rejected write persisted: schedules=%d batches=%d", len(f.schedules.created), f.sessions.batches)
			}
		})
	}
}

func TestPrivateScheduleCapacityOneIsAccepted(t *testing.T) {
	f := newPrivateCapacityFixture()
	res, err := f.usecase.CreateInitialSchedules(context.Background(), f.tenant, &domain.CreateInitialSchedulesRequest{
		ClassID:   f.privateID,
		Schedules: []domain.ScheduleItemRequest{privateCapacityItem(1, &f.enrollment.ID)},
	})
	if err != nil || res == nil || len(res.Schedules) != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	stored := f.schedules.created[0]
	if stored.Capacity != 1 || stored.EnrollmentID == nil || *stored.EnrollmentID != f.enrollment.ID {
		t.Fatalf("schedule=%+v", stored)
	}
}

func TestGroupScheduleCapacityAboveOneIsAccepted(t *testing.T) {
	f := newPrivateCapacityFixture()
	res, err := f.usecase.CreateInitialSchedules(context.Background(), f.tenant, &domain.CreateInitialSchedulesRequest{
		ClassID:   f.groupID,
		Schedules: []domain.ScheduleItemRequest{privateCapacityItem(8, nil)},
	})
	if err != nil || res == nil || len(res.Schedules) != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	stored := f.schedules.created[0]
	if stored.Capacity != 8 || stored.EnrollmentID != nil {
		t.Fatalf("schedule=%+v", stored)
	}
}

func TestPrivateScheduleCapacityRejectsForeignTenantResources(t *testing.T) {
	t.Run("foreign class", func(t *testing.T) {
		f := newPrivateCapacityFixture()
		res, err := f.usecase.CreateInitialSchedules(context.Background(), f.otherTenant, &domain.CreateInitialSchedulesRequest{
			ClassID:   f.privateID,
			Schedules: []domain.ScheduleItemRequest{privateCapacityItem(1, &f.enrollment.ID)},
		})
		if !errors.Is(err, domain.ErrClassForbidden) || res != nil {
			t.Fatalf("res=%+v err=%v want ErrClassForbidden", res, err)
		}
		if len(f.schedules.created) != 0 {
			t.Fatalf("foreign class write persisted: schedules=%d", len(f.schedules.created))
		}
	})
	t.Run("foreign enrollment", func(t *testing.T) {
		f := newPrivateCapacityFixture()
		foreign := &domain.Enrollment{ID: uuid.New(), TenantID: f.otherTenant, StudentID: uuid.New(), ClassID: f.privateID, Status: "active"}
		stub := f.usecase.(*scheduleUsecase).enrollmentRepo.(*privateCapacityEnrollmentRepo)
		stub.enrollments[foreign.ID] = foreign
		res, err := f.usecase.CreateInitialSchedules(context.Background(), f.tenant, &domain.CreateInitialSchedulesRequest{
			ClassID:   f.privateID,
			Schedules: []domain.ScheduleItemRequest{privateCapacityItem(1, &foreign.ID)},
		})
		if !errors.Is(err, ErrInvalidEnrollmentTenant) || res != nil {
			t.Fatalf("res=%+v err=%v want ErrInvalidEnrollmentTenant", res, err)
		}
		if len(f.schedules.created) != 0 {
			t.Fatalf("foreign enrollment write persisted: schedules=%d", len(f.schedules.created))
		}
	})
}
