package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestEnrollmentScope(t *testing.T) {
	userID, tenantID := uuid.New(), uuid.New()
	parent := true
	tenantUser := false
	tests := []struct {
		name       string
		user       string
		tenant     string
		isParent   *bool
		wantTenant *uuid.UUID
		wantParent *uuid.UUID
		wantErr    bool
	}{
		{name: "parent without tenant claim", user: userID.String(), isParent: &parent, wantParent: &userID},
		{name: "parent with zero tenant claim", user: userID.String(), tenant: uuid.Nil.String(), isParent: &parent, wantParent: &userID},
		{name: "parent with valid tenant claim", user: userID.String(), tenant: tenantID.String(), isParent: &parent, wantParent: &userID},
		{name: "tenant user with valid tenant", user: userID.String(), tenant: tenantID.String(), isParent: &tenantUser, wantTenant: &tenantID},
		{name: "tenant user without tenant claim", user: userID.String(), isParent: &tenantUser, wantErr: true},
		{name: "tenant user with zero tenant claim", user: userID.String(), tenant: uuid.Nil.String(), isParent: &tenantUser, wantErr: true},
		{name: "tenant user with invalid tenant claim", user: userID.String(), tenant: "bad", isParent: &tenantUser, wantErr: true},
		{name: "invalid user id", user: "bad", tenant: tenantID.String(), isParent: &tenantUser, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := newStudentHandlerContext(test.user, test.tenant, test.isParent)
			gotTenant, gotParent, err := enrollmentScope(c)
			if (err != nil) != test.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, test.wantErr)
			}
			if !sameUUIDPointer(gotTenant, test.wantTenant) || !sameUUIDPointer(gotParent, test.wantParent) {
				t.Fatalf("scope=(tenant %v, parent %v), want=(tenant %v, parent %v)", gotTenant, gotParent, test.wantTenant, test.wantParent)
			}
		})
	}
}

func TestParseEnrollmentQuery(t *testing.T) {
	tests := []struct {
		name, rawQuery string
		wantErr        bool
	}{
		{name: "valid", rawQuery: "page=1&page_size=20&status=active&date_from=2026-01-01", wantErr: false},
		{name: "invalid status", rawQuery: "status=paused", wantErr: true},
		{name: "invalid uuid", rawQuery: "student_id=bad", wantErr: true},
		{name: "invalid page size", rawQuery: "page_size=101", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/api/v1/enrollments?"+test.rawQuery, nil)
			_, err := parseEnrollmentQuery(c)
			if (err != nil) != test.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, test.wantErr)
			}
		})
	}
}
