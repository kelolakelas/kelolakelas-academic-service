package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type updateTxMock struct {
	err    error
	called bool
}

func (m *updateTxMock) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	m.called = true
	if m.err != nil {
		return m.err
	}
	return fn(ctx)
}

type updateClassRepoMock struct {
	class     *domain.Class
	getErr    error
	updateErr error
	saved     *domain.Class
	updateHit bool
}

func (m *updateClassRepoMock) Create(context.Context, *domain.Class) error { return nil }
func (m *updateClassRepoMock) ListByTenant(context.Context, uuid.UUID, domain.ListQuery) ([]domain.Class, int64, error) {
	return nil, 0, nil
}
func (m *updateClassRepoMock) GetByID(context.Context, uuid.UUID) (*domain.Class, error) {
	return m.class, m.getErr
}
func (m *updateClassRepoMock) UpdatePublicationStatus(context.Context, uuid.UUID, uuid.UUID, bool) (*domain.Class, error) {
	return nil, nil
}
func (m *updateClassRepoMock) Update(context.Context, *domain.Class) error { return nil }
func (m *updateClassRepoMock) UpdateByTenant(_ context.Context, class *domain.Class) error {
	m.updateHit = true
	m.saved = class
	return m.updateErr
}
func (m *updateClassRepoMock) DeleteByTenant(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

type updateCategoryRepoMock struct {
	category *domain.Category
	getErr   error
	lookups  int
}

func (m *updateCategoryRepoMock) Create(context.Context, *domain.Category) error { return nil }
func (m *updateCategoryRepoMock) ListByTenant(context.Context, uuid.UUID, domain.ListQuery) ([]domain.Category, int64, error) {
	return nil, 0, nil
}
func (m *updateCategoryRepoMock) GetByID(context.Context, uuid.UUID) (*domain.Category, error) {
	m.lookups++
	return m.category, m.getErr
}
func (m *updateCategoryRepoMock) Update(context.Context, *domain.Category) error { return nil }
func (m *updateCategoryRepoMock) CountActiveClasses(context.Context, uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *updateCategoryRepoMock) DeleteByTenant(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func strptr(s string) *string { return &s }
func i64ptr(v int64) *int64   { return &v }

func uuptr(v uuid.UUID) *uuid.UUID {
	return &v
}

// updateEnrollmentRepoStub records whether anything wrote through the
// enrollment repository. A class price update must never reach this port.
type updateEnrollmentRepoStub struct {
	touched     bool
	grossAmount int64
}

func (s *updateEnrollmentRepoStub) Create(context.Context, *domain.Enrollment) error {
	s.touched = true
	return nil
}
func (s *updateEnrollmentRepoStub) GetByID(context.Context, uuid.UUID) (*domain.Enrollment, error) {
	return nil, nil
}
func (s *updateEnrollmentRepoStub) GetByIDForAccess(context.Context, *uuid.UUID, *uuid.UUID, uuid.UUID) (*domain.Enrollment, error) {
	return nil, nil
}
func (s *updateEnrollmentRepoStub) List(context.Context, *uuid.UUID, *uuid.UUID, domain.EnrollmentQuery) ([]*domain.Enrollment, int64, error) {
	return nil, 0, nil
}
func (s *updateEnrollmentRepoStub) ExistsActive(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (s *updateEnrollmentRepoStub) GetByIdempotencyKey(context.Context, uuid.UUID, string) (*domain.Enrollment, error) {
	return nil, nil
}
func (s *updateEnrollmentRepoStub) CreateIfCapacityAvailable(context.Context, *domain.Enrollment) error {
	s.touched = true
	return nil
}
func (s *updateEnrollmentRepoStub) IsTutorForEnrollment(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (s *updateEnrollmentRepoStub) GetActiveByClassID(context.Context, uuid.UUID) ([]*domain.Enrollment, error) {
	return nil, nil
}
func (s *updateEnrollmentRepoStub) GetActiveByScheduleID(context.Context, uuid.UUID, uuid.UUID) ([]*domain.Enrollment, error) {
	return nil, nil
}
func (s *updateEnrollmentRepoStub) TransferSchedule(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *updateEnrollmentRepoStub) AssignSchedule(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *updateEnrollmentRepoStub) Update(context.Context, *domain.Enrollment) error {
	s.touched = true
	return nil
}
func (s *updateEnrollmentRepoStub) Delete(context.Context, uuid.UUID) error {
	s.touched = true
	return nil
}

func rawMessage(s string) *json.RawMessage {
	raw := json.RawMessage(s)
	return &raw
}

// newClassForUpdate builds a class owned by tenantID with the given starting
// price. The class is a "group" class so type-change cases have a real target.
func newClassForUpdate(tenantID, classID, categoryID uuid.UUID, price int64) *domain.Class {
	return &domain.Class{
		ID:               classID,
		TenantID:         tenantID,
		CategoryID:       categoryID,
		Name:             "Algebra Grup",
		Description:      rawMessage(`{"text":"lama"}`),
		Type:             "group",
		Price:            price,
		IsPublished:      true,
		EnrollmentStatus: "open",
	}
}

func runUpdateClass(t *testing.T, classRepo *updateClassRepoMock, categoryRepo *updateCategoryRepoMock, tenantID, classID uuid.UUID, req *domain.UpdateClassRequest) (*domain.ClassResponse, error) {
	t.Helper()
	tx := &updateTxMock{}
	uc := NewClassUsecase(classRepo, nil, nil, nil, nil, tx, categoryRepo)
	return uc.UpdateClass(context.Background(), tenantID, classID, req)
}

func TestUpdateClassAppliesOnlySuppliedFields(t *testing.T) {
	tenantID, classID, categoryID := uuid.New(), uuid.New(), uuid.New()
	newCategoryID := uuid.New()

	cases := []struct {
		name         string
		request      *domain.UpdateClassRequest
		wantName     string
		wantPrice    int64
		wantDesc     string
		wantCategory uuid.UUID
		wantLookup   bool // whether the category repository must be consulted
	}{
		{
			name:         "name only",
			request:      &domain.UpdateClassRequest{Name: strptr("Aljabar Dasar")},
			wantName:     "Aljabar Dasar",
			wantPrice:    150000,
			wantDesc:     `{"text":"lama"}`,
			wantCategory: categoryID,
		},
		{
			name:         "price only",
			request:      &domain.UpdateClassRequest{Price: i64ptr(275000)},
			wantName:     "Algebra Grup",
			wantPrice:    275000,
			wantDesc:     `{"text":"lama"}`,
			wantCategory: categoryID,
		},
		{
			name:         "price to zero",
			request:      &domain.UpdateClassRequest{Price: i64ptr(0)},
			wantName:     "Algebra Grup",
			wantPrice:    0,
			wantDesc:     `{"text":"lama"}`,
			wantCategory: categoryID,
		},
		{
			name:         "description only",
			request:      &domain.UpdateClassRequest{Description: rawMessage(`{"text":"baru"}`)},
			wantName:     "Algebra Grup",
			wantPrice:    150000,
			wantDesc:     `{"text":"baru"}`,
			wantCategory: categoryID,
		},
		{
			name:         "category only",
			request:      &domain.UpdateClassRequest{CategoryID: uuptr(newCategoryID)},
			wantName:     "Algebra Grup",
			wantPrice:    150000,
			wantDesc:     `{"text":"lama"}`,
			wantCategory: newCategoryID,
			wantLookup:   true,
		},
		{
			name: "repeat current type is accepted",
			request: &domain.UpdateClassRequest{
				Type: strptr("group"),
			},
			wantName:     "Algebra Grup",
			wantPrice:    150000,
			wantDesc:     `{"text":"lama"}`,
			wantCategory: categoryID,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolveCategory := tc.request.CategoryID != nil
			classRepo := &updateClassRepoMock{class: newClassForUpdate(tenantID, classID, categoryID, 150000)}
			categoryRepo := &updateCategoryRepoMock{category: &domain.Category{ID: newCategoryID, TenantID: tenantID}}

			got, err := runUpdateClass(t, classRepo, categoryRepo, tenantID, classID, tc.request)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !classRepo.updateHit {
				t.Fatal("repository update was not called")
			}
			if got.Name != tc.wantName || got.Price != tc.wantPrice {
				t.Fatalf("name=%q price=%d", got.Name, got.Price)
			}
			if string(*got.Description) != tc.wantDesc {
				t.Fatalf("description=%s want=%s", string(*got.Description), tc.wantDesc)
			}
			if got.CategoryID != tc.wantCategory {
				t.Fatalf("category=%s want=%s", got.CategoryID, tc.wantCategory)
			}
			if resolveCategory && categoryRepo.lookups == 0 {
				t.Fatal("expected the new category to be validated")
			}
			if !resolveCategory && categoryRepo.lookups != 0 {
				t.Fatalf("unexpected category lookups=%d", categoryRepo.lookups)
			}
		})
	}
}

// TestUpdateClassPriceDoesNotTouchExistingEnrollments documents the contract
// behind a price change: the update path only writes the class row, and an
// enrollment keeps the gross_amount it stored when it was created.
func TestUpdateClassPriceDoesNotTouchExistingEnrollments(t *testing.T) {
	tenantID, classID, categoryID := uuid.New(), uuid.New(), uuid.New()
	classRepo := &updateClassRepoMock{class: newClassForUpdate(tenantID, classID, categoryID, 150000)}
	// An enrollment that was created while the class still cost 150000.
	enrollmentRepo := &updateEnrollmentRepoStub{grossAmount: 150000}

	uc := NewClassUsecase(classRepo, nil, nil, enrollmentRepo, nil, &updateTxMock{}, &updateCategoryRepoMock{})
	if _, err := uc.UpdateClass(context.Background(), tenantID, classID, &domain.UpdateClassRequest{Price: i64ptr(300000)}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if enrollmentRepo.touched {
		t.Fatal("class update must not write to enrollments")
	}
	if classRepo.saved.Price != 300000 {
		t.Fatalf("price=%d want=300000", classRepo.saved.Price)
	}
	// The stored snapshot still reflects the price at enrollment time.
	if enrollmentRepo.grossAmount != 150000 {
		t.Fatalf("enrollment gross_amount=%d want=150000", enrollmentRepo.grossAmount)
	}
}

func TestUpdateClassRejectsInvalidInput(t *testing.T) {
	tenantID, classID, categoryID := uuid.New(), uuid.New(), uuid.New()
	otherTenantID := uuid.New()

	cases := []struct {
		name          string
		class         *domain.Class
		getErr        error
		category      *domain.Category
		categoryErr   error
		request       *domain.UpdateClassRequest
		updateErr     error
		wantErr       error
		wantNoPersist bool
	}{
		{
			name:          "another tenant's class",
			class:         newClassForUpdate(otherTenantID, classID, categoryID, 150000),
			request:       &domain.UpdateClassRequest{Name: strptr("Hijack")},
			wantErr:       domain.ErrClassNotFound,
			wantNoPersist: true,
		},
		{
			name:          "missing class",
			getErr:        gorm.ErrRecordNotFound,
			request:       &domain.UpdateClassRequest{Name: strptr("Hijack")},
			wantErr:       domain.ErrClassNotFound,
			wantNoPersist: true,
		},
		{
			name:          "blank name",
			class:         newClassForUpdate(tenantID, classID, categoryID, 150000),
			request:       &domain.UpdateClassRequest{Name: strptr("   ")},
			wantErr:       domain.ErrClassNameRequired,
			wantNoPersist: true,
		},
		{
			name:          "negative price",
			class:         newClassForUpdate(tenantID, classID, categoryID, 150000),
			request:       &domain.UpdateClassRequest{Price: i64ptr(-1)},
			wantErr:       domain.ErrInvalidClassPrice,
			wantNoPersist: true,
		},
		{
			name:          "type change rejected",
			class:         newClassForUpdate(tenantID, classID, categoryID, 150000),
			request:       &domain.UpdateClassRequest{Type: strptr("private")},
			wantErr:       domain.ErrClassTypeImmutable,
			wantNoPersist: true,
		},
		{
			name:          "category of another tenant",
			class:         newClassForUpdate(tenantID, classID, categoryID, 150000),
			category:      &domain.Category{ID: uuid.New(), TenantID: otherTenantID},
			request:       &domain.UpdateClassRequest{CategoryID: uuptr(uuid.New())},
			wantErr:       domain.ErrCategoryForbidden,
			wantNoPersist: true,
		},
		{
			name:          "soft-deleted category",
			class:         newClassForUpdate(tenantID, classID, categoryID, 150000),
			categoryErr:   gorm.ErrRecordNotFound,
			request:       &domain.UpdateClassRequest{CategoryID: uuptr(uuid.New())},
			wantErr:       domain.ErrCategoryNotFound,
			wantNoPersist: true,
		},
		{
			name:          "repository update failure",
			class:         newClassForUpdate(tenantID, classID, categoryID, 150000),
			request:       &domain.UpdateClassRequest{Name: strptr("Baru")},
			updateErr:     errors.New("database unavailable"),
			wantErr:       errors.New("database unavailable"),
			wantNoPersist: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			classRepo := &updateClassRepoMock{class: tc.class, getErr: tc.getErr, updateErr: tc.updateErr}
			categoryRepo := &updateCategoryRepoMock{category: tc.category, getErr: tc.categoryErr}

			_, err := runUpdateClass(t, classRepo, categoryRepo, tenantID, classID, tc.request)
			if err == nil || err.Error() != tc.wantErr.Error() {
				t.Fatalf("error=%v want=%v", err, tc.wantErr)
			}
			if tc.wantNoPersist && classRepo.updateHit {
				t.Fatal("invalid request must not persist changes")
			}
			if tc.class != nil && tc.wantNoPersist {
				if tc.class.Name != "Algebra Grup" || tc.class.Price != 150000 {
					t.Fatalf("class was mutated in memory: %+v", tc.class)
				}
			}
		})
	}
}

// TestUpdateClassKeepsExistingCategoryWhenUnchanged verifies that repeating the
// current category_id does not require an extra ownership lookup.
func TestUpdateClassKeepsExistingCategoryWhenUnchanged(t *testing.T) {
	tenantID, classID, categoryID := uuid.New(), uuid.New(), uuid.New()
	classRepo := &updateClassRepoMock{class: newClassForUpdate(tenantID, classID, categoryID, 150000)}
	categoryRepo := &updateCategoryRepoMock{}

	got, err := runUpdateClass(t, classRepo, categoryRepo, tenantID, classID, &domain.UpdateClassRequest{CategoryID: uuptr(categoryID)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CategoryID != categoryID {
		t.Fatalf("category=%s want=%s", got.CategoryID, categoryID)
	}
	if categoryRepo.lookups != 0 {
		t.Fatalf("category lookups=%d want=0", categoryRepo.lookups)
	}
}
