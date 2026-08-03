package handler

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

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
