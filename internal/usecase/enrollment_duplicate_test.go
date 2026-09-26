package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

// duplicateEnrollmentRepo adds the idempotency-key lookup the use case runs after a
// failed create, so the tests can tell a same-key retry from a different-key duplicate.
type duplicateEnrollmentRepo struct {
	marketplaceEnrollmentRepo
	byKey map[string]*domain.Enrollment
}

func (r *duplicateEnrollmentRepo) GetByIDForUpdate(context.Context, uuid.UUID) (*domain.Enrollment, error) {
	return nil, gorm.ErrRecordNotFound
}
func (r *duplicateEnrollmentRepo) GetByIdempotencyKeyForTenant(context.Context, uuid.UUID, string) (*domain.Enrollment, error) {
	return nil, gorm.ErrRecordNotFound
}
func (r *duplicateEnrollmentRepo) GetByIdempotencyKeyAny(_ context.Context, key string) (*domain.Enrollment, error) {
	if existing, ok := r.byKey[key]; ok {
		return existing, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func TestEnrollPublicDuplicateEnrollment(t *testing.T) {
	parentID, studentID, classID := uuid.New(), uuid.New(), uuid.New()
	class := &domain.Class{ID: classID, TenantID: uuid.New(), Type: "private", Price: 100, IsPublished: true, EnrollmentStatus: "open"}
	transactionID := uuid.New()
	sameKeyWinner := &domain.Enrollment{ID: uuid.New(), StudentID: studentID, ClassID: classID, BillingCycle: "monthly", Status: "pending", PaymentStatus: "pending", PaymentTransactionID: &transactionID}

	newUsecase := func(repo *duplicateEnrollmentRepo, billing *marketplaceBilling) EnrollmentUsecase {
		return NewEnrollmentUsecase(repo, &marketplaceStudentRepo{student: &domain.Student{ID: studentID, ParentID: parentID}}, &marketplaceClassRepo{class: class}, billing, marketplaceTx{})
	}
	request := &domain.PublicEnrollmentRequest{StudentID: studentID, BillingCycle: "monthly"}

	t.Run("different key returns the duplicate sentinel without an invoice", func(t *testing.T) {
		repo := &duplicateEnrollmentRepo{marketplaceEnrollmentRepo: marketplaceEnrollmentRepo{createErr: domain.ErrDuplicateEnrollment}}
		billing := &marketplaceBilling{}
		_, err := newUsecase(repo, billing).EnrollPublic(context.Background(), parentID, classID, request, "second-tab-key")
		if !errors.Is(err, domain.ErrDuplicateEnrollment) {
			t.Fatalf("error = %v, want ErrDuplicateEnrollment", err)
		}
		if billing.calls != 0 {
			t.Fatalf("billing calls = %d, want 0", billing.calls)
		}
	})

	t.Run("same key that lost the race returns the winning enrollment", func(t *testing.T) {
		repo := &duplicateEnrollmentRepo{marketplaceEnrollmentRepo: marketplaceEnrollmentRepo{createErr: domain.ErrDuplicateEnrollment}, byKey: map[string]*domain.Enrollment{"same-key": sameKeyWinner}}
		result, err := newUsecase(repo, &marketplaceBilling{}).EnrollPublic(context.Background(), parentID, classID, request, "same-key")
		if err != nil {
			t.Fatalf("same-key retry error = %v", err)
		}
		if result.Enrollment.ID != sameKeyWinner.ID {
			t.Fatalf("enrollment = %s, want %s", result.Enrollment.ID, sameKeyWinner.ID)
		}
	})

	t.Run("same key replay returns the stored enrollment before any create", func(t *testing.T) {
		repo := &duplicateEnrollmentRepo{marketplaceEnrollmentRepo: marketplaceEnrollmentRepo{existing: sameKeyWinner, createErr: domain.ErrDuplicateEnrollment}}
		result, err := newUsecase(repo, &marketplaceBilling{}).EnrollPublic(context.Background(), parentID, classID, request, "same-key")
		if err != nil || result.Enrollment.ID != sameKeyWinner.ID {
			t.Fatalf("replay result=%+v err=%v, want the stored enrollment", result, err)
		}
	})

	t.Run("tenant enrollment keeps the sentinel wrapped", func(t *testing.T) {
		repo := &duplicateEnrollmentRepo{marketplaceEnrollmentRepo: marketplaceEnrollmentRepo{createErr: domain.ErrDuplicateEnrollment}}
		_, err := newUsecase(repo, &marketplaceBilling{}).EnrollStudent(context.Background(), class.TenantID, &domain.EnrollStudentRequest{StudentID: studentID, ClassID: classID, BillingCycle: "monthly", IdempotencyKey: "tenant-key"})
		if !errors.Is(err, domain.ErrDuplicateEnrollment) {
			t.Fatalf("error = %v, want ErrDuplicateEnrollment", err)
		}
	})
}
