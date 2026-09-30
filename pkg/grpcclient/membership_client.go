package grpcclient

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

const membershipServiceCheckActiveMemberPath = "/tenant.MembershipService/CheckActiveMembership"

// ErrInvalidMembershipResponse reports an identity answer that cannot be trusted:
// a missing or non-boolean "active". It fails closed: the caller must reject the
// substitution instead of guessing that the tutor is eligible.
var ErrInvalidMembershipResponse = errors.New("invalid membership response")

// MembershipClient asks identity whether a member row is active in a tenant.
// It follows the same structpb contract style as PermissionClient because the
// repos carry no .proto sources.
type MembershipClient interface {
	CheckActiveMember(ctx context.Context, tenantID, memberID string) (bool, error)
	Close() error
}

type membershipClient struct {
	conn    *grpc.ClientConn
	timeout time.Duration
}

// NewMembershipClient creates a lazily connected client for target. Each check
// is bounded by timeout when it is positive, so a slow or silent identity
// turns into an error instead of holding the request open.
func NewMembershipClient(target string, timeout time.Duration) (MembershipClient, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &membershipClient{conn: conn, timeout: timeout}, nil
}

func (c *membershipClient) CheckActiveMember(ctx context.Context, tenantID, memberID string) (bool, error) {
	// The deadline is derived from the caller's context, so a request the
	// client already cancelled still ends immediately; a hung identity ends
	// at the deadline, and the caller reports either error as unavailable.
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	req, err := structpb.NewStruct(map[string]interface{}{
		"tenant_id": tenantID,
		"member_id": memberID,
	})
	if err != nil {
		return false, err
	}
	resp := new(structpb.Struct)
	if err := c.conn.Invoke(ctx, membershipServiceCheckActiveMemberPath, req, resp, grpc.StaticMethod()); err != nil {
		return false, err
	}
	return ParseActiveMember(resp)
}

// ParseActiveMember is strict on purpose: anything other than an explicit
// boolean "active" is an error, so the caller rejects the substitution
// instead of treating a malformed or empty answer as eligible.
func ParseActiveMember(resp *structpb.Struct) (bool, error) {
	value, ok := resp.GetFields()["active"]
	if !ok {
		return false, ErrInvalidMembershipResponse
	}
	if _, isBool := value.GetKind().(*structpb.Value_BoolValue); !isBool {
		return false, ErrInvalidMembershipResponse
	}
	return value.GetBoolValue(), nil
}

func (c *membershipClient) Close() error { return c.conn.Close() }
