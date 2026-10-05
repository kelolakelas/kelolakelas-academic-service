package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func (c *client) PreviewVoucher(ctx context.Context, request VoucherPreviewRequest) (*VoucherPreviewResponse, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal voucher preview: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/billing/vouchers/preview", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Service-Credential", c.credential)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("preview voucher: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope); err == nil && resp.StatusCode == http.StatusUnprocessableEntity && envelope.Code == VoucherRejectedCode {
			return nil, ErrVoucherRejected
		}
		return nil, fmt.Errorf("billing voucher preview returned status %d", resp.StatusCode)
	}
	var envelope struct {
		Data *VoucherPreviewResponse `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode voucher preview: %w", err)
	}
	if envelope.Data == nil || envelope.Data.DiscountAmount < 0 || envelope.Data.GrossAmount < 0 {
		return nil, fmt.Errorf("billing voucher preview returned invalid amounts")
	}
	return envelope.Data, nil
}
