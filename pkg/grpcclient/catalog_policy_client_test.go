package grpcclient

import (
	"errors"
	"google.golang.org/protobuf/types/known/structpb"
	"testing"
)

func TestParseCatalogPolicyRejectsUntrustedAnswers(t *testing.T) {
	cases := []struct {
		name      string
		fields    map[string]interface{}
		wantError bool
	}{
		{"seeded default", map[string]interface{}{"open": true, "applied_version": 0, "desired_version": 0}, false},
		{"applied closed", map[string]interface{}{"open": false, "applied_version": 2, "desired_version": 2}, false},
		{"missing open", map[string]interface{}{"applied_version": 0}, true},
		{"string open", map[string]interface{}{"open": "true"}, true},
		{"unapplied open", map[string]interface{}{"open": true, "applied_version": 0, "desired_version": 1}, true},
		{"version mismatch", map[string]interface{}{"open": true, "applied_version": 2, "desired_version": 1}, true},
		{"fractional version", map[string]interface{}{"open": true, "applied_version": 1.5, "desired_version": 2}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, err := structpb.NewStruct(tc.fields)
			if err != nil {
				t.Fatal(err)
			}
			policy, err := ParseCatalogPolicy(msg)
			if tc.wantError && !errors.Is(err, ErrInvalidCatalogPolicy) {
				t.Fatalf("error=%v", err)
			}
			if !tc.wantError && (err != nil || policy.Open != tc.fields["open"]) {
				t.Fatalf("policy=%+v err=%v", policy, err)
			}
		})
	}
}
