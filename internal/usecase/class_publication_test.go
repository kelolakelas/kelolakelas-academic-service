package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/gorm"
)

type publicationClassRepoMock struct {
	updatedClass *domain.Class
	updateErr    error
	classID      uuid.UUID
	tenantID     uuid.UUID
	isPublished  bool
	updateCalled bool
}

func (m *publicationClassRepoMock) Create(context.Context, *domain.Class) error { return nil }
func (m *publicationClassRepoMock) ListByTenant(context.Context, uuid.UUID, domain.ListQuery) ([]domain.Class, int64, error) {
	return nil, 0, nil
}
func (m *publicationClassRepoMock) GetByID(context.Context, uuid.UUID) (*domain.Class, error) {
	return m.updatedClass, m.updateErr
}
func (m *publicationClassRepoMock) UpdatePublicationStatus(_ context.Context, classID, tenantID uuid.UUID, isPublished bool) (*domain.Class, error) {
	m.classID = classID
	m.tenantID = tenantID
	m.isPublished = isPublished
	m.updateCalled = true
	return m.updatedClass, m.updateErr
}
func (m *publicationClassRepoMock) Update(context.Context, *domain.Class) error { return nil }
func (m *publicationClassRepoMock) DeleteByTenant(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func TestUpdateClassPublication(t *testing.T) {
	tenantID, classID := uuid.New(), uuid.New()
	cases := []struct {
		name      string
		requested bool
		class     *domain.Class
		repoErr   error
		wantErr   error
	}{
		{name: "publish", requested: true, class: &domain.Class{ID: classID, TenantID: tenantID, IsPublished: true, EnrollmentStatus: "closed"}},
		{name: "unpublish", requested: false, class: &domain.Class{ID: classID, TenantID: tenantID, IsPublished: false, EnrollmentStatus: "open"}},
		{name: "another tenant", requested: true, repoErr: gorm.ErrRecordNotFound, wantErr: domain.ErrClassNotFound},
		{name: "nonexistent class", requested: true, repoErr: gorm.ErrRecordNotFound, wantErr: domain.ErrClassNotFound},
		{name: "repository error", requested: true, repoErr: errors.New("database unavailable"), wantErr: errors.New("database unavailable")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &publicationClassRepoMock{updatedClass: tc.class, updateErr: tc.repoErr}
			req := &domain.UpdateClassPublicationRequest{IsPublished: &tc.requested}
			got, err := NewClassUsecase(repo, nil, nil, nil, nil, nil).UpdateClassPublication(context.Background(), tenantID, classID, req)
			if tc.wantErr != nil {
				if err == nil || err.Error() != tc.wantErr.Error() {
					t.Fatalf("error=%v want=%v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !repo.updateCalled || repo.classID != classID || repo.tenantID != tenantID || repo.isPublished != tc.requested {
				t.Fatalf("unexpected repository call: %+v", repo)
			}
			if got.IsPublished != tc.requested || got.EnrollmentStatus != tc.class.EnrollmentStatus {
				t.Fatalf("response=%+v", got)
			}
		})
	}
}
