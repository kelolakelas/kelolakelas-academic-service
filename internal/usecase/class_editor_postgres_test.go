package usecase_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestClassEditorPostgres(t *testing.T) {
	dsn := os.Getenv("KEL198_TEST_DSN")
	if dsn == "" {
		t.Skip("KEL198_TEST_DSN not set")
	}
	base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := "kel198_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := base.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	defer base.Exec("DROP SCHEMA " + schema + " CASCADE")
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema+",public"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	for _, file := range []string{"00000000000000_init_schema.up.sql", "00001790812223_add_rescheduled_from_to_class_sessions.up.sql"} {
		data, err := os.ReadFile("../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(string(data)).Error; err != nil {
			t.Fatal(err)
		}
	}
	must := func(sql string, args ...any) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	tenant, teacher := uuid.New(), uuid.New()
	identity := creationIdentity{tenant: tenant, teacher: teacher}
	categories := repository.NewCategoryRepository(db)
	classes := repository.NewClassRepository(db)
	schedules := repository.NewScheduleRepository(db)
	sessions := repository.NewSessionRepository(db)
	enrollments := repository.NewEnrollmentRepository(db)
	tx := repository.NewTransactionManager(db)
	scheduleUC := usecase.NewScheduleUsecase(tx, classes, schedules, sessions, enrollments, identity)
	classUC := usecase.WithClassMembership(usecase.NewClassUsecase(classes, schedules, sessions, enrollments, identity, tx, categories), identity)
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(now.Year(), now.Month()+2, 0, 0, 0, 0, 0, time.UTC)
	effective := time.Date(now.Year(), now.Month()+1, 8, 0, 0, 0, 0, time.UTC)
	historic := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, time.UTC)
	type fixture struct{ class, slot, enrollment, history uuid.UUID }
	fresh := func() fixture {
		t.Helper()
		f := fixture{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
		category, student := uuid.New(), uuid.New()
		must("INSERT INTO categories(id,tenant_id,name)VALUES(?,?,'Existing')", category, tenant)
		must("INSERT INTO classes(id,tenant_id,category_id,name,type,price)VALUES(?,?,?,'Group','group',100)", f.class, tenant, category)
		must("INSERT INTO students(id,parent_id,first_name)VALUES(?,?,'Student')", student, uuid.New())
		must("INSERT INTO class_schedules(id,class_id,tutor_id,capacity,location,day_of_week,start_time,end_time,valid_from,valid_until)VALUES(?,?,?,2,'Old room',1,'09:00','10:00',?,?)", f.slot, f.class, teacher, from, until)
		must("INSERT INTO enrollments(id,tenant_id,student_id,class_id,schedule_id,status,gross_amount)VALUES(?,?,?,?,?,'active',100)", f.enrollment, tenant, student, f.class, f.slot)
		must("INSERT INTO class_sessions(id,class_id,schedule_id,tutor_id,session_date,start_time,end_time,status)VALUES(?,?,?,?,?,'09:00','10:00','completed')", f.history, f.class, f.slot, teacher, historic)
		must("INSERT INTO attendances(id,enrollment_id,session_id,date,status)VALUES(?,?,?,?, 'present')", uuid.New(), f.enrollment, f.history, historic)
		return f
	}
	change := func(f fixture) *domain.PermanentScheduleChangeRequest {
		capacity, location := 3, "New room"
		return &domain.PermanentScheduleChangeRequest{OldScheduleID: f.slot, NewDayOfWeek: 3, NewStartTime: "11:00", NewEndTime: "12:00", EffectiveDate: effective, NewCapacity: &capacity, NewLocation: &location, NewTutorID: &teacher}
	}
	assertOriginal := func(f fixture) {
		t.Helper()
		slot, err := schedules.GetByID(ctx, f.slot)
		if err != nil || slot.Capacity != 2 || *slot.Location != "Old room" || !slot.ValidUntil.Equal(until) {
			t.Fatalf("original changed %+v %v", slot, err)
		}
		enrollment, err := enrollments.GetByID(ctx, f.enrollment)
		if err != nil || *enrollment.ScheduleID != f.slot {
			t.Fatalf("enrollment moved %+v %v", enrollment, err)
		}
		var count int64
		db.Table("class_schedules").Where("class_id = ?", f.class).Count(&count)
		if count != 1 {
			t.Fatal("partial replacement")
		}
	}
	t.Run("effective date preserves attendance enrollment and sold price", func(t *testing.T) {
		f := fresh()
		res, err := scheduleUC.ChangeSchedulePermanent(ctx, tenant, change(f))
		if err != nil {
			t.Fatal(err)
		}
		if res.NewSchedule.Capacity != 3 || *res.NewSchedule.Location != "New room" || *res.NewSchedule.TutorID != teacher || len(res.NewSessions) == 0 {
			t.Fatalf("replacement %+v", res)
		}
		enrollment, err := enrollments.GetByID(ctx, f.enrollment)
		if err != nil || *enrollment.ScheduleID != res.NewSchedule.ID {
			t.Fatal("seat not transferred", err)
		}
		var history domain.ClassSession
		db.First(&history, "id = ?", f.history)
		if history.Status != "completed" || *history.ScheduleID != f.slot {
			t.Fatal("history changed")
		}
		var count int64
		db.Table("attendances").Where("session_id = ?", f.history).Count(&count)
		if count != 1 {
			t.Fatal("attendance lost")
		}
		price, published, status := int64(250), true, "closed"
		teachers := []uuid.UUID{teacher}
		updated, err := classUC.UpdateClass(ctx, tenant, f.class, &domain.UpdateClassRequest{Category: &domain.CreateCategoryRequest{Name: "New category"}, Price: &price, IsPublished: &published, EnrollmentStatus: &status, TeacherIDs: &teachers})
		if err != nil {
			t.Fatal(err)
		}
		if !updated.IsPublished || updated.EnrollmentStatus != "closed" || len(updated.TeacherIDs) != 1 {
			t.Fatalf("class %+v", updated)
		}
		var gross int64
		db.Raw("SELECT gross_amount FROM enrollments WHERE id = ?", f.enrollment).Scan(&gross)
		if gross != 100 {
			t.Fatal("sold price changed")
		}
		listed, err := classUC.ListClasses(ctx, tenant, domain.ListQuery{Page: 1, PageSize: 100})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, c := range listed.Items {
			if c.ID == f.class {
				found = c.Category != nil && c.Category.Name == "New category" && len(c.TeacherIDs) == 1
			}
		}
		if !found {
			t.Fatal("saved config not readable")
		}
	})
	t.Run("history capacity membership and foreign tenant refusal rollback", func(t *testing.T) {
		for _, mode := range []string{"past", "attendance", "capacity", "teacher", "foreign", "invalid time"} {
			t.Run(mode, func(t *testing.T) {
				f := fresh()
				req := change(f)
				owner := tenant
				switch mode {
				case "past":
					req.EffectiveDate = now.AddDate(0, 0, -1)
				case "attendance":
					must("UPDATE class_sessions SET session_date = ? WHERE id = ?", effective, f.history)
				case "capacity":
					other := uuid.New()
					must("INSERT INTO students(id,parent_id,first_name)VALUES(?,?,'Pending')", other, uuid.New())
					must("INSERT INTO enrollments(id,tenant_id,student_id,class_id,schedule_id,status)VALUES(?,?,?,?,?,'pending')", uuid.New(), tenant, other, f.class, f.slot)
					value := 1
					req.NewCapacity = &value
				case "teacher":
					id := uuid.New()
					req.NewTutorID = &id
				case "foreign":
					owner = uuid.New()
				case "invalid time":
					req.NewEndTime = "10:00"
				}
				if _, err := scheduleUC.ChangeSchedulePermanent(ctx, owner, req); err == nil {
					t.Fatal("invalid change accepted")
				}
				assertOriginal(f)
			})
		}
	})
	t.Run("failed session write rolls back version transfer and class category write", func(t *testing.T) {
		f := fresh()
		must("CREATE FUNCTION fail_editor() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected editor failure'; END $$")
		must("CREATE TRIGGER fail_editor BEFORE INSERT ON class_sessions FOR EACH ROW EXECUTE FUNCTION fail_editor()")
		if _, err := scheduleUC.ChangeSchedulePermanent(ctx, tenant, change(f)); err == nil {
			t.Fatal("expected injected failure")
		}
		must("DROP TRIGGER fail_editor ON class_sessions")
		assertOriginal(f)
		var before, after int64
		db.Table("categories").Count(&before)
		must("CREATE TRIGGER fail_editor BEFORE UPDATE ON classes FOR EACH ROW EXECUTE FUNCTION fail_editor()")
		if _, err := classUC.UpdateClass(ctx, tenant, f.class, &domain.UpdateClassRequest{Category: &domain.CreateCategoryRequest{Name: "Rollback category"}}); err == nil {
			t.Fatal("expected class failure")
		}
		must("DROP TRIGGER fail_editor ON classes")
		db.Table("categories").Count(&after)
		if before != after {
			t.Fatal("partial category write")
		}
	})
	t.Run("delete refuses live seats and preserves historic session", func(t *testing.T) {
		f := fresh()
		if err := scheduleUC.DeleteSchedule(ctx, tenant, f.slot); !errors.Is(err, repository.ErrScheduleSeatsConflict) {
			t.Fatalf("delete: %v", err)
		}
		assertOriginal(f)
		must("UPDATE enrollments SET status = 'cancelled' WHERE id = ?", f.enrollment)
		if err := scheduleUC.DeleteSchedule(ctx, tenant, f.slot); err != nil {
			t.Fatal(err)
		}
		var history domain.ClassSession
		db.First(&history, "id = ?", f.history)
		if history.Status != "completed" {
			t.Fatal("history lost")
		}
	})
}
