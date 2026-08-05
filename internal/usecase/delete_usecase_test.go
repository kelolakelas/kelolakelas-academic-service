package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/gorm"
)

type deleteTxMock struct {
	callbackErr error
	called      bool
}

func (m *deleteTxMock) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	m.called = true
	if m.callbackErr != nil {
		return m.callbackErr
	}
	return fn(ctx)
}

type deleteCategoryRepoMock struct {
	category   *domain.Category
	getErr     error
	classCount int64
	countErr   error
	deleteErr  error
	deleted    bool
}

func (m *deleteCategoryRepoMock) Create(context.Context, *domain.Category) error { return nil }
func (m *deleteCategoryRepoMock) ListByTenant(context.Context, uuid.UUID, domain.ListQuery) ([]domain.Category, int64, error) {
	return nil, 0, nil
}
func (m *deleteCategoryRepoMock) GetByID(context.Context, uuid.UUID) (*domain.Category, error) {
	return m.category, m.getErr
}
func (m *deleteCategoryRepoMock) Update(context.Context, *domain.Category) error { return nil }
func (m *deleteCategoryRepoMock) CountActiveClasses(context.Context, uuid.UUID) (int64, error) {
	return m.classCount, m.countErr
}
func (m *deleteCategoryRepoMock) DeleteByTenant(context.Context, uuid.UUID, uuid.UUID) error {
	m.deleted = true
	return m.deleteErr
}

type deleteClassRepoMock struct {
	class     *domain.Class
	getErr    error
	deleteErr error
	deleted   bool
}

func (m *deleteClassRepoMock) Create(context.Context, *domain.Class) error { return nil }
func (m *deleteClassRepoMock) ListByTenant(context.Context, uuid.UUID, domain.ListQuery) ([]domain.Class, int64, error) {
	return nil, 0, nil
}
func (m *deleteClassRepoMock) GetByID(context.Context, uuid.UUID) (*domain.Class, error) {
	return m.class, m.getErr
}
func (m *deleteClassRepoMock) UpdatePublicationStatus(context.Context, uuid.UUID, uuid.UUID, bool) (*domain.Class, error) {
	return m.class, m.getErr
}
func (m *deleteClassRepoMock) Update(context.Context, *domain.Class) error { return nil }
func (m *deleteClassRepoMock) DeleteByTenant(context.Context, uuid.UUID, uuid.UUID) error {
	m.deleted = true
	return m.deleteErr
}

type deleteScheduleRepoMock struct {
	schedule  *domain.ClassSchedule
	getErr    error
	deleteErr error
	deleted   bool
	classID   uuid.UUID
}

func (m *deleteScheduleRepoMock) Create(context.Context, *domain.ClassSchedule) error { return nil }
func (m *deleteScheduleRepoMock) ListByTenant(context.Context, uuid.UUID, domain.ListQuery) ([]domain.ClassSchedule, int64, error) {
	return nil, 0, nil
}
func (m *deleteScheduleRepoMock) BatchCreate(context.Context, []*domain.ClassSchedule) error {
	return nil
}
func (m *deleteScheduleRepoMock) GetByID(context.Context, uuid.UUID) (*domain.ClassSchedule, error) {
	return m.schedule, m.getErr
}
func (m *deleteScheduleRepoMock) Update(context.Context, *domain.ClassSchedule) error { return nil }
func (m *deleteScheduleRepoMock) DeleteByTenant(context.Context, uuid.UUID, uuid.UUID) error {
	m.deleted = true
	return m.deleteErr
}
func (m *deleteScheduleRepoMock) DeleteByClass(_ context.Context, _ uuid.UUID, classID uuid.UUID) error {
	m.classID = classID
	m.deleted = true
	return m.deleteErr
}

type deleteSessionRepoMock struct {
	cancelScheduleErr error
	cancelClassErr    error
	cancelScheduleID  uuid.UUID
	cancelClassID     uuid.UUID
}

func (m *deleteSessionRepoMock) Create(context.Context, *domain.ClassSession) error { return nil }
func (m *deleteSessionRepoMock) BatchCreate(context.Context, []*domain.ClassSession) error {
	return nil
}
func (m *deleteSessionRepoMock) GetByID(context.Context, uuid.UUID) (*domain.ClassSession, error) {
	return nil, nil
}
func (m *deleteSessionRepoMock) GetByIDForTenant(context.Context, uuid.UUID, uuid.UUID) (*domain.ClassSession, error) {
	return nil, nil
}
func (m *deleteSessionRepoMock) DeleteByTenant(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (m *deleteSessionRepoMock) FindForAttendance(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) (*domain.ClassSession, error) {
	return nil, nil
}
func (m *deleteSessionRepoMock) IsTutorForSession(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (m *deleteSessionRepoMock) ListByTenant(context.Context, uuid.UUID, domain.SessionQuery) ([]domain.ClassSession, int64, error) {
	return nil, 0, nil
}
func (m *deleteSessionRepoMock) Update(context.Context, *domain.ClassSession) error { return nil }
func (m *deleteSessionRepoMock) CancelFutureSessionsBySchedule(_ context.Context, id uuid.UUID, _ time.Time) error {
	m.cancelScheduleID = id
	return m.cancelScheduleErr
}
func (m *deleteSessionRepoMock) CancelFutureSessionsByClass(_ context.Context, id uuid.UUID, _ time.Time) error {
	m.cancelClassID = id
	return m.cancelClassErr
}
func (m *deleteSessionRepoMock) UpdateFutureSessionsTutor(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) error {
	return nil
}

type deleteEnrollmentRepoMock struct {
	active []*domain.Enrollment
	err    error
}

func (m *deleteEnrollmentRepoMock) Create(context.Context, *domain.Enrollment) error { return nil }
func (m *deleteEnrollmentRepoMock) GetByID(context.Context, uuid.UUID) (*domain.Enrollment, error) {
	return nil, nil
}
func (m *deleteEnrollmentRepoMock) GetByIDForAccess(context.Context, *uuid.UUID, *uuid.UUID, uuid.UUID) (*domain.Enrollment, error) {
	return nil, nil
}
func (m *deleteEnrollmentRepoMock) List(context.Context, *uuid.UUID, *uuid.UUID, domain.EnrollmentQuery) ([]*domain.Enrollment, int64, error) {
	return nil, 0, nil
}
func (m *deleteEnrollmentRepoMock) ExistsActive(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (m *deleteEnrollmentRepoMock) GetByIdempotencyKey(context.Context, uuid.UUID, string) (*domain.Enrollment, error) {
	return nil, gorm.ErrRecordNotFound
}
func (m *deleteEnrollmentRepoMock) CreateIfCapacityAvailable(context.Context, *domain.Enrollment) error {
	return nil
}
func (m *deleteEnrollmentRepoMock) IsTutorForEnrollment(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (m *deleteEnrollmentRepoMock) GetActiveByClassID(context.Context, uuid.UUID) ([]*domain.Enrollment, error) {
	return m.active, m.err
}
func (m *deleteEnrollmentRepoMock) Update(context.Context, *domain.Enrollment) error { return nil }
func (m *deleteEnrollmentRepoMock) Delete(context.Context, uuid.UUID) error          { return nil }

func TestDeleteCategory(t *testing.T) {
	tenantID, categoryID := uuid.New(), uuid.New()
	cases := []struct {
		name       string
		category   *domain.Category
		getErr     error
		classCount int64
		deleteErr  error
		wantErr    error
	}{
		{name: "owned category", category: &domain.Category{ID: categoryID, TenantID: tenantID}},
		{name: "not found", getErr: gorm.ErrRecordNotFound, wantErr: domain.ErrCategoryNotFound},
		{name: "other tenant", category: &domain.Category{ID: categoryID, TenantID: uuid.New()}, wantErr: domain.ErrCategoryForbidden},
		{name: "active classes", category: &domain.Category{ID: categoryID, TenantID: tenantID}, classCount: 1, wantErr: domain.ErrCategoryActiveClasses},
		{name: "transactional failure", category: &domain.Category{ID: categoryID, TenantID: tenantID}, deleteErr: errors.New("rollback"), wantErr: errors.New("rollback")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &deleteCategoryRepoMock{category: tc.category, getErr: tc.getErr, classCount: tc.classCount, deleteErr: tc.deleteErr}
			gotErr := NewCategoryUsecase(repo, nil, &deleteTxMock{}).DeleteCategory(context.Background(), tenantID, categoryID)
			if tc.wantErr != nil && (gotErr == nil || gotErr.Error() != tc.wantErr.Error()) {
				t.Fatalf("error=%v want=%v", gotErr, tc.wantErr)
			}
			if tc.wantErr == nil && gotErr != nil {
				t.Fatalf("unexpected error: %v", gotErr)
			}
			if tc.wantErr == nil && !repo.deleted {
				t.Fatal("expected category to be soft-deleted")
			}
		})
	}
}

func TestDeleteClass(t *testing.T) {
	tenantID, classID := uuid.New(), uuid.New()
	cases := []struct {
		name      string
		class     *domain.Class
		getErr    error
		active    []*domain.Enrollment
		deleteErr error
		cancelErr error
		wantErr   error
	}{
		{name: "owned class", class: &domain.Class{ID: classID, TenantID: tenantID}},
		{name: "not found", getErr: gorm.ErrRecordNotFound, wantErr: domain.ErrClassNotFound},
		{name: "other tenant", class: &domain.Class{ID: classID, TenantID: uuid.New()}, wantErr: domain.ErrClassForbidden},
		{name: "active enrollment", class: &domain.Class{ID: classID, TenantID: tenantID}, active: []*domain.Enrollment{{}}, wantErr: domain.ErrClassActiveEnrollments},
		{name: "rollback on session handling failure", class: &domain.Class{ID: classID, TenantID: tenantID}, cancelErr: errors.New("rollback"), wantErr: errors.New("rollback")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			classRepo := &deleteClassRepoMock{class: tc.class, getErr: tc.getErr, deleteErr: tc.deleteErr}
			scheduleRepo := &deleteScheduleRepoMock{deleteErr: tc.deleteErr}
			sessionRepo := &deleteSessionRepoMock{cancelClassErr: tc.cancelErr}
			enrollmentRepo := &deleteEnrollmentRepoMock{active: tc.active}
			gotErr := NewClassUsecase(classRepo, scheduleRepo, sessionRepo, enrollmentRepo, nil, &deleteTxMock{}).DeleteClass(context.Background(), tenantID, classID)
			if tc.wantErr != nil && (gotErr == nil || gotErr.Error() != tc.wantErr.Error()) {
				t.Fatalf("error=%v want=%v", gotErr, tc.wantErr)
			}
			if tc.wantErr == nil && gotErr != nil {
				t.Fatalf("unexpected error: %v", gotErr)
			}
			if tc.wantErr == nil && sessionRepo.cancelClassID != classID {
				t.Fatal("expected future class sessions to be cancelled")
			}
		})
	}
}

func TestDeleteSchedule(t *testing.T) {
	tenantID, classID, scheduleID := uuid.New(), uuid.New(), uuid.New()
	cases := []struct {
		name      string
		schedule  *domain.ClassSchedule
		getErr    error
		deleteErr error
		cancelErr error
		wantErr   error
	}{
		{name: "owned schedule", schedule: &domain.ClassSchedule{ID: scheduleID, ClassID: classID, Class: &domain.Class{ID: classID, TenantID: tenantID}}},
		{name: "not found", getErr: gorm.ErrRecordNotFound, wantErr: ErrScheduleNotFound},
		{name: "other tenant", schedule: &domain.ClassSchedule{ID: scheduleID, ClassID: classID, Class: &domain.Class{ID: classID, TenantID: uuid.New()}}, wantErr: domain.ErrScheduleForbidden},
		{name: "rollback on future session failure", schedule: &domain.ClassSchedule{ID: scheduleID, ClassID: classID, Class: &domain.Class{ID: classID, TenantID: tenantID}}, cancelErr: errors.New("rollback"), wantErr: errors.New("rollback")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scheduleRepo := &deleteScheduleRepoMock{schedule: tc.schedule, getErr: tc.getErr, deleteErr: tc.deleteErr}
			sessionRepo := &deleteSessionRepoMock{cancelScheduleErr: tc.cancelErr}
			gotErr := NewScheduleUsecase(&deleteTxMock{}, &deleteClassRepoMock{}, scheduleRepo, sessionRepo, &deleteEnrollmentRepoMock{}).DeleteSchedule(context.Background(), tenantID, scheduleID)
			if tc.wantErr != nil && (gotErr == nil || gotErr.Error() != tc.wantErr.Error()) {
				t.Fatalf("error=%v want=%v", gotErr, tc.wantErr)
			}
			if tc.wantErr == nil && gotErr != nil {
				t.Fatalf("unexpected error: %v", gotErr)
			}
			if tc.wantErr == nil && sessionRepo.cancelScheduleID != scheduleID {
				t.Fatal("expected future schedule sessions to be cancelled")
			}
		})
	}
}
