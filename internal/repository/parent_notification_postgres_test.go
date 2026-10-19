package repository

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type notificationFixture struct {
	db                                                    *gorm.DB
	tenant, class, schedule, session, parent, otherParent uuid.UUID
	enrollments                                           []uuid.UUID
}

func newNotificationFixture(t *testing.T) *notificationFixture {
	t.Helper()
	dsn := os.Getenv("KEL155_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL155_TEST_DSN not set")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { admin.Close() })
	schema := "kel155_" + uuid.New().String()[:8]
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schema + " CASCADE") })
	cfg.RuntimeParams["search_path"] = schema + ",public"
	pool := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"00000000000000_init_schema", "00001790812223_add_rescheduled_from_to_class_sessions", "00001790900002_parent_notifications", "00001790900003_session_reminders"} {
		sql, err := os.ReadFile("../../migrations/" + name + ".up.sql")
		if err != nil {
			t.Fatal(err)
		}
		// PostgreSQL 15 has gen_random_uuid built in; do not create/drop a global
		// extension in a private schema while other packages initialize theirs.
		sql = []byte(strings.ReplaceAll(string(sql), "CREATE EXTENSION IF NOT EXISTS pgcrypto;", ""))
		if err = db.Exec(string(sql)).Error; err != nil {
			t.Fatal(err)
		}
	}
	f := &notificationFixture{db: db, tenant: uuid.New(), class: uuid.New(), schedule: uuid.New(), session: uuid.New(), parent: uuid.New(), otherParent: uuid.New()}
	cat := uuid.New()
	f.exec(t, "INSERT INTO categories(id,tenant_id,name) VALUES (?,?,?)", cat, f.tenant, "Math")
	f.exec(t, "INSERT INTO classes(id,tenant_id,category_id,name,type,price) VALUES (?,?,?,?,?,?)", f.class, f.tenant, cat, "Math group", "group", 100)
	f.exec(t, "INSERT INTO class_schedules(id,class_id,day_of_week,start_time,end_time) VALUES (?,?,1,'09:00','10:00')", f.schedule, f.class)
	f.exec(t, "INSERT INTO class_sessions(id,class_id,schedule_id,tutor_id,session_date,start_time,end_time) VALUES (?,?,?,?,'2026-10-20','09:00','10:00')", f.session, f.class, f.schedule, uuid.New())
	parents := []uuid.UUID{f.parent, f.parent, f.otherParent, uuid.New(), uuid.New()}
	for i, p := range parents {
		student, e := uuid.New(), uuid.New()
		f.enrollments = append(f.enrollments, e)
		f.exec(t, "INSERT INTO students(id,parent_id,first_name,last_name) VALUES (?,?,?,'PRIVATE-LAST-NAME')", student, p, []string{"Ani", "Budi", "Cici", "Dedi", "Eka"}[i])
		status := "active"
		email := "parent@example.test"
		if i == 2 {
			email = ""
		}
		if i == 3 {
			status = "suspended"
		}
		if i == 4 {
			status = "pending"
		}
		f.exec(t, "INSERT INTO enrollments(id,tenant_id,student_id,class_id,schedule_id,status,parent_email) VALUES (?,?,?,?,?,?,?)", e, f.tenant, student, f.class, f.schedule, status, email)
	}
	return f
}
func (f *notificationFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := f.db.Exec(sql, args...).Error; err != nil {
		t.Fatal(err)
	}
}
func (f *notificationFixture) count(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := f.db.Table("parent_notification_outbox").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestParentNotificationAtomicScopeAndTransitions(t *testing.T) {
	f := newNotificationFixture(t)
	tx := f.db.Begin()
	if err := tx.Exec("UPDATE class_sessions SET tutor_id=? WHERE id=?", uuid.New(), f.session).Error; err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	if n := f.count(t); n != 0 {
		t.Fatalf("rollback left %d notifications", n)
	}
	f.exec(t, "UPDATE class_sessions SET tutor_id=? WHERE id=?", uuid.New(), f.session)
	if n := f.count(t); n != 3 {
		t.Fatalf("want 2 chat + 1 email, got %d", n)
	}
	var bad int64
	f.db.Table("parent_notification_outbox").Where("body LIKE '%PRIVATE-LAST-NAME%' OR body LIKE '%Dedi%' OR body LIKE '%Eka%' OR tenant_id<>?", f.tenant).Count(&bad)
	if bad != 0 {
		t.Fatal("private/inactive/foreign recipient data leaked")
	}
	var rows []struct {
		ParentID uuid.UUID
		Body     string
		Channel  string
	}
	f.db.Table("parent_notification_outbox").Find(&rows)
	for _, r := range rows {
		if r.ParentID == f.parent && (!strings.Contains(r.Body, "Ani, Budi") || !strings.Contains(r.Body, "WIB")) {
			t.Fatalf("siblings not grouped: %s", r.Body)
		}
	}
	// Same tutor is a no-op; an ordinary generated session is not a reschedule.
	f.exec(t, "UPDATE class_sessions SET tutor_id=tutor_id WHERE id=?", f.session)
	if f.count(t) != 3 {
		t.Fatal("no-op produced event")
	}
	replacement := uuid.New()
	f.exec(t, "UPDATE class_sessions SET status='rescheduled' WHERE id=?", f.session)
	f.exec(t, "INSERT INTO class_sessions(id,class_id,rescheduled_from_session_id,tutor_id,session_date,start_time,end_time) VALUES (?,?,?,?,'2026-10-21','11:00','12:00')", replacement, f.class, f.session, uuid.New())
	if f.count(t) != 6 {
		t.Fatal("group replacement recipients wrong")
	}
	f.db.Table("parent_notification_outbox").Where("body LIKE '%20-10-2026 09:00:00%' AND body LIKE '%21-10-2026 11:00:00%'").Count(&bad)
	if bad != 3 {
		t.Fatal("old/new WIB times missing")
	}
	// Replacement of a replacement follows the root schedule.
	f.exec(t, "UPDATE class_sessions SET status='rescheduled' WHERE id=?", replacement)
	second := uuid.New()
	f.exec(t, "INSERT INTO class_sessions(id,class_id,rescheduled_from_session_id,tutor_id,session_date,start_time,end_time) VALUES (?,?,?,?,'2026-10-22','11:00','12:00')", second, f.class, replacement, uuid.New())
	if f.count(t) != 9 {
		t.Fatal("recursive reschedule lost cohort")
	}
	// Absence notifies only that student's parent, through single and bulk writes.
	f.exec(t, "INSERT INTO attendances(id,enrollment_id,session_id,date,status) VALUES (?,?,?,'2026-10-22','absent')", uuid.New(), f.enrollments[0], second)
	if f.count(t) != 11 {
		t.Fatal("absence recipient wrong")
	}
	f.exec(t, "UPDATE attendances SET status='absent' WHERE session_id=?", second)
	if f.count(t) != 11 {
		t.Fatal("replay duplicated absence")
	}
	f.exec(t, "UPDATE attendances SET status='present' WHERE session_id=?", second)
	if f.count(t) != 11 {
		t.Fatal("present created absence event")
	}
	// A foreign-tenant enrollment is excluded even if linked to this class.
	f.exec(t, "UPDATE enrollments SET tenant_id=? WHERE id=?", uuid.New(), f.enrollments[2])
	f.exec(t, "UPDATE class_sessions SET tutor_id=? WHERE id=?", uuid.New(), second)
	if f.count(t) != 13 {
		t.Fatal("foreign tenant enrollment notified")
	}
	// Real bulk repository writes and their outbox must commit/rollback together.
	f.exec(t, "CREATE FUNCTION fail_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END $$")
	f.exec(t, "CREATE TRIGGER fail_outbox BEFORE INSERT ON parent_notification_outbox FOR EACH ROW EXECUTE FUNCTION fail_outbox()")
	bulk := NewAttendanceRepository(f.db)
	items := []domain.BulkAttendanceItem{{EnrollmentID: f.enrollments[1], Status: "absent"}}
	if _, err := bulk.UpsertBulk(context.Background(), f.tenant, second, items); err == nil {
		t.Fatal("outbox failure accepted")
	}
	var attendanceCount int64
	f.db.Table("attendances").Where("enrollment_id=?", f.enrollments[1]).Count(&attendanceCount)
	if attendanceCount != 0 || f.count(t) != 13 {
		t.Fatal("bulk mutation escaped failed outbox transaction")
	}
	f.exec(t, "DROP TRIGGER fail_outbox ON parent_notification_outbox")
	for range 2 {
		if _, err := bulk.UpsertBulk(context.Background(), f.tenant, second, items); err != nil {
			t.Fatal(err)
		}
	}
	if f.count(t) != 15 {
		t.Fatal("bulk retry duplicated absence event")
	}
}

func TestParentNotificationEmailUpgradeAndPrivacy(t *testing.T) {
	f := newNotificationFixture(t)
	down, err := os.ReadFile("../../migrations/00001790900002_parent_notifications.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, string(down))
	up, err := os.ReadFile("../../migrations/00001790900002_parent_notifications.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, string(up))
	var missing int64
	f.db.Table("enrollments").Where("parent_email IS NULL").Count(&missing)
	if missing != 5 {
		t.Fatal("legacy email was invented during upgrade")
	}
	student := uuid.New()
	f.exec(t, "INSERT INTO students(id,parent_id,first_name) VALUES (?,?,?)", student, f.parent, "Fani")
	e := &domain.Enrollment{ID: uuid.New(), TenantID: f.tenant, ClassID: f.class, StudentID: student, Status: "active", BillingCycle: "monthly", ParentEmail: "verified@example.test"}
	if err := NewEnrollmentRepository(f.db).Create(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	var email string
	f.db.Raw("SELECT parent_email FROM enrollments WHERE id=?", e.ID).Scan(&email)
	if email != e.ParentEmail {
		t.Fatal("verified email not persisted")
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), email) || strings.Contains(string(data), "parent_email") {
		t.Fatal("email exposed in API JSON")
	}
}

func TestParentNotificationPrivateAndCancelled(t *testing.T) {
	f := newNotificationFixture(t)
	f.exec(t, "UPDATE class_sessions SET enrollment_id=?,schedule_id=NULL WHERE id=?", f.enrollments[0], f.session)
	f.exec(t, "UPDATE class_sessions SET tutor_id=? WHERE id=?", uuid.New(), f.session)
	if f.count(t) != 2 {
		t.Fatal("private session included another enrollment")
	}
	var wrong int64
	f.db.Table("parent_notification_outbox").Where("parent_id<>? OR body LIKE '%Budi%'", f.parent).Count(&wrong)
	if wrong != 0 {
		t.Fatal("private scope leaked")
	}
	f.exec(t, "UPDATE class_sessions SET status='cancelled',tutor_id=? WHERE id=?", uuid.New(), f.session)
	f.exec(t, "INSERT INTO attendances(id,enrollment_id,session_id,date,status) VALUES (?,?,?,'2026-10-20','absent')", uuid.New(), f.enrollments[0], f.session)
	if f.count(t) != 2 {
		t.Fatal("cancelled session notified")
	}
}

func TestParentNotificationLegacySiblingIsChatOnly(t *testing.T) {
	f := newNotificationFixture(t)
	f.exec(t, "UPDATE enrollments SET parent_email=NULL WHERE id=?", f.enrollments[1])
	f.exec(t, "UPDATE class_sessions SET tutor_id=? WHERE id=?", uuid.New(), f.session)
	var count int64
	f.db.Table("parent_notification_outbox").Where("parent_id=? AND channel='email' AND body LIKE '%Budi%'", f.parent).Count(&count)
	if count != 0 {
		t.Fatal("legacy enrollment without verified email included in email")
	}
	f.db.Table("parent_notification_outbox").Where("parent_id=? AND channel='chat' AND body LIKE '%Ani, Budi%'", f.parent).Count(&count)
	if count != 1 || f.count(t) != 3 {
		t.Fatal("sibling chat grouping changed")
	}
}

func TestParentNotificationClaimLeaseAndFencing(t *testing.T) {
	f := newNotificationFixture(t)
	f.exec(t, "UPDATE class_sessions SET tutor_id=? WHERE id=?", uuid.New(), f.session)
	store := NewParentNotificationRepository(f.db)
	var wg sync.WaitGroup
	ids := make(chan uuid.UUID, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			n, err := store.Claim(context.Background(), "first@example.test")
			if err != nil {
				errs <- err
			}
			if n != nil {
				ids <- n.ID
			}
		})
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	seen := map[uuid.UUID]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatal("double claim")
		}
		seen[id] = true
	}
	if len(seen) != 3 {
		t.Fatalf("claimed %d", len(seen))
	}
	var old ParentNotification
	f.db.Raw("SELECT * FROM parent_notification_outbox LIMIT 1").Scan(&old)
	f.exec(t, "UPDATE parent_notification_outbox SET available_at=now()-interval '1 second' WHERE id=?", old.ID)
	renewed, err := store.Claim(context.Background(), "changed@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if renewed.ID != old.ID || renewed.LeaseToken == old.LeaseToken || renewed.Attempts != 2 || renewed.SenderEmail != "first@example.test" {
		t.Fatalf("bad lease reclaim: %+v", renewed)
	}
	if err := store.Finish(context.Background(), &old, "sent", "", 0); err != nil {
		t.Fatal(err)
	}
	var status string
	f.db.Raw("SELECT status FROM parent_notification_outbox WHERE id=?", old.ID).Scan(&status)
	if status != "sending" {
		t.Fatal("stale worker completed new lease")
	}
	if err := store.Finish(context.Background(), renewed, "pending", "delivery_unconfirmed", time.Minute); err != nil {
		t.Fatal(err)
	}
	n, err := store.Claim(context.Background(), "")
	if err != nil || n != nil {
		t.Fatal("backoff was ignored", err)
	}
}

func TestParentNotificationRollbackMigrationPreservesHistory(t *testing.T) {
	f := newNotificationFixture(t)
	f.exec(t, "UPDATE class_sessions SET tutor_id=? WHERE id=?", uuid.New(), f.session)
	sql, err := os.ReadFile("../../migrations/00001790900002_parent_notifications.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.Exec(string(sql)).Error; err == nil {
		t.Fatal("rollback discarded history")
	}
	if f.count(t) != 3 {
		t.Fatal("failed rollback damaged history")
	}
	f.exec(t, "DELETE FROM parent_notification_outbox")
	if err = f.db.Exec(string(sql)).Error; err != nil {
		t.Fatal(err)
	}
}
