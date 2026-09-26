package usecase

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

// CatalogPolicyTTL bounds how long a closed policy read is reused. Open reads
// are never cached so a closing change reaches both routes on the next call.
const CatalogPolicyTTL = 15 * time.Second

// catalogPolicyGate is the single public catalog policy check (KEL-98) shared
// by list and detail, so the two routes can never enforce different policies.
//
// Only successful closed reads are cached, and only for ttl. An open answer
// must be revalidated on every request: serving a cached open value after an
// applied close would expose classes that should be hidden. A failed or
// untrusted read never falls back to an older answer and hides the catalog.
type catalogPolicyGate struct {
	client grpcclient.CatalogPolicyClient
	ttl    time.Duration
	now    func() time.Time

	mu        sync.Mutex
	cached    grpcclient.CatalogPolicy
	fetchedAt time.Time
	hasCached bool
	lastSeen  int64
}

func newCatalogPolicyGate(client grpcclient.CatalogPolicyClient, ttl time.Duration) *catalogPolicyGate {
	if ttl <= 0 {
		ttl = CatalogPolicyTTL
	}
	return &catalogPolicyGate{client: client, ttl: ttl, now: time.Now, lastSeen: -1}
}

// check returns nil when the catalog may be shown, ErrCatalogClosed when the
// effective policy is closed, and ErrCatalogPolicyUnavailable when it cannot
// be read. A nil client is treated as unavailable, never as open.
func (g *catalogPolicyGate) check(ctx context.Context) error {
	policy, err := g.policy(ctx)
	if err != nil {
		return domain.ErrCatalogPolicyUnavailable
	}
	if !policy.Open {
		return domain.ErrCatalogClosed
	}
	return nil
}

func (g *catalogPolicyGate) policy(ctx context.Context) (grpcclient.CatalogPolicy, error) {
	if g == nil || g.client == nil {
		return grpcclient.CatalogPolicy{}, domain.ErrCatalogPolicyUnavailable
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if g.hasCached && !g.cached.Open && now.Sub(g.fetchedAt) < g.ttl {
		return g.cached, nil
	}
	// Drop the expired answer before asking again, so a failed refresh can
	// never leave an old "open" in place.
	g.hasCached = false
	policy, err := g.client.GetPublicCatalogPolicy(ctx)
	if err != nil {
		slog.Warn("public catalog policy unavailable; catalog hidden", "error", err)
		return grpcclient.CatalogPolicy{}, err
	}
	g.cached, g.fetchedAt, g.hasCached = policy, now, true
	if policy.AppliedVersion != g.lastSeen {
		// Record the applied control-plane version academic now enforces, so
		// operators can match it against identity's history.
		slog.Info("public catalog policy enforced", "open", policy.Open, "applied_version", policy.AppliedVersion, "desired_version", policy.DesiredVersion)
		g.lastSeen = policy.AppliedVersion
	}
	return policy, nil
}
