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

type catalogPolicyClientStub struct {
	policy grpcclient.CatalogPolicy
	err    error
	calls  int
}

func (s *catalogPolicyClientStub) GetPublicCatalogPolicy(context.Context) (grpcclient.CatalogPolicy, error) {
	s.calls++
	return s.policy, s.err
}
func (s *catalogPolicyClientStub) Close() error { return nil }

func TestCatalogPolicyBothRoutesAndExpiry(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	repo := &catalogRepoStub{items: []domain.CatalogItem{{ID: id}}, total: 1}
	client := &catalogPolicyClientStub{policy: grpcclient.CatalogPolicy{Open: true, AppliedVersion: 1}}
	uc := NewCatalogUsecase(repo, &tenantClientStub{}, time.Minute, client, time.Second).(*catalogUsecase)
	clock := time.Now()
	uc.policy.now = func() time.Time { return clock }
	query := validCatalogQuery()
	opened, err := uc.ListClasses(ctx, query)
	if err != nil || !opened.CatalogOpen || len(opened.Items) != 1 {
		t.Fatalf("open list: %+v, %v", opened, err)
	}
	if _, err = uc.GetClass(ctx, id); err != nil {
		t.Fatalf("open detail: %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("policy calls=%d, want 2 open checks", client.calls)
	}
	client.policy = grpcclient.CatalogPolicy{Open: false, AppliedVersion: 2}
	// Open answers are never cached: page 2 immediately sees an applied
	// close, even though the configured closed-cache TTL has not elapsed.
	query.Page = 2
	closed, err := uc.ListClasses(ctx, query)
	if err != nil || closed.CatalogOpen || len(closed.Items) != 0 || closed.Pagination.TotalItems != 0 || closed.Pagination.Page != 2 {
		t.Fatalf("closed page 2: %+v, %v", closed, err)
	}
	if _, err = uc.GetClass(ctx, id); !errors.Is(err, domain.ErrCatalogClosed) {
		t.Fatalf("closed detail: %v", err)
	}
	if len(repo.items) != 1 || repo.total != 1 {
		t.Fatal("policy modified repository data")
	}
	client.policy = grpcclient.CatalogPolicy{Open: true, AppliedVersion: 3}
	clock = clock.Add(time.Second)
	reopened, err := uc.ListClasses(ctx, validCatalogQuery())
	if err != nil || !reopened.CatalogOpen || len(reopened.Items) != 1 {
		t.Fatalf("reopened list: %+v %v", reopened, err)
	}
}

func TestCatalogPolicyOutageHidesBothRoutesAfterTTL(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	repo := &catalogRepoStub{tenantIDs: []uuid.UUID{id}, freshIDs: []uuid.UUID{id}, items: []domain.CatalogItem{{ID: id}}, total: 1}
	client := &catalogPolicyClientStub{policy: grpcclient.CatalogPolicy{Open: true}}
	uc := NewCatalogUsecase(repo, &tenantClientStub{}, time.Minute, client, time.Second).(*catalogUsecase)
	now := time.Now()
	uc.policy.now = func() time.Time { return now }
	if _, err := uc.ListClasses(ctx, validCatalogQuery()); err != nil {
		t.Fatal(err)
	}
	client.err = errors.New("identity unavailable")
	now = now.Add(time.Second)
	if _, err := uc.ListClasses(ctx, validCatalogQuery()); !errors.Is(err, domain.ErrCatalogPolicyUnavailable) {
		t.Fatalf("list outage: %v", err)
	}
	if _, err := uc.GetClass(ctx, id); !errors.Is(err, domain.ErrCatalogPolicyUnavailable) {
		t.Fatalf("detail outage: %v", err)
	}
	if client.calls != 3 {
		t.Fatalf("failed read cached: calls=%d", client.calls)
	}
	if len(repo.sinceSamples) != 1 {
		t.Fatalf("snapshot refreshed under outage: %d", len(repo.sinceSamples))
	}
}
