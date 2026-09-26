package grpcclient

import (
	"context"
	"errors"
	"math"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

const catalogPolicyServiceGetPath = "/tenant.CatalogPolicyService/GetPublicCatalogPolicy"

// ErrInvalidCatalogPolicy reports an identity answer that cannot be trusted:
// a missing or non-boolean "open", or a non-integral version.
var ErrInvalidCatalogPolicy = errors.New("invalid public catalog policy response")

// CatalogPolicy is the effective public catalog policy identity decided
// (KEL-98): Open plus the applied and desired control-plane versions.
type CatalogPolicy struct {
	Open           bool
	AppliedVersion int64
	DesiredVersion int64
}

type CatalogPolicyClient interface {
	GetPublicCatalogPolicy(ctx context.Context) (CatalogPolicy, error)
	Close() error
}

type catalogPolicyClient struct {
	conn    *grpc.ClientConn
	timeout time.Duration
}

// NewCatalogPolicyClient creates a lazily connected client for identity's
// CatalogPolicyService. Like PermissionClient it uses the structpb contract
// (the repos carry no .proto sources) and bounds each call by timeout.
func NewCatalogPolicyClient(target string, timeout time.Duration) (CatalogPolicyClient, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &catalogPolicyClient{conn: conn, timeout: timeout}, nil
}

func (c *catalogPolicyClient) GetPublicCatalogPolicy(ctx context.Context) (CatalogPolicy, error) {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	resp := new(structpb.Struct)
	if err := c.conn.Invoke(ctx, catalogPolicyServiceGetPath, &structpb.Struct{}, resp, grpc.StaticMethod()); err != nil {
		return CatalogPolicy{}, err
	}
	return ParseCatalogPolicy(resp)
}

// ParseCatalogPolicy is strict on purpose: anything other than an explicit
// boolean "open" is an error, so the caller hides the catalog instead of
// treating a malformed or empty answer as open.
func ParseCatalogPolicy(resp *structpb.Struct) (CatalogPolicy, error) {
	fields := resp.GetFields()
	open, ok := fields["open"]
	if !ok {
		return CatalogPolicy{}, ErrInvalidCatalogPolicy
	}
	if _, isBool := open.GetKind().(*structpb.Value_BoolValue); !isBool {
		return CatalogPolicy{}, ErrInvalidCatalogPolicy
	}
	policy := CatalogPolicy{Open: open.GetBoolValue()}
	for key, target := range map[string]*int64{"applied_version": &policy.AppliedVersion, "desired_version": &policy.DesiredVersion} {
		value, present := fields[key]
		if !present {
			return CatalogPolicy{}, ErrInvalidCatalogPolicy
		}
		number, isNumber := value.GetKind().(*structpb.Value_NumberValue)
		if !isNumber || number.NumberValue < 0 || number.NumberValue != math.Trunc(number.NumberValue) {
			return CatalogPolicy{}, ErrInvalidCatalogPolicy
		}
		*target = int64(number.NumberValue)
	}
	// Version 0 is the seeded default. Once a desired version exists,
	// an open answer without a corresponding applied version is untrusted.
	if policy.Open && policy.DesiredVersion > 0 && policy.AppliedVersion == 0 {
		return CatalogPolicy{}, ErrInvalidCatalogPolicy
	}
	if policy.AppliedVersion > policy.DesiredVersion {
		return CatalogPolicy{}, ErrInvalidCatalogPolicy
	}
	return policy, nil
}

func (c *catalogPolicyClient) Close() error { return c.conn.Close() }
