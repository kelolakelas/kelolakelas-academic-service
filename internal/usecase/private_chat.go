package usecase

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/privatechat"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

type PrivateChatInvoiceReader interface {
	Invoice(context.Context, privatechat.InvoiceScope) (*privatechat.Invoice, error)
}
type PrivateChatSender interface {
	Deliver(context.Context, privatechat.Delivery) error
}
type PrivateChatEvents struct {
	Store    *repository.PrivateChatStore
	Invoices PrivateChatInvoiceReader
}

func privateSlotsText(slots []domain.PrivateScheduleSlot) string {
	days := []string{"", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu", "Minggu"}
	lines := []string{}
	for _, s := range slots {
		if s.DayOfWeek < 1 || s.DayOfWeek > 7 || len(s.StartTime) < 5 || len(s.EndTime) < 5 {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s–%s", days[s.DayOfWeek], s.StartTime, s.EndTime))
	}
	return strings.Join(lines, "\n")
}
func (s *PrivateChatEvents) Projection(ctx context.Context, id uuid.UUID) (*domain.PrivateChatProjection, error) {
	e, err := s.Store.Event(ctx, id)
	if err != nil {
		return nil, err
	}
	p := &domain.PrivateChatProjection{PrivateChatEvent: *e}
	title := fmt.Sprintf("%s — %s", e.ClassName, e.StudentFirstName)
	switch e.Kind {
	case "created":
		p.Body = "Mengajukan jadwal kelas privat: " + title + "\n" + privateSlotsText(e.Slots)
	case "rejected":
		p.Body = "Permintaan jadwal ditolak: " + title
		if e.Reason != nil && strings.TrimSpace(*e.Reason) != "" {
			p.Body += "\nAlasan: " + *e.Reason
		}
		if len(e.RecommendedSlots) > 0 {
			p.Body += "\nRekomendasi jadwal:\n" + privateSlotsText(e.RecommendedSlots)
		}
	case "approved", "recommendation_accepted":
		enrollment, err := s.Store.Enrollment(ctx, e)
		if err != nil {
			return nil, err
		}
		if (enrollment.Status != "pending" && enrollment.Status != "active") || enrollment.PaymentTransactionID == nil || *enrollment.PaymentTransactionID == uuid.Nil {
			return nil, domain.ErrPrivateChatNotReady
		}
		if s.Invoices == nil {
			return nil, privatechat.ErrUnavailable
		}
		invoice, err := s.Invoices.Invoice(ctx, privatechat.InvoiceScope{TransactionID: *enrollment.PaymentTransactionID, EnrollmentID: enrollment.ID, TenantID: e.TenantID, ParentID: e.ParentID, StudentID: e.StudentID})
		if err != nil {
			return nil, err
		}
		if invoice == nil {
			return nil, domain.ErrPrivateChatNotReady
		}
		p.TransactionID = &invoice.ID
		p.Body = "Jadwal kelas privat disetujui: " + title + "\n" + privateSlotsText(e.Slots)
		if e.Kind == "recommendation_accepted" {
			p.Body = "Parent menerima rekomendasi jadwal: " + title + "\n" + privateSlotsText(e.Slots)
		}
		switch invoice.Status {
		case "pending":
			if invoice.InvoiceExpiresAt == nil {
				return nil, domain.ErrPrivateChatNotReady
			}
			deadline := invoice.InvoiceExpiresAt.In(time.FixedZone("WIB", 7*60*60)).Format("02 Jan 2006 15:04:05 WIB (UTC+07:00)")
			p.Body += "\nBatas pembayaran: " + deadline
			if !invoice.InvoiceExpiresAt.After(time.Now()) {
				p.Body += "\nBatas pembayaran telah lewat. Hubungi admin untuk langkah berikutnya."
				break
			}
			checkout, err := url.Parse(invoice.CheckoutSessionURL)
			if err != nil || checkout.Hostname() == "" || checkout.User != nil || checkout.Opaque != "" || (checkout.Scheme != "https" && checkout.Scheme != "http") || len(invoice.CheckoutSessionURL) > 1800 || strings.TrimSpace(invoice.CheckoutSessionURL) != invoice.CheckoutSessionURL {
				return nil, domain.ErrPrivateChatNotReady
			}
			p.Body += "\nLink pembayaran: " + invoice.CheckoutSessionURL
			p.InvoiceExpiresAt = invoice.InvoiceExpiresAt
		case "paid":
			p.Body += "\nPembayaran telah diterima. Tidak perlu melakukan pembayaran lagi."
		case "expired", "failed", "cancelled", "refunded":
			p.Body += "\nInvoice tidak dapat dibayar. Hubungi admin untuk langkah berikutnya."
		default:
			return nil, domain.ErrPrivateChatNotReady
		}
	default:
		return nil, domain.ErrPrivateChatNotReady
	}
	if len([]rune(p.Body)) > 8192 {
		return nil, domain.ErrPrivateChatNotReady
	}
	return p, nil
}

type PrivateChatWorker struct {
	Store    *repository.PrivateChatStore
	Sender   PrivateChatSender
	Interval time.Duration
}

func (w *PrivateChatWorker) RunOnce(ctx context.Context) error {
	for range 50 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		d, err := w.Store.Claim(ctx)
		if err != nil {
			return err
		}
		if d == nil {
			return nil
		}
		e, err := w.Store.Event(ctx, d.EventID)
		if d.Attempts > 12 {
			err = errors.New("retry exhausted")
		}
		if err == nil {
			err = w.Sender.Deliver(ctx, privatechat.Delivery{EventID: e.ID, RequestID: e.RequestID, TenantID: e.TenantID, ParentID: e.ParentID})
		}
		status, code, delay := "sent", "", time.Duration(0)
		if err != nil {
			status, code = "pending", "delivery_unconfirmed"
			if errors.Is(err, domain.ErrPrivateChatNotReady) {
				code = "invoice_or_decision_not_ready"
			}
			delay = time.Duration(5*(1<<min(d.Attempts-1, 9))) * time.Second
			if d.Attempts >= 12 {
				status = "failed"
			}
		}
		if err = w.Store.Finish(ctx, d, status, code, delay); err != nil {
			return err
		}
		slog.InfoContext(ctx, "private request chat delivery", "event_id", d.EventID, "attempt", d.Attempts, "status", status, "code", code)
	}
	return nil
}
func (w *PrivateChatWorker) Run(ctx context.Context) {
	interval := w.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "private request chat worker", "code", "outbox_unavailable")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
