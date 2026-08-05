package usecase

import (
	"context"
	"math"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

type CatalogUsecase interface {
	ListClasses(ctx context.Context, query domain.CatalogQuery) (*domain.CatalogListResponse, error)
	GetClass(ctx context.Context, id uuid.UUID) (*domain.CatalogItem, error)
}

func (u *catalogUsecase) GetClass(ctx context.Context, id uuid.UUID) (*domain.CatalogItem, error) {
	ids, err := u.repo.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}
	stringIDs := make([]string, len(ids))
	for i, tenantID := range ids {
		stringIDs[i] = tenantID.String()
	}
	info, err := u.tenantClient.GetTenantPublicInfo(ctx, stringIDs)
	if err != nil {
		return nil, err
	}
	snapshots := make([]domain.TenantLocationSnapshot, 0, len(info))
	for _, tenant := range info {
		snapshots = append(snapshots, domain.TenantLocationSnapshot{TenantID: mustUUID(tenant.ID), Name: tenant.Name, AddressFormatted: stringPtr(tenant.AddressFormatted), Latitude: floatPtr(tenant.Latitude, tenant.HasLocation), Longitude: floatPtr(tenant.Longitude, tenant.HasLocation), IsActive: tenant.IsActive})
	}
	if err := u.repo.UpsertTenantSnapshots(ctx, snapshots); err != nil {
		return nil, err
	}
	return u.repo.GetByID(ctx, id)
}

type catalogUsecase struct {
	repo         domain.CatalogRepository
	tenantClient grpcclient.TenantClient
}

func NewCatalogUsecase(repo domain.CatalogRepository, tenantClient grpcclient.TenantClient) CatalogUsecase {
	return &catalogUsecase{repo: repo, tenantClient: tenantClient}
}

func (u *catalogUsecase) ListClasses(ctx context.Context, query domain.CatalogQuery) (*domain.CatalogListResponse, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}
	ids, err := u.repo.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}
	stringIDs := make([]string, len(ids))
	for i, id := range ids {
		stringIDs[i] = id.String()
	}
	info, err := u.tenantClient.GetTenantPublicInfo(ctx, stringIDs)
	if err != nil {
		return nil, err
	}
	snapshots := make([]domain.TenantLocationSnapshot, 0, len(info))
	for _, tenant := range info {
		snapshots = append(snapshots, domain.TenantLocationSnapshot{TenantID: mustUUID(tenant.ID), Name: tenant.Name, AddressFormatted: stringPtr(tenant.AddressFormatted), Latitude: floatPtr(tenant.Latitude, tenant.HasLocation), Longitude: floatPtr(tenant.Longitude, tenant.HasLocation), IsActive: tenant.IsActive})
	}
	if err := u.repo.UpsertTenantSnapshots(ctx, snapshots); err != nil {
		return nil, err
	}
	items, total, err := u.repo.List(ctx, query)
	if err != nil {
		return nil, err
	}
	return &domain.CatalogListResponse{Items: items, Pagination: domain.Pagination{Page: query.Page, PageSize: query.PageSize, TotalItems: total, TotalPages: int(math.Ceil(float64(total) / float64(query.PageSize)))}}, nil
}
func mustUUID(value string) (id uuid.UUID) { id, _ = uuid.Parse(value); return }
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
