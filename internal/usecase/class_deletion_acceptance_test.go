package usecase_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Opt-in browser fixture. Application handlers and repositories are real;
// identity/session/permission responses are controlled synthetic test doubles.
func TestClassDeletionAcceptanceStack(t *testing.T) {
	path := os.Getenv("KEL206_STACK_CONFIG")
	if path == "" {
		t.Skip("KEL206_STACK_CONFIG not set")
	}
	f := newDeletionFixture(t)
	class, slot := f.seed(t, "KEL206 browser target")
	keep, _ := f.seed(t, "KEL206 retained class")
	now := time.Now().UTC().Truncate(24 * time.Hour)
	for _, day := range []int{-2, 2} {
		if err := f.db.Exec("INSERT INTO class_sessions(id,class_id,schedule_id,tutor_id,session_date,start_time,end_time,status) VALUES(?,?,?,?,?,'09:00','10:00','scheduled')", uuid.New(), class, slot, uuid.New(), now.AddDate(0, 0, day)).Error; err != nil {
			t.Fatal(err)
		}
	}
	permissions := []string{"class:read", "class:delete", "category:read", "schedule:read", "member:read"}
	identity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/members/me/membership" {
			json.NewEncoder(w).Encode(gin.H{"status": "success", "data": gin.H{"member_id": uuid.New(), "role_id": f.role, "role_name": "Creator", "permissions": permissions}})
			return
		}
		w.WriteHeader(204)
	}))
	defer identity.Close()
	done := make(chan struct{}, 1)
	f.router.POST("/test/shutdown", func(c *gin.Context) { done <- struct{}{}; c.Status(204) })
	f.router.GET("/test/state", func(c *gin.Context) {
		var classes, schedules []map[string]any
		f.db.Table("classes").Order("name").Find(&classes)
		f.db.Table("class_schedules").Find(&schedules)
		var sessions []map[string]any
		f.db.Table("class_sessions").Find(&sessions)
		c.JSON(200, gin.H{"classes": classes, "schedules": schedules, "sessions": sessions})
	})
	academic := httptest.NewServer(f.router)
	defer academic.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cmd := exec.Command(os.Getenv("KEL196_GATEWAY_BINARY"))
	cmd.Dir = t.TempDir()
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "JWT_SECRET=" + deletionTestSecret, "PORT=" + strconv.Itoa(port), "APP_URL=http://localhost:3216", "IDENTITY_SERVICE_URL=" + identity.URL, "ACADEMIC_SERVICE_URL=" + academic.URL, "BILLING_SERVICE_URL=" + identity.URL, "REDIS_HOST=127.0.0.1", "REDIS_PORT=55497", "GIN_MODE=release"}
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Signal(os.Interrupt); cmd.Wait() }()
	gateway := "http://127.0.0.1:" + strconv.Itoa(port)
	ready := false
	for n := 0; n < 100; n++ {
		res, err := http.Get(gateway + "/health")
		if err == nil {
			res.Body.Close()
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("gateway unavailable")
	}
	config := map[string]string{"gateway": gateway, "academic": academic.URL, "token": f.token, "tenant": f.tenant.String(), "class": class.String(), "schedule": slot.String(), "keep": keep.String()}
	raw, _ := json.Marshal(config)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	fmt.Println("KEL206 isolated deletion acceptance ready")
	select {
	case <-done:
	case <-time.After(20 * time.Minute):
		t.Fatal("acceptance fixture timed out")
	}
}
