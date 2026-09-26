package usecase

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

type CatalogUsecase interface {
	ListClasses(ctx context.Context, query domain.CatalogQuery) (*domain.CatalogListResponse, error)
	GetClass(ctx context.Context, id uuid.UUID) (*domain.CatalogItem, error)
}

// CatalogTTL bounds how long a tenant snapshot may age before identity is asked again.
// Identity is the source of truth for tenant name, location, and active status, so a
// snapshot older than the TTL is refreshed instead of trusted forever.
const CatalogTTL = 5 * time.Minute

type catalogUsecase struct {
	repo         domain.CatalogRepository
	tenantClient grpcclient.TenantClient
	ttl          time.Duration
	policy       *catalogPolicyGate
}

// NewCatalogUsecase builds the catalog usecase. A non-positive ttl falls back to
// CatalogTTL so a zero value cannot silently disable snapshot refresh; a
// non-positive policyTTL falls back to CatalogPolicyTTL. A nil policyClient
// hides the catalog (fail closed) rather than showing it unguarded.
func NewCatalogUsecase(repo domain.CatalogRepository, tenantClient grpcclient.TenantClient, ttl time.Duration, policyClient grpcclient.CatalogPolicyClient, policyTTL time.Duration) CatalogUsecase {
	if ttl <= 0 {
		ttl = CatalogTTL
	}
	return &catalogUsecase{repo: repo, tenantClient: tenantClient, ttl: ttl, policy: newCatalogPolicyGate(policyClient, policyTTL)}
}

// refreshTenantSnapshots asks identity only for tenants whose snapshot is missing or older
// than the TTL. Callers keep serving from the stored snapshot when identity is unreachable,
// so an identity outage degrades freshness instead of the catalog response itself.
// Callers must not run their catalog query before this returns.
func (u *catalogUsecase) refreshTenantSnapshots(ctx context.Context) {
	ids, err := u.repo.TenantIDs(ctx)
	if err != nil {
		slog.Error("catalog tenant list failed", "error", err)
		return
	}
	if len(ids) == 0 {
		return
	}
	fresh, err := u.repo.FreshSnapshotTenantIDs(ctx, time.Now().Add(-u.ttl))
	if err != nil {
		slog.Error("catalog snapshot freshness lookup failed", "error", err)
		return
	}
	freshSet := make(map[uuid.UUID]struct{}, len(fresh))
	for _, id := range fresh {
		freshSet[id] = struct{}{}
	}
	missing := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := freshSet[id]; !ok {
			missing = append(missing, id.String())
		}
	}
	if len(missing) == 0 {
		return
	}

	info, err := u.tenantClient.GetTenantPublicInfo(ctx, missing)
	if err != nil {
		slog.Warn("catalog tenant info refresh skipped", "error", err, "tenants", len(missing))
		return
	}
	now := time.Now().UTC()
	snapshots := make([]domain.TenantLocationSnapshot, 0, len(info))
	for _, tenant := range info {
		tenantID, parseErr := uuid.Parse(tenant.ID)
		if parseErr != nil {
			// A tenant id identity cannot parse would otherwise be stored under uuid.Nil
			// and become visible for every tenant lookup, so it is dropped instead.
			slog.Warn("catalog tenant info ignored invalid tenant id", "tenant_id", tenant.ID)
			continue
		}
		snapshots = append(snapshots, domain.TenantLocationSnapshot{TenantID: tenantID, Name: tenant.Name, AddressFormatted: stringPtr(tenant.AddressFormatted), Latitude: floatPtr(tenant.Latitude, tenant.HasLocation), Longitude: floatPtr(tenant.Longitude, tenant.HasLocation), IsActive: tenant.IsActive, UpdatedAt: now})
	}
	if err := u.repo.UpsertTenantSnapshots(ctx, snapshots); err != nil {
		slog.Error("catalog tenant snapshot refresh failed", "error", err, "tenants", len(snapshots))
	}
}

// GetClass and ListClasses both run the same policy gate before any snapshot
// refresh or catalog query (KEL-98). A closed or unreadable policy returns
// before the query, so no class row is read, let alone shown; class and
// tenant data are never modified by the policy.
func (u *catalogUsecase) GetClass(ctx context.Context, id uuid.UUID) (*domain.CatalogItem, error) {
	if err := u.policy.check(ctx); err != nil {
		return nil, err
	}
	u.refreshTenantSnapshots(ctx)
	return u.repo.GetByID(ctx, id)
}

func (u *catalogUsecase) ListClasses(ctx context.Context, query domain.CatalogQuery) (*domain.CatalogListResponse, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}
	if err := u.policy.check(ctx); err != nil {
		if errors.Is(err, domain.ErrCatalogClosed) {
			// A closed catalog is a clear, successful public answer: no items,
			// zero totals on every page, and catalog_open=false.
			return &domain.CatalogListResponse{Items: []domain.CatalogItem{}, Pagination: domain.Pagination{Page: query.Page, PageSize: query.PageSize}, CatalogOpen: false}, nil
		}
		return nil, err
	}
	u.refreshTenantSnapshots(ctx)
	items, total, err := u.repo.List(ctx, query)
	if err != nil {
		return nil, err
	}
	return &domain.CatalogListResponse{Items: items, Pagination: domain.Pagination{Page: query.Page, PageSize: query.PageSize, TotalItems: total, TotalPages: int(math.Ceil(float64(total) / float64(query.PageSize)))}, CatalogOpen: true}, nil
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func floatPtr(value float64, ok bool) *float64 {
	if !ok {
		return nil
	}
	return &value
}
