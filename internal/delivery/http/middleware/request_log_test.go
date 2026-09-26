package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/metadata"
)

func TestRequestLogCorrelationAndRedaction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	r := gin.New()
	r.Use(RequestLog())
	r.GET("/private/:id", func(c *gin.Context) {
		if RequestID(c.Request.Context()) != c.GetHeader("X-Request-ID") {
			t.Error("missing request context ID")
		}
		md, _ := metadata.FromOutgoingContext(OutgoingContext(c.Request.Context()))
		if md.Get("x-request-id")[0] != "trace-1" {
			t.Error("missing gRPC request ID")
		}
		c.Status(http.StatusForbidden)
	})
	req := httptest.NewRequest(http.MethodGet, "/private/secret-path?password=hidden-query", nil)
	req.Header.Set("X-Request-ID", "trace-1")
	req.Header.Set("Authorization", "Bearer hidden-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || w.Header().Get("X-Request-ID") != "trace-1" {
		t.Fatalf("response = %d %q", w.Code, w.Header().Get("X-Request-ID"))
	}
	for _, fragment := range []string{"trace-1", `"status":403`, `"route":"/private/:id"`} {
		if !strings.Contains(logs.String(), fragment) {
			t.Errorf("missing %s in access log", fragment)
		}
	}
	for _, secret := range []string{"hidden-token", "hidden-query", "secret-path"} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("sensitive value logged: %s", secret)
		}
	}
}

func TestRequestLogCatalogReadsAndEnrollmentWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	r := gin.New()
	r.Use(RequestLog())
	r.GET("/api/v1/catalog/classes", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/api/v1/catalog/classes/:id", func(c *gin.Context) {
		if c.Param("id") == "failure" {
			c.Status(http.StatusForbidden)
			return
		}
		c.Status(http.StatusOK)
	})
	r.POST("/api/v1/catalog/classes/:class_id/enrollments", func(c *gin.Context) { c.Status(http.StatusCreated) })
	r.PUT("/internal/enrollments/:id/activate", func(c *gin.Context) { c.Status(http.StatusOK) })

	for _, tc := range []struct {
		name, method, path string
		status             int
		logged             bool
	}{
		{"catalog list", http.MethodGet, "/api/v1/catalog/classes", http.StatusOK, false},
		{"catalog detail", http.MethodGet, "/api/v1/catalog/classes/private", http.StatusOK, false},
		{"catalog error", http.MethodGet, "/api/v1/catalog/classes/failure", http.StatusForbidden, true},
		{"enrollment created", http.MethodPost, "/api/v1/catalog/classes/private/enrollments", http.StatusCreated, true},
		{"internal activation", http.MethodPut, "/internal/enrollments/private/activate", http.StatusOK, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs.Reset()
			req := httptest.NewRequest(tc.method, tc.path+"?secret=hidden-query", nil)
			req.Header.Set("X-Request-ID", "trace-catalog")
			req.Header.Set("Authorization", "Bearer hidden-token")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.status || w.Header().Get("X-Request-ID") != "trace-catalog" {
				t.Fatalf("response = %d, id = %q", w.Code, w.Header().Get("X-Request-ID"))
			}
			if got := logs.Len() > 0; got != tc.logged {
				t.Fatalf("logged = %v, want %v: %s", got, tc.logged, logs.String())
			}
			if tc.logged {
				for _, fragment := range []string{`"request_id":"trace-catalog"`, `"status":` + strconv.Itoa(tc.status), `"method":"` + tc.method + `"`} {
					if !strings.Contains(logs.String(), fragment) {
						t.Errorf("missing %s in access log: %s", fragment, logs.String())
					}
				}
			}
			for _, secret := range []string{"hidden-token", "hidden-query", "private"} {
				if strings.Contains(logs.String(), secret) {
					t.Errorf("sensitive value logged: %s", secret)
				}
			}
		})
	}
}

func TestRequestLogGeneratesSafeID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestLog())
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
	for _, input := range []string{"", "bad\nheader", strings.Repeat("x", 65)} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if input != "" {
			req.Header.Set("X-Request-ID", input)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if id := w.Header().Get("X-Request-ID"); !validRequestID(id) || id == input {
			t.Errorf("unsafe generated ID %q for %q", id, input)
		}
	}
}
