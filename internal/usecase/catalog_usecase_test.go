package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

// catalogRepoStub models the snapshot surface the catalog depends on: which tenants own
// classes, which of them already have a fresh snapshot, and what the usecase wrote back.
type catalogRepoStub struct {
	tenantIDs    []uuid.UUID
	freshIDs     []uuid.UUID
	sinceSamples []time.Time
	written      []domain.TenantLocationSnapshot
	upsertErr    error
	items        []domain.CatalogItem
	total        int64
}

func (s *catalogRepoStub) TenantIDs(context.Context) ([]uuid.UUID, error) { return s.tenantIDs, nil }

func (s *catalogRepoStub) FreshSnapshotTenantIDs(_ context.Context, since time.Time) ([]uuid.UUID, error) {
	s.sinceSamples = append(s.sinceSamples, since)
	return s.freshIDs, nil
}

func (s *catalogRepoStub) UpsertTenantSnapshots(_ context.Context, snapshots []domain.TenantLocationSnapshot) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.written = append(s.written, snapshots...)
	return nil
}

func (s *catalogRepoStub) List(context.Context, domain.CatalogQuery) ([]domain.CatalogItem, int64, error) {
	return s.items, s.total, nil
}

func (s *catalogRepoStub) GetByID(_ context.Context, id uuid.UUID) (*domain.CatalogItem, error) {
	if len(s.items) == 0 {
		return nil, errors.New("not found")
	}
	item := s.items[0]
	return &item, nil
}

// tenantClientStub records every identity call and can fail the way an unreachable identity
// fails, so the "serve from snapshot" path is asserted rather than assumed.
type tenantClientStub struct {
	calls   [][]string
	info    map[string]grpcclient.TenantPublicInfo
	err     error
	delay   time.Duration
	ctxErrs []error
}

func (s *tenantClientStub) ValidateTenantStatus(context.Context, string) (bool, string, error) {
	return true, "", nil
}

func (s *tenantClientStub) GetTenantPublicInfo(ctx context.Context, tenantIDs []string) (map[string]grpcclient.TenantPublicInfo, error) {
	s.calls = append(s.calls, tenantIDs)
	s.ctxErrs = append(s.ctxErrs, ctx.Err())
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.info, nil
}

func (s *tenantClientStub) Close() error { return nil }

func tenantInfo(id uuid.UUID, active bool, name string) grpcclient.TenantPublicInfo {
	return grpcclient.TenantPublicInfo{ID: id.String(), Name: name, AddressFormatted: "Jl. Merdeka 1", Latitude: -6.2, Longitude: 106.8, HasLocation: true, IsActive: active}
}

func validCatalogQuery() domain.CatalogQuery {
	return domain.CatalogQuery{ListQuery: domain.ListQuery{Page: 1, PageSize: 20}, RadiusKM: 25, Sort: "newest"}
}

func TestCatalogServesFromFreshSnapshotWithoutCallingIdentity(t *testing.T) {
	tenantID := uuid.New()
	repo := &catalogRepoStub{tenantIDs: []uuid.UUID{tenantID}, freshIDs: []uuid.UUID{tenantID}}
	client := &tenantClientStub{info: map[string]grpcclient.TenantPublicInfo{tenantID.String(): tenantInfo(tenantID, true, "Tenant")}}
	uc := NewCatalogUsecase(repo, client, time.Minute, &catalogPolicyClientStub{policy: grpcclient.CatalogPolicy{Open: true}}, time.Second)

	if _, err := uc.ListClasses(context.Background(), validCatalogQuery()); err != nil {
		t.Fatalf("ListClasses error: %v", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("identity calls=%v, want none while the snapshot is fresh", client.calls)
	}
	if len(repo.written) != 0 {
		t.Fatalf("snapshot writes=%v, want none while the snapshot is fresh", repo.written)
	}
	if len(repo.sinceSamples) != 1 {
		t.Fatalf("freshness lookups=%d, want exactly one per request", len(repo.sinceSamples))
	}
	expectedCutoff := time.Now().Add(-time.Minute)
	if repo.sinceSamples[0].Before(expectedCutoff.Add(-time.Second)) || repo.sinceSamples[0].After(expectedCutoff.Add(time.Second)) {
		t.Fatalf("freshness cutoff=%s, want approximately %s", repo.sinceSamples[0], expectedCutoff)
	}
}

func TestCatalogSecondRequestInsideTTLDoesNotRefreshAgain(t *testing.T) {
	tenantID := uuid.New()
	// The first request refreshes and records the write; the second request must observe a
	// fresh snapshot, which is what the repository reports after the first upsert lands.
	repo := &catalogRepoStub{tenantIDs: []uuid.UUID{tenantID}}
	client := &tenantClientStub{info: map[string]grpcclient.TenantPublicInfo{tenantID.String(): tenantInfo(tenantID, true, "Tenant")}}
	uc := NewCatalogUsecase(repo, client, time.Minute, &catalogPolicyClientStub{policy: grpcclient.CatalogPolicy{Open: true}}, time.Second)

	if _, err := uc.ListClasses(context.Background(), validCatalogQuery()); err != nil {
		t.Fatalf("first ListClasses error: %v", err)
	}
	if len(client.calls) != 1 || len(repo.written) != 1 {
		t.Fatalf("after first request: identity calls=%d writes=%d, want 1 and 1", len(client.calls), len(repo.written))
	}
	// Simulate the stored snapshot now being younger than the TTL.
	repo.freshIDs = []uuid.UUID{tenantID}

	if _, err := uc.ListClasses(context.Background(), validCatalogQuery()); err != nil {
		t.Fatalf("second ListClasses error: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("identity calls=%v, want the second in-TTL request to skip identity", client.calls)
	}
	if len(repo.written) != 1 {
		t.Fatalf("snapshot writes=%d, want no second write inside the TTL", len(repo.written))
	}
}

func TestCatalogRefreshesOnlyMissingOrStaleTenants(t *testing.T) {
	freshID, staleID := uuid.New(), uuid.New()
	repo := &catalogRepoStub{tenantIDs: []uuid.UUID{freshID, staleID}, freshIDs: []uuid.UUID{freshID}, items: []domain.CatalogItem{{ID: uuid.New()}}}
	client := &tenantClientStub{info: map[string]grpcclient.TenantPublicInfo{staleID.String(): tenantInfo(staleID, true, "Stale")}}
	uc := NewCatalogUsecase(repo, client, time.Minute, &catalogPolicyClientStub{policy: grpcclient.CatalogPolicy{Open: true}}, time.Second)

	if _, err := uc.GetClass(context.Background(), uuid.New()); err != nil {
		t.Fatalf("GetClass error: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("identity calls=%d, want one refresh call", len(client.calls))
	}
	if got := client.calls[0]; len(got) != 1 || got[0] != staleID.String() {
		t.Fatalf("refreshed tenants=%v, want only %s", got, staleID)
	}
	if len(repo.written) != 1 || repo.written[0].TenantID != staleID {
		t.Fatalf("written snapshots=%v, want only the stale tenant", repo.written)
	}
	if repo.written[0].UpdatedAt.IsZero() {
		t.Fatal("refreshed snapshot has zero UpdatedAt, which would make the TTL unmeasurable")
	}
}

func TestCatalogServesSnapshotWhenIdentityIsUnavailable(t *testing.T) {
	tenantID := uuid.New()
	repo := &catalogRepoStub{tenantIDs: []uuid.UUID{tenantID}}
	client := &tenantClientStub{err: errors.New("identity unreachable")}
	uc := NewCatalogUsecase(repo, client, time.Minute, &catalogPolicyClientStub{policy: grpcclient.CatalogPolicy{Open: true}}, time.Second)

	response, err := uc.ListClasses(context.Background(), validCatalogQuery())
	if err != nil {
		t.Fatalf("ListClasses error=%v, want the catalog to keep serving from the snapshot", err)
	}
	if response == nil {
		t.Fatal("ListClasses returned no response while identity was down")
	}
	if len(repo.written) != 0 {
		t.Fatalf("snapshot writes=%v, want none when the refresh fails", repo.written)
	}
}

func TestCatalogDropsTenantInfoWithInvalidID(t *testing.T) {
	tenantID := uuid.New()
	repo := &catalogRepoStub{tenantIDs: []uuid.UUID{tenantID}}
	client := &tenantClientStub{info: map[string]grpcclient.TenantPublicInfo{
		tenantID.String(): tenantInfo(tenantID, true, "Tenant"),
		"not-a-uuid":      {ID: "not-a-uuid", Name: "Bogus", IsActive: true},
	}}
	uc := NewCatalogUsecase(repo, client, time.Minute, &catalogPolicyClientStub{policy: grpcclient.CatalogPolicy{Open: true}}, time.Second)

	if _, err := uc.ListClasses(context.Background(), validCatalogQuery()); err != nil {
		t.Fatalf("ListClasses error: %v", err)
	}
	if len(repo.written) != 1 || repo.written[0].TenantID != tenantID {
		t.Fatalf("written snapshots=%v, want only the parseable tenant", repo.written)
	}
	for _, snapshot := range repo.written {
		if snapshot.TenantID == uuid.Nil {
			t.Fatal("a snapshot was stored under uuid.Nil, which would be visible to every tenant lookup")
		}
	}
}

func TestCatalogUsecaseAppliesDefaultTTLForNonPositiveValue(t *testing.T) {
	usecase, ok := NewCatalogUsecase(&catalogRepoStub{}, &tenantClientStub{}, 0, &catalogPolicyClientStub{policy: grpcclient.CatalogPolicy{Open: true}}, 0).(*catalogUsecase)
	if !ok {
		t.Fatal("NewCatalogUsecase did not return a *catalogUsecase")
	}
	if usecase.ttl != CatalogTTL {
		t.Fatalf("ttl=%s, want the %s default", usecase.ttl, CatalogTTL)
	}
}

func TestCatalogRefreshPassesCallerContextToIdentity(t *testing.T) {
	tenantID := uuid.New()
	repo := &catalogRepoStub{tenantIDs: []uuid.UUID{tenantID}, items: []domain.CatalogItem{{ID: uuid.New()}}}
	client := &tenantClientStub{info: map[string]grpcclient.TenantPublicInfo{tenantID.String(): tenantInfo(tenantID, true, "Tenant")}}
	uc := NewCatalogUsecase(repo, client, time.Minute, &catalogPolicyClientStub{policy: grpcclient.CatalogPolicy{Open: true}}, time.Second)

	if _, err := uc.GetClass(context.Background(), uuid.New()); err != nil {
		t.Fatalf("GetClass error: %v", err)
	}
	if len(client.ctxErrs) != 1 || client.ctxErrs[0] != nil {
		t.Fatalf("identity call context errors=%v, want one live context", client.ctxErrs)
	}
}
