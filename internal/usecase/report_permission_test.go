package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

type reportEnrollmentStub struct {
	repository.EnrollmentRepository
	enrollment *domain.Enrollment
	assigned   bool
}

func (s *reportEnrollmentStub) GetByIDForAccess(_ context.Context, tenantID, parentID *uuid.UUID, id uuid.UUID) (*domain.Enrollment, error) {
	if tenantID == nil || parentID != nil || id != s.enrollment.ID {
		return nil, errors.New("unexpected enrollment scope")
	}
	return s.enrollment, nil
}
func (s *reportEnrollmentStub) IsTutorForEnrollment(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return s.assigned, nil
}

type reportWriteStub struct {
	repository.ReportRepository
	created    *domain.Report
	stored     *domain.Report
	updated    int
	deleted    []uuid.UUID
	deletedErr error
}

func (s *reportWriteStub) Create(_ context.Context, item *domain.Report) error {
	s.created = item
	return nil
}

func (s *reportWriteStub) GetByIDForTenant(_ context.Context, tenantID, id uuid.UUID) (*domain.Report, error) {
	if s.stored == nil || s.stored.ID != id || s.stored.TenantID != tenantID {
		return nil, gorm.ErrRecordNotFound
	}
	return s.stored, nil
}

func (s *reportWriteStub) Update(_ context.Context, item *domain.Report) error {
	s.updated++
	s.stored = item
	return nil
}

func (s *reportWriteStub) Delete(_ context.Context, id uuid.UUID) error {
	if s.deletedErr != nil {
		return s.deletedErr
	}
	s.deleted = append(s.deleted, id)
	return nil
}

func TestReportCreatePreservesTutorAssignment(t *testing.T) {
	memberID, tenantID, enrollmentID := uuid.New(), uuid.New(), uuid.New()
	for _, assigned := range []bool{false, true} {
		name := "unassigned"
		if assigned {
			name = "assigned"
		}
		t.Run(name, func(t *testing.T) {
			enrollments := &reportEnrollmentStub{enrollment: &domain.Enrollment{ID: enrollmentID}, assigned: assigned}
			writes := &reportWriteStub{}
			item, err := NewReportUsecase(writes, enrollments).Create(context.Background(), tenantID, memberID, &domain.CreateReportRequest{EnrollmentID: enrollmentID, Title: "Progress"})
			if !assigned {
				if !errors.Is(err, domain.ErrReportForbidden) || writes.created != nil {
					t.Fatalf("unassigned err=%v stored=%v", err, writes.created)
				}
				return
			}
			if err != nil || item == nil || writes.created != item || item.TenantID != tenantID || item.ReporterID != memberID || item.EnrollmentID != enrollmentID {
				t.Fatalf("assigned err=%v item=%+v stored=%v", err, item, writes.created != nil)
			}
		})
	}
}

// TestReportUpdateDeleteRequireTutorAssignment is the KEL-135 proof that a
// tutor who does not teach the report's class cannot change or remove the
// report: unassigned callers are rejected with ErrReportForbidden and write
// nothing, assigned callers succeed. The check reads the stored enrollment,
// so a caller cannot retarget the report to a class they teach.
func TestReportUpdateDeleteRequireTutorAssignment(t *testing.T) {
	tenantID, memberID, enrollmentID, reportID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	newStored := func() *domain.Report {
		return &domain.Report{ID: reportID, TenantID: tenantID, EnrollmentID: enrollmentID, ReporterID: memberID, Title: "Before"}
	}
	t.Run("update unassigned is forbidden with zero writes", func(t *testing.T) {
		enrollments := &reportEnrollmentStub{enrollment: &domain.Enrollment{ID: enrollmentID}, assigned: false}
		writes := &reportWriteStub{stored: newStored()}
		res, err := NewReportUsecase(writes, enrollments).Update(context.Background(), tenantID, memberID, reportID, &domain.UpdateReportRequest{Title: "After"})
		if !errors.Is(err, domain.ErrReportForbidden) || res != nil {
			t.Fatalf("res=%+v err=%v want ErrReportForbidden", res, err)
		}
		if writes.updated != 0 {
			t.Fatalf("updates=%d want=0 for an unassigned tutor", writes.updated)
		}
	})
	t.Run("update assigned changes the stored row", func(t *testing.T) {
		enrollments := &reportEnrollmentStub{enrollment: &domain.Enrollment{ID: enrollmentID}, assigned: true}
		writes := &reportWriteStub{stored: newStored()}
		res, err := NewReportUsecase(writes, enrollments).Update(context.Background(), tenantID, memberID, reportID, &domain.UpdateReportRequest{Title: "After"})
		if err != nil || res == nil || res.Title != "After" {
			t.Fatalf("res=%+v err=%v want title=After", res, err)
		}
		if writes.updated != 1 {
			t.Fatalf("updates=%d want=1", writes.updated)
		}
	})
	t.Run("delete unassigned is forbidden with zero deletes", func(t *testing.T) {
		enrollments := &reportEnrollmentStub{enrollment: &domain.Enrollment{ID: enrollmentID}, assigned: false}
		writes := &reportWriteStub{stored: newStored()}
		err := NewReportUsecase(writes, enrollments).Delete(context.Background(), tenantID, memberID, reportID)
		if !errors.Is(err, domain.ErrReportForbidden) {
			t.Fatalf("err=%v want ErrReportForbidden", err)
		}
		if len(writes.deleted) != 0 {
			t.Fatalf("deletes=%d want=0 for an unassigned tutor", len(writes.deleted))
		}
	})
	t.Run("delete assigned removes the row", func(t *testing.T) {
		enrollments := &reportEnrollmentStub{enrollment: &domain.Enrollment{ID: enrollmentID}, assigned: true}
		writes := &reportWriteStub{stored: newStored()}
		if err := NewReportUsecase(writes, enrollments).Delete(context.Background(), tenantID, memberID, reportID); err != nil {
			t.Fatalf("Delete error: %v", err)
		}
		if len(writes.deleted) != 1 || writes.deleted[0] != reportID {
			t.Fatalf("deleted=%v want=[%s]", writes.deleted, reportID)
		}
	})
}
