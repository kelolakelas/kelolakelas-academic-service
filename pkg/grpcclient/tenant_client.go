package grpcclient

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/kelolakelas/kelolakelas-academic-service/pkg/proto/tenant"
)

type TenantClient interface {
	ValidateTenantStatus(ctx context.Context, tenantID string) (bool, string, error)
	GetTenantPublicInfo(ctx context.Context, tenantIDs []string) (map[string]TenantPublicInfo, error)
	Close() error
}

type TenantPublicInfo struct {
	ID, Name, AddressFormatted string
	Latitude, Longitude        float64
	HasLocation, IsActive      bool
}

type tenantClient struct {
	conn   *grpc.ClientConn
	client pb.TenantServiceClient
}

func NewTenantClient(target string) (TenantClient, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	client := pb.NewTenantServiceClient(conn)
	return &tenantClient{
		conn:   conn,
		client: client,
	}, nil
}

func (c *tenantClient) ValidateTenantStatus(ctx context.Context, tenantID string) (bool, string, error) {
	resp, err := c.client.ValidateTenantStatus(ctx, &pb.ValidateTenantRequest{TenantId: tenantID})
	if err != nil {
		return false, "", err
	}
	return resp.GetIsActive(), resp.GetMessage(), nil
}

func (c *tenantClient) GetTenantPublicInfo(ctx context.Context, tenantIDs []string) (map[string]TenantPublicInfo, error) {
	resp, err := c.client.GetTenantPublicInfo(ctx, &pb.TenantPublicInfoRequest{TenantIds: tenantIDs})
	if err != nil {
		return nil, err
	}
	result := make(map[string]TenantPublicInfo, len(resp.GetTenants()))
	for _, tenant := range resp.GetTenants() {
		result[tenant.GetTenantId()] = TenantPublicInfo{ID: tenant.GetTenantId(), Name: tenant.GetName(), AddressFormatted: tenant.GetAddressFormatted(), Latitude: tenant.GetLatitude(), Longitude: tenant.GetLongitude(), HasLocation: tenant.GetHasLocation(), IsActive: tenant.GetIsActive()}
	}
	return result, nil
}

func (c *tenantClient) Close() error {
	return c.conn.Close()
}
