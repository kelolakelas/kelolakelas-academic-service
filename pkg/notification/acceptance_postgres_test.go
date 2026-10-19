package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/handler"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Exercise the real handler -> usecase -> database trigger -> worker -> HTTP
// transport flow. Only verified tenant context and external providers are doubles.
func TestParentNotificationHTTPAcceptancePostgres(t *testing.T) {
	dsn := os.Getenv("KEL155_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL155_TEST_DSN not set")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*cfg)
	defer admin.Close()
	schema := "kel155_http_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP SCHEMA " + schema + " CASCADE")
	cfg.RuntimeParams["search_path"] = schema + ",public"
	pool := stdlib.OpenDB(*cfg)
	defer pool.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"00000000000000_init_schema", "00001790812223_add_rescheduled_from_to_class_sessions", "00001790900002_parent_notifications", "00001790900003_session_reminders"} {
		data, err := os.ReadFile("../../migrations/" + name + ".up.sql")
		if err != nil {
			t.Fatal(err)
		}
		// Keep global extension lifecycle out of this private test schema.
		exec(strings.ReplaceAll(string(data), "CREATE EXTENSION IF NOT EXISTS pgcrypto;", ""))
	}
	tenant, parent, cat, class, schedule, session, student, enrollment := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec("INSERT INTO categories(id,tenant_id,name) VALUES (?,?,'Math')", cat, tenant)
	exec("INSERT INTO classes(id,tenant_id,category_id,name,type,price) VALUES (?,?,?,'Math','group',100)", class, tenant, cat)
	exec("INSERT INTO students(id,parent_id,first_name) VALUES (?,?,'Ani')", student, parent)
	exec("INSERT INTO class_schedules(id,class_id,capacity,day_of_week,start_time,end_time) VALUES (?,?,10,1,'09:00','10:00')", schedule, class)
	exec("INSERT INTO enrollments(id,tenant_id,class_id,student_id,schedule_id,status,parent_email) VALUES (?,?,?,?,?,'active','parent@example.test')", enrollment, tenant, class, student, schedule)
	exec("INSERT INTO class_sessions(id,class_id,schedule_id,tutor_id,session_date,start_time,end_time) VALUES (?,?,?,?,'2026-10-20','09:00','10:00')", session, class, schedule, uuid.New())
	uc := usecase.NewScheduleUsecase(repository.NewTransactionManager(db), repository.NewClassRepository(db), repository.NewScheduleRepository(db), repository.NewSessionRepository(db), repository.NewEnrollmentRepository(db))
	h := handler.NewScheduleHandler(uc)
	r := gin.New()
	currentTenant := uuid.New()
	r.POST("/reschedule", func(c *gin.Context) { c.Set("tenant_id", currentTenant.String()); h.RescheduleSession(c) })
	payload := fmt.Sprintf(`{"session_id":%q,"new_session_date":"2026-10-21T00:00:00Z","new_start_time":"11:00:00","new_end_time":"12:00:00"}`, session.String())
	request := func() int {
		req := httptest.NewRequest("POST", "/reschedule", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	if request() != 404 {
		t.Fatal("foreign tenant accepted")
	}
	currentTenant = tenant
	if request() != 200 {
		t.Fatal("owned reschedule failed")
	}
	// Provider accepts the first request but its reply is lost/5xx. Persistent
	// idempotency below represents provider-side deduplication after worker restart.
	seen := map[string][]byte{}
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var data map[string]any
		if json.NewDecoder(req.Body).Decode(&data) != nil {
			t.Error("bad provider payload")
			w.WriteHeader(400)
			return
		}
		key := req.Header.Get("Idempotency-Key")
		if req.URL.Path == "/internal/notifications" {
			key, _ = data["idempotency_key"].(string)
			if data["tenant_id"] != tenant.String() || data["parent_user_id"] != parent.String() {
				t.Error("recipient mismatch")
			}
		}
		encoded, _ := json.Marshal(data)
		calls[key]++
		if old, ok := seen[key]; ok && !bytes.Equal(old, encoded) {
			t.Error("retry changed payload")
		}
		seen[key] = encoded
		if calls[key] == 1 {
			w.WriteHeader(503)
			return
		}
		if req.URL.Path == "/emails" {
			fmt.Fprint(w, `{"id":"email"}`)
		} else {
			fmt.Fprint(w, `{"data":{"id":"message"}}`)
		}
	}))
	defer server.Close()
	client := NewClient(server.URL, "test-internal-credential", "test-resend-key")
	client.resendURL = server.URL + "/emails"
	store := repository.NewParentNotificationRepository(db)
	if err := usecase.NewParentNotificationWorker(store, client, "sender@example.test", time.Second).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE parent_notification_outbox SET available_at=now() WHERE status='pending'")
	if err := usecase.NewParentNotificationWorker(repository.NewParentNotificationRepository(db), client, "changed@example.test", time.Second).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var sent int64
	db.Table("parent_notification_outbox").Where("status='sent'").Count(&sent)
	if sent != 2 || len(seen) != 2 {
		t.Fatalf("sent=%d unique provider messages=%d", sent, len(seen))
	}
	for _, n := range calls {
		if n != 2 {
			t.Fatal("retry path not exercised")
		}
	}
	// KEL-156 uses the same transport/dispatcher, including uncertain replies.
	from := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	reminders := repository.NewSessionReminderRepository(db)
	batch, err := reminders.EnqueueBatch(context.Background(), from, from.AddDate(0, 0, 1), 100)
	if err != nil || batch.EnrollmentsMarked != 1 || batch.NotificationsQueued != 2 {
		t.Fatalf("reminders=%+v err=%v", batch, err)
	}
	for range 2 {
		if err := usecase.NewParentNotificationWorker(store, client, "sender@example.test", time.Second).RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		exec("UPDATE parent_notification_outbox SET available_at=now() WHERE status='pending'")
	}
	if batch, err = reminders.EnqueueBatch(context.Background(), from, from.AddDate(0, 0, 1), 100); err != nil || batch.NotificationsQueued != 0 {
		t.Fatal("reminder replay queued twice", err)
	}
	db.Table("parent_notification_outbox").Where("status='sent'").Count(&sent)
	if sent != 4 || len(seen) != 4 {
		t.Fatalf("sent=%d unique provider messages=%d", sent, len(seen))
	}
	for _, n := range calls {
		if n != 2 {
			t.Fatal("reminder transport retry changed")
		}
	}
}
