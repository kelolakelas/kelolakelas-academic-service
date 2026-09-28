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

func TestPrivateScheduleRequestPostgresIsolationAndTransitions(t *testing.T) {
	dsn := os.Getenv("KEL107_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL107_TEST_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"00000000000000_init_schema.up.sql", "00001790600000_private_schedule_requests.up.sql"} {
		sql, err := os.ReadFile("../../migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(string(sql)).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	repo := NewPrivateScheduleRequestRepository(db)
	tenantA, tenantB, parentA, parentB, studentA, studentB, classA, classB := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	categoryA, categoryB := uuid.New(), uuid.New()
	for _, item := range []struct{ id, tenant uuid.UUID }{{categoryA, tenantA}, {categoryB, tenantB}} {
		if err := db.Exec("INSERT INTO categories(id,tenant_id,name) VALUES(?,?,?)", item.id, item.tenant, "test").Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct{ id, tenant, category uuid.UUID }{{classA, tenantA, categoryA}, {classB, tenantB, categoryB}} {
		if err := db.Exec("INSERT INTO classes(id,tenant_id,category_id,name,type,price,is_published,enrollment_status) VALUES(?,?,?,'test','private',100,true,'open')", item.id, item.tenant, item.category).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct{ id, parent uuid.UUID }{{studentA, parentA}, {studentB, parentB}} {
		if err := db.Exec("INSERT INTO students(id,parent_id,first_name) VALUES(?,?,'test')", item.id, item.parent).Error; err != nil {
			t.Fatal(err)
		}
	}
	newRequest := func(id, tenant, class, student, parent uuid.UUID) *domain.PrivateScheduleRequest {
		return &domain.PrivateScheduleRequest{ID: id, TenantID: tenant, ClassID: class, StudentID: student, ParentID: parent, BillingCycle: "monthly", ParentEmail: "verified@example.test", Slots: []domain.PrivateScheduleSlot{{DayOfWeek: 1, StartTime: "10:00:00", EndTime: "11:00:00"}}, Status: "pending"}
	}
	idA, idB := uuid.New(), uuid.New()
	if err := repo.Create(ctx, newRequest(idA, tenantA, classA, studentA, parentA)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newRequest(idB, tenantB, classB, studentB, parentB)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newRequest(uuid.New(), tenantA, classA, studentA, parentB)); !errors.Is(err, domain.ErrStudentOwnership) {
		t.Fatalf("foreign student=%v", err)
	}
	if err := repo.Create(ctx, newRequest(uuid.New(), tenantA, classA, studentA, parentA)); !errors.Is(err, domain.ErrPrivateRequestConflict) {
		t.Fatalf("second pending=%v", err)
	}
	for _, scope := range []struct{ tenant, parent *uuid.UUID }{{&tenantB, nil}, {nil, &parentB}} {
		if _, err := repo.Get(ctx, idA, scope.tenant, scope.parent); !errors.Is(err, domain.ErrPrivateRequestNotFound) {
			t.Fatalf("foreign get=%v", err)
		}
		items, err := repo.List(ctx, scope.tenant, scope.parent, "")
		if err != nil || len(items) != 1 || items[0].ID == idA {
			t.Fatalf("foreign list=%v %+v", err, items)
		}
		if _, err := repo.Transition(ctx, idA, scope.tenant, scope.parent, "rejected", nil); !errors.Is(err, domain.ErrPrivateRequestNotFound) {
			t.Fatalf("foreign transition=%v", err)
		}
	}
	var wg sync.WaitGroup
	out := make(chan error, 2)
	for _, run := range []func() error{
		func() error { _, err := repo.Transition(ctx, idA, &tenantA, nil, "rejected", nil); return err },
		func() error { _, err := repo.Transition(ctx, idA, nil, &parentA, "cancelled", nil); return err },
	} {
		wg.Add(1)
		go func(f func() error) { defer wg.Done(); out <- f() }(run)
	}
	wg.Wait()
	close(out)
	success, conflict := 0, 0
	for err := range out {
		switch {
		case err == nil:
			success++
		case errors.Is(err, domain.ErrPrivateRequestTransition):
			conflict++
		default:
			t.Fatalf("race error=%v", err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("race success=%d conflict=%d", success, conflict)
	}
	if err := repo.Create(ctx, newRequest(uuid.New(), tenantA, classA, studentA, parentA)); err != nil {
		t.Fatalf("resubmit after decision=%v", err)
	}
	enrollment := &domain.Enrollment{ID: uuid.New(), TenantID: tenantB, ClassID: classB, StudentID: studentB, Status: "active", BillingCycle: "monthly"}
	if err := db.Create(enrollment).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newRequest(uuid.New(), tenantB, classB, studentB, parentB)); !errors.Is(err, domain.ErrDuplicateEnrollment) {
		t.Fatalf("active enrollment=%v", err)
	}
}
