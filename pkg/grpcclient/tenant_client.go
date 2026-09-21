package grpcclient

import (
	"context"
	"time"

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
	conn    *grpc.ClientConn
	client  pb.TenantServiceClient
	timeout time.Duration
}

func NewTenantClient(target string, timeout time.Duration) (TenantClient, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	client := pb.NewTenantServiceClient(conn)
	return &tenantClient{
		conn:    conn,
		client:  client,
		timeout: timeout,
	}, nil
}

// call bounds every identity call so a hung identity cannot hold a catalog request
// open indefinitely. A zero timeout keeps the previous unbounded behaviour.
func (c *tenantClient) call(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.timeout)
}

func (c *tenantClient) ValidateTenantStatus(ctx context.Context, tenantID string) (bool, string, error) {
	callCtx, cancel := c.call(ctx)
	defer cancel()
	resp, err := c.client.ValidateTenantStatus(callCtx, &pb.ValidateTenantRequest{TenantId: tenantID})
	if err != nil {
		return false, "", err
	}
	return resp.GetIsActive(), resp.GetMessage(), nil
}

func (c *tenantClient) GetTenantPublicInfo(ctx context.Context, tenantIDs []string) (map[string]TenantPublicInfo, error) {
	callCtx, cancel := c.call(ctx)
	defer cancel()
	resp, err := c.client.GetTenantPublicInfo(callCtx, &pb.TenantPublicInfoRequest{TenantIds: tenantIDs})
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
