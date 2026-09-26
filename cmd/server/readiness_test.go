package main

import (
	"net"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestReadinessAndLiveness(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(srv, hs)
	go srv.Serve(lis)
	defer srv.Stop()
	r := gin.New()
	r.GET("/health", healthHandler("academic-service"))
	r.GET("/ready", readinessHandler(db, lis.Addr().String()))
	mock.ExpectPing()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/ready", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"identity_grpc":"healthy"`) {
		t.Fatalf("ready: %d %s", w.Code, w.Body.String())
	}
	hs.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	mock.ExpectPing()
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/ready", nil))
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"identity_grpc":"unavailable"`) {
		t.Fatalf("grpc down: %d %s", w.Code, w.Body.String())
	}
	mock.ExpectPing().WillReturnError(net.ErrClosed)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/ready", nil))
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"database":"unavailable"`) {
		t.Fatalf("db down: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 200 {
		t.Fatalf("liveness: %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
