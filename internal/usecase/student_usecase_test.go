package usecase

import (
	"context"
	"testing"

	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
)

type studentRepoStub struct{ student *domain.Student }

func (s *studentRepoStub) Create(_ context.Context, student *domain.Student) error {
	s.student = student
	return nil
}
func (studentRepoStub) GetByID(context.Context, uuid.UUID) (*domain.Student, error) {
	return nil, domain.ErrStudentNotFound
}
func (s *studentRepoStub) GetByIDForAccess(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID) (*domain.Student, error) {
	if s.student == nil {
		return nil, domain.ErrStudentNotFound
	}
	return s.student, nil
}
func (studentRepoStub) List(context.Context, *uuid.UUID, *uuid.UUID, domain.StudentQuery) ([]domain.Student, int64, error) {
	return nil, 0, nil
}
func (studentRepoStub) CountActiveEnrollments(context.Context, uuid.UUID) (int64, error) {
	return 0, nil
}
func (s *studentRepoStub) Update(_ context.Context, student *domain.Student) error {
	s.student = student
	return nil
}
func (*studentRepoStub) Delete(context.Context, uuid.UUID) error { return nil }

type studentNoteRepoStub struct {
	note *domain.StudentNote
	err  error
}

func (s *studentNoteRepoStub) Create(_ context.Context, note *domain.StudentNote) error {
	if s.err != nil {
		return s.err
	}
	s.note = note
	return nil
}
func (*studentNoteRepoStub) GetByID(context.Context, uuid.UUID) (*domain.StudentNote, error) {
	return nil, domain.ErrStudentNoteInvalid
}
func (*studentNoteRepoStub) Update(context.Context, *domain.StudentNote) error { return nil }
func (*studentNoteRepoStub) Delete(context.Context, uuid.UUID) error           { return nil }

type transactionStub struct {
	err   error
	calls int
}

func (s *transactionStub) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	s.calls++
	if s.err != nil {
		return s.err
	}
	return fn(ctx)
}

func newStudentUsecaseForTest(repo *studentRepoStub, noteRepo *studentNoteRepoStub, tx *transactionStub) StudentUsecase {
	var _ repository.StudentRepository = repo
	var _ repository.StudentNoteRepository = noteRepo
	var _ repository.TransactionManager = tx
	return NewStudentUsecase(repo, noteRepo, tx)
}

func TestStudentUsecaseCreateParentOwnership(t *testing.T) {
	parent := uuid.New()
	other := uuid.New()
	tests := []struct {
		name    string
		tenant  *uuid.UUID
		user    uuid.UUID
		parent  uuid.UUID
		wantErr error
	}{
		{name: "parent owns student", tenant: nil, user: parent, parent: parent, wantErr: nil},
		{name: "parent cannot create for another user", tenant: nil, user: parent, parent: other, wantErr: domain.ErrStudentForbidden},
		{name: "staff can create cross-service parent", tenant: &other, user: parent, parent: other, wantErr: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newStudentUsecaseForTest(&studentRepoStub{}, &studentNoteRepoStub{}, &transactionStub{}).Create(context.TODO(), test.tenant, &test.user, &domain.CreateStudentRequest{ParentID: test.parent, FirstName: " Student ", DateOfBirth: "2015-01-01"})
			if err != test.wantErr {
				t.Fatalf("error=%v want=%v", err, test.wantErr)
			}
		})
	}
}

func TestStudentUsecaseCreateMapsNewFields(t *testing.T) {
	parent := uuid.New()
	lastName := " Doe "
	nickname := " JD "
	gender := "male"
	repo := &studentRepoStub{}
	student, err := newStudentUsecaseForTest(repo, &studentNoteRepoStub{}, &transactionStub{}).Create(context.Background(), nil, &parent, &domain.CreateStudentRequest{
		ParentID: parent, FirstName: " John ", LastName: &lastName, Nickname: &nickname, Gender: &gender, DateOfBirth: "2015-01-01",
	})
	if err != nil {
		t.Fatalf("create error: %v", err)
	}
	if student.FirstName != "John" || *student.LastName != "Doe" || *student.Nickname != "JD" || *student.Gender != "male" {
		t.Fatalf("unexpected student mapping: %+v", student)
	}
}

func TestStudentUsecaseRejectsWhitespaceFirstName(t *testing.T) {
	parent := uuid.New()
	_, err := newStudentUsecaseForTest(&studentRepoStub{}, &studentNoteRepoStub{}, &transactionStub{}).Create(context.Background(), nil, &parent, &domain.CreateStudentRequest{ParentID: parent, FirstName: "   ", DateOfBirth: "2015-01-01"})
	if err != domain.ErrStudentFirstNameRequired {
		t.Fatalf("error=%v want=%v", err, domain.ErrStudentFirstNameRequired)
	}
}

func TestStudentUsecaseUpdateMapsNewFieldsAndClearsOptionalFields(t *testing.T) {
	studentID := uuid.New()
	parent := uuid.New()
	lastName := "Old"
	nickname := "Oldie"
	gender := "female"
	repo := &studentRepoStub{student: &domain.Student{ID: studentID, ParentID: parent, FirstName: "Old", LastName: &lastName, Nickname: &nickname, Gender: &gender}}
	updated, err := newStudentUsecaseForTest(repo, &studentNoteRepoStub{}, &transactionStub{}).Update(context.Background(), nil, &parent, &parent, studentID, &domain.UpdateStudentRequest{
		FirstName: " New ", LastName: &lastName, DateOfBirth: "2016-02-03",
	})
	if err != nil {
		t.Fatalf("update error: %v", err)
	}
	if updated.FirstName != "New" || updated.LastName == nil || *updated.LastName != "Old" || updated.Nickname != nil || updated.Gender != nil {
		t.Fatalf("unexpected student update: %+v", updated)
	}
}

func TestStudentRequestRejectsInvalidGender(t *testing.T) {
	gender := "other"
	err := binding.Validator.ValidateStruct(&domain.CreateStudentRequest{ParentID: uuid.New(), FirstName: "Student", Gender: &gender, DateOfBirth: "2015-01-01"})
	if err == nil {
		t.Fatal("expected invalid gender validation error")
	}
}

func TestStudentUsecaseCreateStudentNote(t *testing.T) {
	parent := uuid.New()
	tenant := uuid.New()
	author := uuid.New()
	tests := []struct {
		name    string
		note    *domain.StudentNoteRequest
		wantErr error
	}{
		{name: "valid note", note: &domain.StudentNoteRequest{NoteType: "academic", Content: "  Needs support.  "}},
		{name: "invalid type", note: &domain.StudentNoteRequest{NoteType: "other", Content: "Needs support."}, wantErr: domain.ErrStudentNoteInvalid},
		{name: "blank content", note: &domain.StudentNoteRequest{NoteType: "medical", Content: "   "}, wantErr: domain.ErrStudentNoteContentRequired},
		{name: "tenant required", note: &domain.StudentNoteRequest{NoteType: "behavioral", Content: "Progressing."}, wantErr: domain.ErrStudentNoteTenantRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &studentRepoStub{}
			noteRepo := &studentNoteRepoStub{}
			requestTenant := &tenant
			user := &author
			if test.name == "tenant required" {
				requestTenant = nil
				user = &parent
			}
			student, err := newStudentUsecaseForTest(repo, noteRepo, &transactionStub{}).Create(context.Background(), requestTenant, user, &domain.CreateStudentRequest{
				ParentID: parent, FirstName: "Student", DateOfBirth: "2015-01-01", StudentNote: test.note,
			})
			if err != test.wantErr {
				t.Fatalf("error=%v want=%v", err, test.wantErr)
			}
			if test.wantErr == nil {
				if student == nil || noteRepo.note == nil || noteRepo.note.StudentID != student.ID || noteRepo.note.TenantID != tenant || noteRepo.note.AuthorID != author || noteRepo.note.Content != "Needs support." {
					t.Fatalf("unexpected note mapping: student=%+v note=%+v", student, noteRepo.note)
				}
			}
		})
	}
}

func TestStudentUsecaseUpdateCreatesNewStudentNote(t *testing.T) {
	studentID := uuid.New()
	parent := uuid.New()
	tenant := uuid.New()
	author := uuid.New()
	repo := &studentRepoStub{student: &domain.Student{ID: studentID, ParentID: parent, FirstName: "Student"}}
	noteRepo := &studentNoteRepoStub{}
	_, err := newStudentUsecaseForTest(repo, noteRepo, &transactionStub{}).Update(context.Background(), &tenant, &parent, &author, studentID, &domain.UpdateStudentRequest{
		FirstName: "Student", DateOfBirth: "2015-01-01", StudentNote: &domain.StudentNoteRequest{NoteType: "behavioral", Content: "  Improved. "},
	})
	if err != nil {
		t.Fatalf("update error: %v", err)
	}
	if noteRepo.note == nil || noteRepo.note.ID == uuid.Nil || noteRepo.note.StudentID != studentID || noteRepo.note.TenantID != tenant || noteRepo.note.AuthorID != author || noteRepo.note.Content != "Improved." {
		t.Fatalf("unexpected update note: %+v", noteRepo.note)
	}
}

func TestStudentUsecaseRollsBackWhenStudentNoteCreateFails(t *testing.T) {
	parent := uuid.New()
	tenant := uuid.New()
	noteErr := domain.ErrStudentNoteInvalid
	tx := &transactionStub{}
	_, err := newStudentUsecaseForTest(&studentRepoStub{}, &studentNoteRepoStub{err: noteErr}, tx).Create(context.Background(), &tenant, &parent, &domain.CreateStudentRequest{
		ParentID: parent, FirstName: "Student", DateOfBirth: "2015-01-01", StudentNote: &domain.StudentNoteRequest{NoteType: "academic", Content: "Note"},
	})
	if err != noteErr {
		t.Fatalf("error=%v want=%v", err, noteErr)
	}
	if tx.calls != 1 {
		t.Fatalf("transaction calls=%d want=1", tx.calls)
	}
}
