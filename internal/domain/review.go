package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

const MaxReviewCommentLength = 2000

var ErrReviewNotEligible = errors.New("eligible enrollment not found")
var ErrInvalidReview = errors.New("invalid review")

type ReviewRequest struct {
	Rating  int    `json:"rating"`
	Comment string `json:"comment"`
}

// PublicReview deliberately contains no enrollment, student, or parent identifier.
type PublicReview struct {
	Rating    int       `json:"rating"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ReviewListResponse struct {
	Items      []PublicReview `json:"items"`
	Pagination Pagination     `json:"pagination"`
}

type ReviewRepository interface {
	Upsert(ctx context.Context, parentID, enrollmentID uuid.UUID, rating int, comment string) error
	List(ctx context.Context, classID uuid.UUID, page, pageSize int) ([]PublicReview, int64, error)
}
