package usecase

import (
	"context"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"testing"
)

type studentRepoStub struct{}

func (studentRepoStub) Create(context.Context, *domain.Student) error { return nil }
func (studentRepoStub) GetByID(context.Context, uuid.UUID) (*domain.Student, error) {
	return nil, domain.ErrStudentNotFound
}
func (studentRepoStub) GetByIDForAccess(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID) (*domain.Student, error) {
	return nil, domain.ErrStudentNotFound
}
func (studentRepoStub) List(context.Context, *uuid.UUID, *uuid.UUID, domain.StudentQuery) ([]domain.Student, int64, error) {
	return nil, 0, nil
}
func (studentRepoStub) CountActiveEnrollments(context.Context, uuid.UUID) (int64, error) {
	return 0, nil
}
func (studentRepoStub) Update(context.Context, *domain.Student) error { return nil }
func (studentRepoStub) Delete(context.Context, uuid.UUID) error       { return nil }

func TestStudentUsecaseCreateParentOwnership(t *testing.T) {
	parent := uuid.New()
	other := uuid.New()
	tests := []struct {
		name    string
		tenant  *uuid.UUID
		user    uuid.UUID
		parent  uuid.UUID
		wantErr error
	}{
		{name: "parent owns student", tenant: nil, user: parent, parent: parent, wantErr: nil},
		{name: "parent cannot create for another user", tenant: nil, user: parent, parent: other, wantErr: domain.ErrStudentForbidden},
		{name: "staff can create cross-service parent", tenant: &other, user: parent, parent: other, wantErr: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewStudentUsecase(studentRepoStub{}).Create(context.TODO(), test.tenant, &test.user, &domain.CreateStudentRequest{ParentID: test.parent, FullName: "Student", DateOfBirth: "2015-01-01"})
			if err != test.wantErr {
				t.Fatalf("error=%v want=%v", err, test.wantErr)
			}
		})
	}
}
