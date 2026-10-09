package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
)

func TestStudentDateOfBirthCreateAndUpdate(t *testing.T) {
	today := time.Now().In(time.FixedZone("WIB", 7*60*60))
	for _, operation := range []string{"create", "update"} {
		for _, test := range []struct {
			name    string
			date    string
			invalid bool
		}{
			{"tomorrow", today.AddDate(0, 0, 1).Format("2006-01-02"), true},
			{"today", today.Format("2006-01-02"), false},
			{"past", "2015-01-01", false},
			{"invalid calendar date", "2026-02-30", true},
		} {
			t.Run(operation+"/"+test.name, func(t *testing.T) {
				parent, id := uuid.New(), uuid.New()
				repo := &studentRepoStub{}
				if operation == "update" {
					repo.student = &domain.Student{ID: id, ParentID: parent, FirstName: "Original"}
				}
				tx := &transactionStub{}
				notes := &studentNoteRepoStub{}
				u := newStudentUsecaseForTest(repo, notes, tx)
				var student *domain.Student
				var err error
				if operation == "create" {
					student, err = u.Create(context.Background(), nil, &parent, &domain.CreateStudentRequest{ParentID: parent, FirstName: "New", DateOfBirth: test.date})
				} else {
					student, err = u.Update(context.Background(), nil, &parent, &parent, id, &domain.UpdateStudentRequest{FirstName: "New", DateOfBirth: test.date})
				}
				if (err != nil) != test.invalid {
					t.Fatalf("error=%v invalid=%v", err, test.invalid)
				}
				if test.name == "tomorrow" && !errors.Is(err, domain.ErrStudentDateOfBirthFuture) {
					t.Fatalf("expected future DOB error, got %v", err)
				}
				if test.invalid {
					if tx.calls != 0 || notes.calls != 0 || student != nil {
						t.Fatal("invalid DOB must not enter transaction or write notes")
					}
					if operation == "create" && repo.student != nil {
						t.Fatal("invalid create must not persist")
					}
					if operation == "update" && (repo.student.FirstName != "Original" || repo.student.DateOfBirth != nil) {
						t.Fatal("invalid update must not mutate existing student")
					}
				} else if student == nil || student.DateOfBirth.Format("2006-01-02") != test.date || tx.calls != 1 {
					t.Fatalf("valid date was not persisted: %+v", student)
				}
			})
		}
	}
}

func TestStudentDateOfBirthWIBBoundary(t *testing.T) {
	for _, test := range []struct {
		instant string
		date    string
		future  bool
	}{
		{"2026-10-08T16:59:59Z", "2026-10-09", true},
		{"2026-10-08T17:00:00Z", "2026-10-09", false},
		{"2026-10-09T16:59:59Z", "2026-10-09", false},
		{"2026-10-09T16:59:59Z", "2026-10-10", true},
	} {
		t.Run(test.instant+"/"+test.date, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, test.instant)
			_, err := parseStudentDateOfBirth(test.date, now)
			if errors.Is(err, domain.ErrStudentDateOfBirthFuture) != test.future || (err != nil && !test.future) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
