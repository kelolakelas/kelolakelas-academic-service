package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

func newMockStudentRepository(t *testing.T) (*studentRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	return &studentRepository{db: db}, mock, func() { _ = sqlDB.Close() }
}

func TestStudentRepositoryListPropagatesCountDatabaseError(t *testing.T) {
	repo, mock, closeDB := newMockStudentRepository(t)
	defer closeDB()
	tenantID := uuid.New()
	databaseErr := errors.New("database count query failed")
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "students" WHERE (EXISTS (SELECT 1 FROM enrollments e WHERE e.student_id = students.id AND e.tenant_id = $1 AND e.deleted_at IS NULL)) AND "students"."deleted_at" IS NULL`)).
		WithArgs(sqlmock.AnyArg()).WillReturnError(databaseErr)

	_, _, err := repo.List(context.Background(), &tenantID, nil, domain.StudentQuery{Page: 1, PageSize: 20})
	if !errors.Is(err, databaseErr) {
		t.Fatalf("error=%v want=%v", err, databaseErr)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestStudentRepositoryListTenantCountAndRowsHaveNoJoinDuplicates(t *testing.T) {
	repo, mock, closeDB := newMockStudentRepository(t)
	defer closeDB()
	tenantID := uuid.New()
	countSQL := `SELECT count(*) FROM "students" WHERE (EXISTS (SELECT 1 FROM enrollments e WHERE e.student_id = students.id AND e.tenant_id = $1 AND e.deleted_at IS NULL)) AND "students"."deleted_at" IS NULL`
	mock.ExpectQuery(regexp.QuoteMeta(countSQL)).WithArgs(sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	rowsSQL := `SELECT \* FROM "students" WHERE \(EXISTS \(SELECT 1 FROM enrollments e WHERE e.student_id = students.id AND e.tenant_id = \$1 AND e.deleted_at IS NULL\)\) AND "students"."deleted_at" IS NULL ORDER BY students.created_at DESC LIMIT \$2`
	rows := sqlmock.NewRows([]string{"id", "parent_id", "first_name", "last_name", "nickname", "gender", "date_of_birth", "created_at", "updated_at", "deleted_at"})
	rows.AddRow(uuid.New(), uuid.New(), "One", nil, nil, nil, nil, nil, nil, nil)
	rows.AddRow(uuid.New(), uuid.New(), "Two", nil, nil, nil, nil, nil, nil, nil)
	mock.ExpectQuery(rowsSQL).WithArgs(sqlmock.AnyArg(), 20).WillReturnRows(rows)

	students, total, err := repo.List(context.Background(), &tenantID, nil, domain.StudentQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if total != 2 || len(students) != 2 {
		t.Fatalf("total=%d students=%d want total=2 students=2", total, len(students))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}
