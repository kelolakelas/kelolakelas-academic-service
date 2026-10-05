package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
	"gorm.io/gorm"
)

// VoucherPreviewUsecase is optional so existing enrollment implementations stay compatible.
type VoucherPreviewUsecase interface {
	PreviewVoucher(context.Context, uuid.UUID, uuid.UUID, string) (*billing.VoucherPreviewResponse, error)
}

func (u *enrollmentUsecase) PreviewVoucher(ctx context.Context, parentID, classID uuid.UUID, code string) (*billing.VoucherPreviewResponse, error) {
	if parentID == uuid.Nil {
		return nil, domain.ErrParentRequired
	}
	if strings.TrimSpace(code) == "" || utf8.RuneCountInString(code) > 255 {
		return nil, domain.ErrVoucherRejected
	}
	class, err := u.classRepo.GetByID(ctx, classID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrClassNotFound
	}
	if err != nil {
		return nil, err
	}
	if class.Type != "group" {
		return nil, domain.ErrPrivateCheckout
	}
	if !class.IsPublished || class.EnrollmentStatus != "open" {
		return nil, domain.ErrClassNotEnrollable
	}
	client, ok := u.billingClient.(billing.VoucherPreviewClient)
	if !ok {
		return nil, fmt.Errorf("billing voucher preview unavailable")
	}
	result, err := client.PreviewVoucher(ctx, billing.VoucherPreviewRequest{TenantID: class.TenantID, SubtotalAmount: class.Price, VoucherCode: code})
	if errors.Is(err, billing.ErrVoucherRejected) {
		return nil, domain.ErrVoucherRejected
	}
	return result, err
}
