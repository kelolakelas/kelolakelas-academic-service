package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/gorm"
)

type reviewRepository struct{ db *gorm.DB }

func NewReviewRepository(db *gorm.DB) domain.ReviewRepository { return &reviewRepository{db: db} }

// Lock the enrollment while checking eligibility and saving. This serializes
// concurrent reviews with status transitions, which also update the same row.
// The unique enrollment key independently protects concurrent inserts.
func (r *reviewRepository) Upsert(ctx context.Context, parentID, enrollmentID uuid.UUID, rating int, comment string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ownedID string
		err := tx.Raw(`SELECT e.id FROM enrollments e JOIN students s ON s.id = e.student_id
			WHERE e.id = ? AND s.parent_id = ? AND e.deleted_at IS NULL
			AND s.deleted_at IS NULL AND e.status IN ('active', 'completed')
			FOR UPDATE OF e`, enrollmentID, parentID).Scan(&ownedID).Error
		if err != nil {
			return err
		}
		if ownedID == "" {
			return domain.ErrReviewNotEligible
		}
		result := tx.Exec(`
        INSERT INTO class_reviews (enrollment_id, class_id, rating, comment)
        SELECT e.id, e.class_id, ?, ?
        FROM enrollments e JOIN students s ON s.id = e.student_id
        WHERE e.id = ? AND s.parent_id = ? AND e.deleted_at IS NULL
          AND s.deleted_at IS NULL AND e.status IN ('active', 'completed')
        ON CONFLICT (enrollment_id) DO UPDATE SET
            rating = EXCLUDED.rating, comment = EXCLUDED.comment, updated_at = now()
        WHERE EXISTS (
            SELECT 1 FROM enrollments e JOIN students s ON s.id = e.student_id
            WHERE e.id = EXCLUDED.enrollment_id AND s.parent_id = ?
              AND e.deleted_at IS NULL AND s.deleted_at IS NULL
              AND e.status IN ('active', 'completed')
        )`, rating, comment, enrollmentID, parentID, parentID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return domain.ErrReviewNotEligible
		}
		return nil
	})
}

func (r *reviewRepository) List(ctx context.Context, classID uuid.UUID, page, pageSize int) ([]domain.PublicReview, int64, error) {
	query := r.db.WithContext(ctx).Table("class_reviews").Where("class_id = ?", classID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]domain.PublicReview, 0)
	err := query.Select("rating, comment, created_at, updated_at").Order("created_at DESC, id DESC").Limit(pageSize).Offset((page - 1) * pageSize).Scan(&items).Error
	return items, total, err
}
