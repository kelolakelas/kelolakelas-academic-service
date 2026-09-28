package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type containsSQL []string

func (parts containsSQL) Match(expected, sql string) error {
	if !strings.Contains(sql, expected) {
		return fmt.Errorf("SQL missing %q: %s", expected, sql)
	}
	for _, part := range parts {
		if !strings.Contains(sql, part) {
			return fmt.Errorf("SQL missing %q: %s", part, sql)
		}
	}
	return nil
}

func TestChatContextRepositoryProjectionAndSoftDelete(t *testing.T) {
	for _, kind := range []string{"schedule", "report"} {
		t.Run(kind, func(t *testing.T) {
			filters := containsSQL{"classes.deleted_at IS NULL", "students.deleted_at IS NULL"}
			if kind == "report" {
				filters = append(filters, "reports.deleted_at IS NULL", "enrollments.deleted_at IS NULL")
			}
			sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(filters))
			if err != nil {
				t.Fatal(err)
			}
			defer sqlDB.Close()
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			repo := NewChatContextRepository(db)
			id, tenant, student, parent, class, enrollment, reporter := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			var columns []string
			var values []driver.Value
			query := "FROM private_schedule_requests AS req"
			if kind == "schedule" {
				columns = []string{"id", "tenant_id", "parent_id", "class_id", "class_name", "student_id", "student_first_name", "status"}
				values = []driver.Value{id.String(), tenant.String(), parent.String(), class.String(), "Hidden class", student.String(), "Ana", "cancelled"}
			} else {
				query = "FROM reports AS reports"
				columns = []string{"id", "tenant_id", "enrollment_id", "student_id", "student_first_name", "parent_id", "class_name", "title", "reporter_id"}
				values = []driver.Value{id.String(), tenant.String(), enrollment.String(), student.String(), "Ana", parent.String(), "Hidden class", "Private report", reporter.String()}
			}
			call := func() (any, error) {
				if kind == "schedule" {
					return repo.ScheduleRequest(context.Background(), id)
				}
				return repo.Report(context.Background(), id)
			}
			mock.ExpectQuery(query).WithArgs(id, 1).WillReturnRows(sqlmock.NewRows(columns).AddRow(values...))
			got, err := call()
			if err != nil {
				t.Fatal(err)
			}
			if kind == "schedule" {
				item := got.(*domain.ScheduleRequestChatContext)
				if item.ID != id || item.Status != "cancelled" || item.ClassName != "Hidden class" || item.StudentFirstName != "Ana" {
					t.Fatalf("item=%+v", item)
				}
			} else {
				item := got.(*domain.ReportChatContext)
				if item.ID != id || item.ParentID != parent || item.ReporterID != reporter || item.StudentFirstName != "Ana" {
					t.Fatalf("item=%+v", item)
				}
			}
			mock.ExpectQuery(query).WithArgs(id, 1).WillReturnRows(sqlmock.NewRows(columns))
			_, err = call()
			if !errors.Is(err, domain.ErrChatContextNotFound) {
				t.Fatalf("missing=%v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
