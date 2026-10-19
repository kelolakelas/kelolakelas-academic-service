package repository

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func reminderDates() (time.Time, time.Time) {
	from := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	return from, from.AddDate(0, 0, 1)
}

func TestSessionReminderPostgresIdempotencyAndLateEnrollment(t *testing.T) {
	f := newNotificationFixture(t)
	repo := NewSessionReminderRepository(f.db)
	from, until := reminderDates()
	first, err := repo.EnqueueBatch(context.Background(), from, until, 100)
	if err != nil {
		t.Fatal(err)
	}
	if first.Sessions != 1 || first.EnrollmentsMarked != 3 || first.NotificationsQueued != 3 {
		t.Fatalf("first=%+v", first)
	}
	again, err := repo.EnqueueBatch(context.Background(), from, until, 100)
	if err != nil || again.EnrollmentsMarked != 0 || f.count(t) != 3 {
		t.Fatalf("repeat=%+v err=%v", again, err)
	}
	// A shifted overlapping window cannot bypass persistent deduplication.
	if _, err = repo.EnqueueBatch(context.Background(), from.AddDate(0, 0, -1), until.AddDate(0, 0, 1), 100); err != nil || f.count(t) != 3 {
		t.Fatal("window change duplicated reminders", err)
	}
	for i, parent := range []uuid.UUID{uuid.New(), f.parent} {
		student := uuid.New()
		f.exec(t, "INSERT INTO students(id,parent_id,first_name) VALUES (?,?,?)", student, parent, []string{"Fani", "Gita"}[i])
		f.exec(t, "INSERT INTO enrollments(id,tenant_id,class_id,student_id,schedule_id,status,parent_email) VALUES (?,?,?,?,?,'active','late@example.test')", uuid.New(), f.tenant, f.class, student, f.schedule)
	}
	late, err := repo.EnqueueBatch(context.Background(), from, until, 100)
	if err != nil || late.EnrollmentsMarked != 2 || late.NotificationsQueued != 2 || f.count(t) != 5 {
		t.Fatalf("late=%+v err=%v", late, err)
	}
	// Grouped original reminder has both sibling first names and no private surname.
	var bad int64
	f.db.Table("parent_notification_outbox").Where("body LIKE '%PRIVATE-LAST-NAME%' OR body LIKE '%Dedi%' OR body LIKE '%Eka%'").Count(&bad)
	if bad != 0 {
		t.Fatal("inactive/private data in reminders")
	}
	var body string
	f.db.Raw("SELECT body FROM parent_notification_outbox WHERE parent_id=? AND channel='chat'", f.parent).Scan(&body)
	if body != "Pengingat kelas Math group, sesi 20-10-2026 09:00:00–10:00:00 WIB.\nSiswa: Ani, Budi" {
		t.Fatalf("body=%q", body)
	}
}

func TestSessionReminderPostgresConcurrentWorkers(t *testing.T) {
	f := newNotificationFixture(t)
	from, until := reminderDates()
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Go(func() {
			_, err := NewSessionReminderRepository(f.db).EnqueueBatch(context.Background(), from, until, 100)
			if err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if f.count(t) != 3 {
		t.Fatal("concurrent workers duplicated reminders")
	}
	var markers int64
	f.db.Table("session_reminder_enrollments").Count(&markers)
	if markers != 3 {
		t.Fatalf("markers=%d", markers)
	}
}

func TestSessionReminderPostgresFiltersAndDateBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, mutation string }{
		{"cancelled", "UPDATE class_sessions SET status='cancelled'"},
		{"completed", "UPDATE class_sessions SET status='completed'"},
		{"rescheduled", "UPDATE class_sessions SET status='rescheduled'"},
		{"deleted session", "UPDATE class_sessions SET deleted_at=now()"},
		{"deleted class", "UPDATE classes SET deleted_at=now()"},
		{"inactive enrollments", "UPDATE enrollments SET status='suspended'"},
		{"deleted enrollments", "UPDATE enrollments SET deleted_at=now()"},
		{"deleted students", "UPDATE students SET deleted_at=now()"},
		{"foreign tenant enrollments", "UPDATE enrollments SET tenant_id=gen_random_uuid()"},
		{"before lower bound", "UPDATE class_sessions SET session_date='2026-10-19'"},
		{"at exclusive upper bound", "UPDATE class_sessions SET session_date='2026-10-21'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newNotificationFixture(t)
			f.exec(t, tc.mutation)
			from, until := reminderDates()
			batch, err := NewSessionReminderRepository(f.db).EnqueueBatch(context.Background(), from, until, 100)
			if err != nil || batch.Sessions != 0 || f.count(t) != 0 {
				t.Fatalf("batch=%+v err=%v", batch, err)
			}
		})
	}
	t.Run("private enrollment only", func(t *testing.T) {
		f := newNotificationFixture(t)
		f.exec(t, "UPDATE class_sessions SET enrollment_id=?,schedule_id=NULL WHERE id=?", f.enrollments[0], f.session)
		from, until := reminderDates()
		batch, err := NewSessionReminderRepository(f.db).EnqueueBatch(context.Background(), from, until, 100)
		if err != nil || batch.EnrollmentsMarked != 1 || batch.NotificationsQueued != 2 {
			t.Fatalf("batch=%+v err=%v", batch, err)
		}
	})
}

func TestSessionReminderPostgresAtomicityAndDownMigration(t *testing.T) {
	f := newNotificationFixture(t)
	repo := NewSessionReminderRepository(f.db)
	from, until := reminderDates()
	f.exec(t, "CREATE FUNCTION fail_reminder() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected ledger failure'; END $$")
	f.exec(t, "CREATE TRIGGER fail_reminder BEFORE INSERT ON session_reminder_enrollments FOR EACH ROW EXECUTE FUNCTION fail_reminder()")
	if _, err := repo.EnqueueBatch(context.Background(), from, until, 100); err == nil {
		t.Fatal("ledger failure ignored")
	}
	if f.count(t) != 0 {
		t.Fatal("ledger failure left outbox rows")
	}
	f.exec(t, "DROP TRIGGER fail_reminder ON session_reminder_enrollments")
	if _, err := repo.EnqueueBatch(context.Background(), from, until, 100); err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/00001790900003_session_reminders.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.Exec(string(down)).Error; err == nil {
		t.Fatal("rollback erased reminder coverage")
	}
	var count int64
	f.db.Table("session_reminder_enrollments").Count(&count)
	if count != 3 || f.count(t) != 3 {
		t.Fatal("failed down migration changed history")
	}
}

func TestSessionReminderPostgresRescheduleAfterWorker(t *testing.T) {
	f := newNotificationFixture(t)
	repo := NewSessionReminderRepository(f.db)
	from, until := reminderDates()
	f.exec(t, "UPDATE class_sessions SET session_date='2026-10-19' WHERE id=?", f.session)
	if batch, err := repo.EnqueueBatch(context.Background(), from, until, 100); err != nil || batch.Sessions != 0 {
		t.Fatal("today was reminded", err)
	}
	replacement := uuid.New()
	f.exec(t, "UPDATE class_sessions SET status='rescheduled' WHERE id=?", f.session)
	f.exec(t, "INSERT INTO class_sessions(id,class_id,rescheduled_from_session_id,tutor_id,session_date,start_time,end_time) VALUES (?,?,?,?,'2026-10-20','11:00','12:00')", replacement, f.class, f.session, uuid.New())
	batch, err := repo.EnqueueBatch(context.Background(), from, until, 100)
	if err != nil || batch.EnrollmentsMarked != 3 || batch.NotificationsQueued != 3 {
		t.Fatalf("replacement batch=%+v err=%v", batch, err)
	}
	// Rescheduling a reminded session uses KEL-155 change notices, not another reminder.
	second := uuid.New()
	f.exec(t, "UPDATE class_sessions SET status='rescheduled' WHERE id=?", replacement)
	f.exec(t, "INSERT INTO class_sessions(id,class_id,rescheduled_from_session_id,tutor_id,session_date,start_time,end_time) VALUES (?,?,?,?,'2026-10-20','13:00','14:00')", second, f.class, replacement, uuid.New())
	if batch, err = repo.EnqueueBatch(context.Background(), from, until, 100); err != nil || batch.NotificationsQueued != 0 {
		t.Fatal("rescheduled already-reminded cohort got duplicate reminder", err)
	}
}

func TestSessionReminderPostgresSuppressesStalePendingDelivery(t *testing.T) {
	for _, change := range []string{"UPDATE class_sessions SET status='cancelled'", "UPDATE enrollments SET status='suspended'"} {
		f := newNotificationFixture(t)
		from, until := reminderDates()
		if _, err := NewSessionReminderRepository(f.db).EnqueueBatch(context.Background(), from, until, 100); err != nil {
			t.Fatal(err)
		}
		f.exec(t, change)
		n, err := NewParentNotificationRepository(f.db).Claim(context.Background(), "sender@example.test")
		if err != nil || n != nil {
			t.Fatal("stale reminder claimed", err)
		}
		var suppressed int64
		f.db.Table("parent_notification_outbox").Where("status='failed' AND last_error='reminder_no_longer_eligible'").Count(&suppressed)
		if suppressed != 3 {
			t.Fatalf("suppressed=%d", suppressed)
		}
	}
}
