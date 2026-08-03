package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParseSessionQuery(t *testing.T) {
	tests := []struct {
		name, rawQuery string
		wantErr        bool
	}{
		{name: "defaults", rawQuery: "", wantErr: false},
		{name: "valid filters", rawQuery: "page=2&page_size=10&status=completed&date_from=2026-01-01&date_to=2026-01-31", wantErr: false},
		{name: "invalid status", rawQuery: "status=unknown", wantErr: true},
		{name: "invalid date", rawQuery: "date_from=not-a-date", wantErr: true},
		{name: "reversed dates", rawQuery: "date_from=2026-02-01&date_to=2026-01-01", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/api/v1/sessions?"+test.rawQuery, nil)
			_, err := parseSessionQuery(c)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, test.wantErr)
			}
		})
	}
}
