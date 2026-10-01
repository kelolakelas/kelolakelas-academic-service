package repository

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestReviewOwnershipConcurrencyAndCatalogAggregatePostgres(t *testing.T) {
	dsn := os.Getenv("KEL159_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL159_TEST_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"../../migrations/00000000000000_init_schema.up.sql", "../../migrations/00001790890000_class_reviews.up.sql"} {
		sql, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(string(sql)).Error; err != nil {
			t.Fatal(err)
		}
	}
	parent, other, student, classID, category, tenant, enrollment := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, stmt := range []struct {
		sql  string
		args []interface{}
	}{
		{"INSERT INTO categories (id, tenant_id, name) VALUES (?, ?, 'reviews')", []interface{}{category, tenant}},
		{"INSERT INTO classes (id, tenant_id, category_id, name, type, price, is_published, enrollment_status) VALUES (?, ?, ?, 'reviews', 'group', 100, true, 'open')", []interface{}{classID, tenant, category}},
		{"INSERT INTO students (id, parent_id, first_name) VALUES (?, ?, 'private student')", []interface{}{student, parent}},
		{"INSERT INTO enrollments (id, tenant_id, student_id, class_id, status) VALUES (?, ?, ?, ?, 'active')", []interface{}{enrollment, tenant, student, classID}},
		{"INSERT INTO tenant_location_snapshots (tenant_id, name, is_active, updated_at) VALUES (?, 'tenant', true, now())", []interface{}{tenant}},
	} {
		if err := db.Exec(stmt.sql, stmt.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM class_reviews WHERE class_id = ?", classID)
		db.Exec("DELETE FROM enrollments WHERE id = ?", enrollment)
		db.Exec("DELETE FROM students WHERE id = ?", student)
		db.Exec("DELETE FROM classes WHERE id = ?", classID)
		db.Exec("DELETE FROM categories WHERE id = ?", category)
		db.Exec("DELETE FROM tenant_location_snapshots WHERE tenant_id = ?", tenant)
	})
	repo := NewReviewRepository(db)
	empty, err := NewCatalogRepository(db).GetByID(context.Background(), classID)
	if err != nil || empty.RatingCount != 0 || empty.RatingAverage != nil {
		t.Fatalf("empty aggregate=%+v err=%v", empty, err)
	}
	if err := db.Exec("UPDATE enrollments SET status = 'pending' WHERE id = ?", enrollment).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.Upsert(context.Background(), parent, enrollment, 5, "early"); !errors.Is(err, domain.ErrReviewNotEligible) {
		t.Fatalf("pending enrollment: %v", err)
	}
	if err := db.Exec("UPDATE enrollments SET status = 'active' WHERE id = ?", enrollment).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.Upsert(context.Background(), other, enrollment, 5, "forged"); !errors.Is(err, domain.ErrReviewNotEligible) {
		t.Fatalf("foreign parent: %v", err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, rating := range []int{2, 4} {
		wg.Add(1)
		go func(rating int) {
			defer wg.Done()
			errs <- repo.Upsert(context.Background(), parent, enrollment, rating, "safe")
		}(rating)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := repo.List(context.Background(), classID, 1, 20)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("reviews total=%d items=%v err=%v", total, items, err)
	}
	if err := db.Exec("UPDATE enrollments SET status = 'completed' WHERE id = ?", enrollment).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.Upsert(context.Background(), parent, enrollment, 5, "edited"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Upsert(context.Background(), other, enrollment, 1, "overwrite"); !errors.Is(err, domain.ErrReviewNotEligible) {
		t.Fatalf("foreign edit: %v", err)
	}
	item, err := NewCatalogRepository(db).GetByID(context.Background(), classID)
	if err != nil || item.RatingCount != 1 || item.RatingAverage == nil || *item.RatingAverage != 5 {
		t.Fatalf("aggregate=%+v err=%v", item, err)
	}
	if err := db.Exec("UPDATE enrollments SET status = 'dropped' WHERE id = ?", enrollment).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.Upsert(context.Background(), parent, enrollment, 1, "late"); !errors.Is(err, domain.ErrReviewNotEligible) {
		t.Fatalf("dropped enrollment: %v", err)
	}
	items, total, err = repo.List(context.Background(), classID, 1, 20)
	if err != nil || total != 1 || items[0].Rating != 5 {
		t.Fatalf("historical review: %v %d %v", err, total, items)
	}
}
