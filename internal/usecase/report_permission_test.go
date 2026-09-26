package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

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
	created *domain.Report
}

func (s *reportWriteStub) Create(_ context.Context, item *domain.Report) error {
	s.created = item
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
