package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

type categoryListClassRepo struct {
	publicationClassRepoMock
	items  []domain.Class
	err    error
	tenant uuid.UUID
}

func (r *categoryListClassRepo) ListByTenant(_ context.Context, tenant uuid.UUID, _ domain.ListQuery) ([]domain.Class, int64, error) {
	r.tenant = tenant
	return r.items, int64(len(r.items)), r.err
}

func TestListClassesCategoryIdentity(t *testing.T) {
	tenant, categoryID := uuid.New(), uuid.New()
	cases := []struct {
		name     string
		category *domain.Category
		want     bool
	}{
		{"same tenant", &domain.Category{ID: categoryID, TenantID: tenant, Name: "Matematika"}, true},
		{"missing or deleted", nil, false},
		{"another tenant", &domain.Category{ID: categoryID, TenantID: uuid.New(), Name: "Private name"}, false},
		{"mismatched reference", &domain.Category{ID: uuid.New(), TenantID: tenant, Name: "Wrong name"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &categoryListClassRepo{items: []domain.Class{{ID: uuid.New(), TenantID: tenant, CategoryID: categoryID, Category: tc.category, Name: "Algebra", Type: "group", Price: 150000, IsPublished: true, EnrollmentStatus: "open"}}}
			result, err := NewClassUsecase(repo, nil, nil, nil, nil, nil).ListClasses(context.Background(), tenant, domain.ListQuery{Page: 1, PageSize: 20})
			if err != nil {
				t.Fatal(err)
			}
			if repo.tenant != tenant || len(result.Items) != 1 || result.Pagination.TotalItems != 1 {
				t.Fatalf("result=%+v tenant=%s", result, repo.tenant)
			}
			row := result.Items[0]
			if row.IsPublished != true || row.EnrollmentStatus != "open" || row.Price != 150000 || row.Type != "group" || row.CategoryID != categoryID {
				t.Fatalf("existing fields changed: %+v", row)
			}
			encoded, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]json.RawMessage
			if err = json.Unmarshal(encoded, &payload); err != nil {
				t.Fatal(err)
			}
			if tc.want {
				if row.Category == nil || row.Category.ID != categoryID || row.Category.Name != "Matematika" {
					t.Fatalf("category=%+v", row.Category)
				}
				var category map[string]interface{}
				if err = json.Unmarshal(payload["category"], &category); err != nil {
					t.Fatal(err)
				}
				if len(category) != 2 {
					t.Fatalf("category exposes extra fields: %s", payload["category"])
				}
			} else if _, ok := payload["category"]; ok {
				t.Fatalf("unexpected category: %s", payload["category"])
			}
		})
	}
}

func TestListClassesCategoryRepositoryFailure(t *testing.T) {
	want := errors.New("category preload failed")
	repo := &categoryListClassRepo{err: want}
	got, err := NewClassUsecase(repo, nil, nil, nil, nil, nil).ListClasses(context.Background(), uuid.New(), domain.ListQuery{Page: 1, PageSize: 20})
	if got != nil || !errors.Is(err, want) {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}
