package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

// KEL-188: UpdateCategory patches only the supplied fields of a tenant-owned
// category and refuses every cross-tenant or invalid write.

type categoryUpdateRepoMock struct {
	category  *domain.Category
	getErr    error
	updateErr error
	saved     *domain.Category
	updateHit bool
}

func (m *categoryUpdateRepoMock) Create(context.Context, *domain.Category) error { return nil }
func (m *categoryUpdateRepoMock) ListByTenant(context.Context, uuid.UUID, domain.ListQuery) ([]domain.Category, int64, error) {
	return nil, 0, nil
}
func (m *categoryUpdateRepoMock) GetByID(context.Context, uuid.UUID) (*domain.Category, error) {
	return m.category, m.getErr
}
func (m *categoryUpdateRepoMock) Update(context.Context, *domain.Category) error { return nil }
func (m *categoryUpdateRepoMock) UpdateByTenant(_ context.Context, category *domain.Category) error {
	m.updateHit = true
	m.saved = category
	return m.updateErr
}
func (m *categoryUpdateRepoMock) CountActiveClasses(context.Context, uuid.UUID) (int64, error) {
	return 0, nil
}
func (m *categoryUpdateRepoMock) DeleteByTenant(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

// errAny marks a case that must fail with any non-nil error.
var errAny = errors.New("any error")

func newCategoryForUpdate(tenantID, categoryID uuid.UUID) *domain.Category {
	return &domain.Category{
		ID:          categoryID,
		TenantID:    tenantID,
		Name:        "Matematika",
		Description: rawMessage(`{"text":"deskripsi lama"}`),
	}
}

func TestUpdateCategoryAppliesOnlySuppliedFields(t *testing.T) {
	tenantID, categoryID := uuid.New(), uuid.New()

	cases := []struct {
		name      string
		request   *domain.UpdateCategoryRequest
		wantName  string
		wantDesc  string
		wantWrite bool
	}{
		{
			name:      "name only",
			request:   &domain.UpdateCategoryRequest{Name: strptr("Fisika Dasar")},
			wantName:  "Fisika Dasar",
			wantDesc:  `{"text":"deskripsi lama"}`,
			wantWrite: true,
		},
		{
			name:      "description only",
			request:   &domain.UpdateCategoryRequest{Description: rawMessage(`{"text":"deskripsi baru"}`)},
			wantName:  "Matematika",
			wantDesc:  `{"text":"deskripsi baru"}`,
			wantWrite: true,
		},
		{
			name:      "empty body leaves everything unchanged",
			request:   &domain.UpdateCategoryRequest{},
			wantName:  "Matematika",
			wantDesc:  `{"text":"deskripsi lama"}`,
			wantWrite: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &categoryUpdateRepoMock{category: newCategoryForUpdate(tenantID, categoryID)}
			uc := NewCategoryUsecase(repo, nil, &updateTxMock{})
			got, err := uc.UpdateCategory(context.Background(), tenantID, categoryID, tc.request)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !repo.updateHit {
				t.Fatal("tenant-scoped write did not run")
			}
			if repo.saved.Name != tc.wantName {
				t.Fatalf("name=%q want %q", repo.saved.Name, tc.wantName)
			}
			if repo.saved.Description == nil || string(*repo.saved.Description) != tc.wantDesc {
				t.Fatalf("description=%v want %s", repo.saved.Description, tc.wantDesc)
			}
			if repo.saved.TenantID != tenantID || repo.saved.ID != categoryID {
				t.Fatalf("write lost its scope: id=%s tenant=%s", repo.saved.ID, repo.saved.TenantID)
			}
			if got.Name != tc.wantName {
				t.Fatalf("response name=%q want %q", got.Name, tc.wantName)
			}
		})
	}
}

func TestUpdateCategoryRejectsInvalidAndForeignWrites(t *testing.T) {
	tenantID, otherTenant, categoryID := uuid.New(), uuid.New(), uuid.New()

	cases := []struct {
		name    string
		owner   uuid.UUID
		request *domain.UpdateCategoryRequest
		getErr  error
		updErr  error
		wantErr error
	}{
		{
			name:    "blank name",
			owner:   tenantID,
			request: &domain.UpdateCategoryRequest{Name: strptr("   ")},
			wantErr: domain.ErrCategoryNameRequired,
		},
		{
			name:    "foreign tenant category",
			owner:   otherTenant,
			request: &domain.UpdateCategoryRequest{Name: strptr("Bukan Milik Kita")},
			wantErr: domain.ErrCategoryForbidden,
		},
		{
			name:    "category not found",
			owner:   tenantID,
			request: &domain.UpdateCategoryRequest{Name: strptr("X")},
			getErr:  gorm.ErrRecordNotFound,
			wantErr: domain.ErrCategoryNotFound,
		},
		{
			name:    "row vanished before write",
			owner:   tenantID,
			request: &domain.UpdateCategoryRequest{Name: strptr("X")},
			updErr:  gorm.ErrRecordNotFound,
			wantErr: domain.ErrCategoryNotFound,
		},
		{
			name:    "repository failure",
			owner:   tenantID,
			request: &domain.UpdateCategoryRequest{Name: strptr("X")},
			updErr:  errors.New("database unavailable"),
			wantErr: errAny,
		},
		{
			name:    "nil request",
			owner:   tenantID,
			request: nil,
			wantErr: domain.ErrCategoryNameRequired,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &categoryUpdateRepoMock{
				category:  newCategoryForUpdate(tc.owner, categoryID),
				getErr:    tc.getErr,
				updateErr: tc.updErr,
			}
			uc := NewCategoryUsecase(repo, nil, &updateTxMock{})
			got, err := uc.UpdateCategory(context.Background(), tenantID, categoryID, tc.request)
			if tc.wantErr == errAny {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error=%v want=%v", err, tc.wantErr)
			}
			if got != nil {
				t.Fatalf("response=%v want nil", got)
			}
			// A rejected-before-write request must never reach the write. The
			// vanished-row case reaches it (and gets RecordNotFound back), so
			// the assertion excludes it.
			if repo.updateHit && tc.updErr == nil {
				t.Fatal("rejected request reached the tenant-scoped write")
			}
		})
	}
}
