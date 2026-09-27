package repository

import (
	"context"
	"database/sql/driver"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

// KEL-70: the parent enrollment list/detail preload each row's schedule so the
// response can carry a schedule summary. These tests pin the generated SQL: the
// parent scope stays in the enrollment query, the schedule preload is ONE query
// keyed only by the schedule ids of the rows that passed that scope (no N+1, no
// schedule outside the caller's rows), and soft-deleted schedules are excluded.

var enrollmentColumns = []string{"id", "tenant_id", "student_id", "class_id", "schedule_id", "status", "billing_cycle", "joined_at", "updated_at", "deleted_at"}

func expectEnrollmentPreloads(mock sqlmock.Sqlmock, classID uuid.UUID, scheduleArgs []driver.Value, scheduleRows *sqlmock.Rows, studentIDs ...driver.Value) {
	// GORM runs preloads in name order: Class, Schedule, Student.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "classes" WHERE "classes"."id" = $1 AND "classes"."deleted_at" IS NULL`)).
		WithArgs(classID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(classID, "Kelas Grup"))
	if scheduleArgs != nil {
		placeholders := make([]string, len(scheduleArgs))
		for i := range scheduleArgs {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
		}
		query := `SELECT * FROM "class_schedules" WHERE "class_schedules"."id" = $1 AND "class_schedules"."deleted_at" IS NULL`
		if len(scheduleArgs) > 1 {
			query = `SELECT * FROM "class_schedules" WHERE "class_schedules"."id" IN (` + strings.Join(placeholders, ",") + `) AND "class_schedules"."deleted_at" IS NULL`
		}
		mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(scheduleArgs...).WillReturnRows(scheduleRows)
	}
	studentQuery := `SELECT * FROM "students" WHERE "students"."id" = $1 AND "students"."deleted_at" IS NULL`
	if len(studentIDs) > 1 {
		studentQuery = `SELECT * FROM "students" WHERE "students"."id" IN ($1,$2) AND "students"."deleted_at" IS NULL`
	}
	mock.ExpectQuery(regexp.QuoteMeta(studentQuery)).WithArgs(studentIDs...).
		WillReturnRows(sqlmock.NewRows([]string{"id", "first_name"}))
}

func TestEnrollmentListParentScopePreloadsOnlyOwnSchedulesInOneQuery(t *testing.T) {
	db, mock := newScopedRepositoryDB(t)
	parentID, tenantID, classID := uuid.New(), uuid.New(), uuid.New()
	scheduleA, scheduleB := uuid.New(), uuid.New()
	studentA, studentB := uuid.New(), uuid.New()
	joined := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "enrollments" JOIN students s ON s.id = enrollments.student_id WHERE s.parent_id = $1 AND "enrollments"."deleted_at" IS NULL`)).
		WithArgs(parentID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT "enrollments"."id","enrollments"."tenant_id","enrollments"."student_id","enrollments"."class_id","enrollments"."schedule_id","enrollments"."status","enrollments"."billing_cycle","enrollments"."idempotency_key","enrollments"."payment_transaction_id","enrollments"."checkout_session_url","enrollments"."payment_status","enrollments"."gross_amount","enrollments"."joined_at","enrollments"."updated_at","enrollments"."deleted_at" FROM "enrollments" JOIN students s ON s.id = enrollments.student_id WHERE s.parent_id = $1 AND "enrollments"."deleted_at" IS NULL ORDER BY enrollments.joined_at DESC LIMIT $2`)).
		WithArgs(parentID, 20).
		WillReturnRows(sqlmock.NewRows(enrollmentColumns).
			AddRow(uuid.New(), tenantID, studentA, classID, scheduleA, "active", "monthly", joined, joined, nil).
			// Two enrollments on the same schedule still produce one id in the IN list.
			AddRow(uuid.New(), tenantID, studentB, classID, scheduleA, "pending", "monthly", joined, joined, nil).
			AddRow(uuid.New(), tenantID, studentB, classID, scheduleB, "active", "monthly", joined, joined, nil))
	expectEnrollmentPreloads(mock, classID, []driver.Value{scheduleA, scheduleB},
		sqlmock.NewRows([]string{"id", "class_id", "capacity", "location", "day_of_week", "start_time", "end_time", "deleted_at"}).
			AddRow(scheduleA, classID, 10, "Ruang A", 1, "16:00:00", "17:30:00", nil),
		// scheduleB is soft-deleted: the deleted_at predicate means the database
		// does not return it, so that enrollment keeps a nil Schedule.
		studentA, studentB)

	items, total, err := (&enrollmentRepository{db: db}).List(context.Background(), nil, &parentID, domain.EnrollmentQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("total=%d items=%d, want 3/3", total, len(items))
	}
	for i, want := range []*uuid.UUID{&scheduleA, &scheduleA, nil} {
		got := items[i].Schedule
		if want == nil {
			if got != nil {
				t.Fatalf("item %d schedule = %+v, want nil (soft-deleted)", i, got)
			}
			continue
		}
		if got == nil || got.ID != *want || got.DayOfWeek != 1 || got.StartTime != "16:00:00" || got.Location == nil || *got.Location != "Ruang A" {
			t.Fatalf("item %d schedule = %+v, want schedule %s", i, got, *want)
		}
	}
}

// A page with no scheduled enrollment (private classes) issues no schedule query.
func TestEnrollmentListWithoutSchedulesSkipsSchedulePreload(t *testing.T) {
	db, mock := newScopedRepositoryDB(t)
	parentID, classID, studentID := uuid.New(), uuid.New(), uuid.New()
	joined := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT count\(\*\) FROM "enrollments" JOIN students s`).WithArgs(parentID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT .* FROM "enrollments" JOIN students s ON s\.id = enrollments\.student_id WHERE s\.parent_id = \$1`).
		WithArgs(parentID, 20).
		WillReturnRows(sqlmock.NewRows(enrollmentColumns).AddRow(uuid.New(), uuid.New(), studentID, classID, nil, "pending", "monthly", joined, joined, nil))
	expectEnrollmentPreloads(mock, classID, nil, nil, studentID)

	items, _, err := (&enrollmentRepository{db: db}).List(context.Background(), nil, &parentID, domain.EnrollmentQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
	if len(items) != 1 || items[0].Schedule != nil || items[0].ScheduleID != nil {
		t.Fatalf("items = %+v, want one private enrollment without schedule", items)
	}
}

// The detail read keeps its tenant/parent predicate in the enrollment query, so a
// row owned by someone else is not found and no schedule is ever read for it.
func TestEnrollmentGetByIDForAccessOtherParentReadsNoSchedule(t *testing.T) {
	db, mock := newScopedRepositoryDB(t)
	parentID, enrollmentID := uuid.New(), uuid.New()

	mock.ExpectQuery(`SELECT .* FROM "enrollments" JOIN students s ON s\.id = enrollments\.student_id WHERE enrollments\.id = \$1 AND s\.parent_id = \$2 AND "enrollments"\."deleted_at" IS NULL ORDER BY "enrollments"\."id" LIMIT \$3`).
		WithArgs(enrollmentID, parentID, 1).
		WillReturnRows(sqlmock.NewRows(enrollmentColumns))

	_, err := (&enrollmentRepository{db: db}).GetByIDForAccess(context.Background(), nil, &parentID, enrollmentID)
	if err == nil {
		t.Fatal("GetByIDForAccess of another parent's enrollment returned no error")
	}
	// Any schedule/student/class preload would be an unexpected query.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestEnrollmentGetByIDForAccessPreloadsSchedule(t *testing.T) {
	db, mock := newScopedRepositoryDB(t)
	tenantID, enrollmentID, classID, scheduleID, studentID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	joined := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT \* FROM "enrollments" WHERE enrollments\.id = \$1 AND enrollments\.tenant_id = \$2 AND "enrollments"\."deleted_at" IS NULL`).
		WithArgs(enrollmentID, tenantID, 1).
		WillReturnRows(sqlmock.NewRows(enrollmentColumns).AddRow(enrollmentID, tenantID, studentID, classID, scheduleID, "active", "monthly", joined, joined, nil))
	expectEnrollmentPreloads(mock, classID, []driver.Value{scheduleID},
		sqlmock.NewRows([]string{"id", "class_id", "capacity", "day_of_week", "start_time", "end_time", "deleted_at"}).
			AddRow(scheduleID, classID, 5, 6, "07:30:00", "09:00:00", nil),
		studentID)

	item, err := (&enrollmentRepository{db: db}).GetByIDForAccess(context.Background(), &tenantID, nil, enrollmentID)
	if err != nil {
		t.Fatalf("GetByIDForAccess: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
	if item.Schedule == nil || item.Schedule.ID != scheduleID || item.Schedule.DayOfWeek != 6 || item.Schedule.EndTime != "09:00:00" {
		t.Fatalf("schedule = %+v, want Sabtu 07:30-09:00", item.Schedule)
	}
}
