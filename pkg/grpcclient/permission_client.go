package grpcclient

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

const permissionServiceCheckPermissionPath = "/tenant.PermissionService/CheckPermission"

type PermissionClient interface {
	// CheckPermission asks identity whether roleID grants permission while operating on
	// tenantID. The tenant is part of the question because identity only accepts a role that
	// belongs to that tenant or is a system role, so a role lifted from another tenant can
	// never satisfy the check. memberID is the membership the verified token was issued
	// for (KEL-80): identity pins the check to that membership, so a member who was
	// removed or moved to another role is denied even while the token is still valid.
	CheckPermission(ctx context.Context, tenantID, roleID, memberID, permission string) (bool, error)
	Close() error
}

type permissionClient struct {
	conn    *grpc.ClientConn
	timeout time.Duration
}

// NewPermissionClient creates a lazily connected client for target. Each check is bounded
// by timeout when it is positive, so a slow or silent identity turns into an error instead
// of holding the request open.
func NewPermissionClient(target string, timeout time.Duration) (PermissionClient, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &permissionClient{conn: conn, timeout: timeout}, nil
}

func (c *permissionClient) CheckPermission(ctx context.Context, tenantID, roleID, memberID, permission string) (bool, error) {
	// The deadline is derived from the caller's context, so a request the client already
	// cancelled still ends immediately; a hung identity ends at the deadline, and the
	// middleware reports either error as 503.
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	// tenant_id is sent alongside the existing role_id and permission keys, so identity
	// deployments that only understand the older contract keep working during the ADR 0002
	// transition window. member_id follows the same rule: an identity that predates it
	// ignores the field and keeps answering from the role alone.
	req, err := structpb.NewStruct(map[string]interface{}{
		"role_id":    roleID,
		"permission": permission,
		"tenant_id":  tenantID,
		"member_id":  memberID,
	})
	if err != nil {
		return false, err
	}
	resp := new(structpb.Struct)
	if err := c.conn.Invoke(ctx, permissionServiceCheckPermissionPath, req, resp, grpc.StaticMethod()); err != nil {
		return false, err
	}
	value, ok := resp.GetFields()["allowed"]
	return ok && value.GetBoolValue(), nil
}

func (c *permissionClient) Close() error { return c.conn.Close() }
