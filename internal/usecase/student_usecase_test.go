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
	notes []*domain.StudentNote
	err   error
	errAt int
	calls int
}

func (s *studentNoteRepoStub) Create(_ context.Context, note *domain.StudentNote) error {
	call := s.calls
	s.calls++
	if s.err != nil && call == s.errAt {
		return s.err
	}
	s.notes = append(s.notes, note)
	return nil
}
func (*studentNoteRepoStub) GetByID(context.Context, uuid.UUID) (*domain.StudentNote, error) {
	return nil, domain.ErrStudentNoteInvalid
}
func (*studentNoteRepoStub) GetByIDForAccess(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID) (*domain.StudentNote, error) {
	return nil, domain.ErrStudentNoteInvalid
}
func (*studentNoteRepoStub) Update(context.Context, *domain.StudentNote) error { return nil }
func (*studentNoteRepoStub) UpdateForAccess(context.Context, *domain.StudentNote, *uuid.UUID, *uuid.UUID) error {
	return nil
}
func (*studentNoteRepoStub) Delete(context.Context, uuid.UUID) error { return nil }
func (*studentNoteRepoStub) DeleteForAccess(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID) error {
	return nil
}

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
		{name: "parent-owned note", note: &domain.StudentNoteRequest{NoteType: "behavioral", Content: "Progressing."}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &studentRepoStub{}
			noteRepo := &studentNoteRepoStub{}
			requestTenant := &tenant
			user := &author
			if test.name == "parent-owned note" {
				requestTenant = nil
				user = &parent
			}
			student, err := newStudentUsecaseForTest(repo, noteRepo, &transactionStub{}).Create(context.Background(), requestTenant, user, &domain.CreateStudentRequest{
				ParentID: parent, FirstName: "Student", DateOfBirth: "2015-01-01", StudentNotes: []domain.StudentNoteRequest{*test.note},
			})
			if err != test.wantErr {
				t.Fatalf("error=%v want=%v", err, test.wantErr)
			}
			if test.wantErr == nil {
				expectedTenant := requestTenant
				if student == nil || len(noteRepo.notes) != 1 || noteRepo.notes[0].StudentID != student.ID || !sameUUIDPointer(noteRepo.notes[0].TenantID, expectedTenant) || noteRepo.notes[0].AuthorID != *user || noteRepo.notes[0].Content == "" {
					t.Fatalf("unexpected note mapping: student=%+v notes=%+v", student, noteRepo.notes)
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
		FirstName: "Student", DateOfBirth: "2015-01-01", StudentNotes: []domain.StudentNoteRequest{{NoteType: "behavioral", Content: "  Improved. "}},
	})
	if err != nil {
		t.Fatalf("update error: %v", err)
	}
	if len(noteRepo.notes) != 1 || noteRepo.notes[0].ID == uuid.Nil || noteRepo.notes[0].StudentID != studentID || !sameUUIDPointer(noteRepo.notes[0].TenantID, &tenant) || noteRepo.notes[0].AuthorID != author || noteRepo.notes[0].Content != "Improved." {
		t.Fatalf("unexpected update notes: %+v", noteRepo.notes)
	}
}

func TestStudentUsecaseRollsBackWhenStudentNoteCreateFails(t *testing.T) {
	parent := uuid.New()
	tenant := uuid.New()
	noteErr := domain.ErrStudentNoteInvalid
	tx := &transactionStub{}
	_, err := newStudentUsecaseForTest(&studentRepoStub{}, &studentNoteRepoStub{err: noteErr}, tx).Create(context.Background(), &tenant, &parent, &domain.CreateStudentRequest{
		ParentID: parent, FirstName: "Student", DateOfBirth: "2015-01-01", StudentNotes: []domain.StudentNoteRequest{{NoteType: "academic", Content: "Note"}},
	})
	if err != noteErr {
		t.Fatalf("error=%v want=%v", err, noteErr)
	}
	if tx.calls != 1 {
		t.Fatalf("transaction calls=%d want=1", tx.calls)
	}
}

func TestStudentUsecaseCreateStudentNotes(t *testing.T) {
	parent, tenant, author := uuid.New(), uuid.New(), uuid.New()
	repo := &studentRepoStub{}
	noteRepo := &studentNoteRepoStub{}
	student, err := newStudentUsecaseForTest(repo, noteRepo, &transactionStub{}).Create(context.Background(), &tenant, &author, &domain.CreateStudentRequest{
		ParentID: parent, FirstName: "Student", DateOfBirth: "2015-01-01",
		StudentNotes: []domain.StudentNoteRequest{
			{NoteType: "academic", Content: "  First note.  "},
			{NoteType: "behavioral", Content: "Second note."},
		},
	})
	if err != nil {
		t.Fatalf("create error: %v", err)
	}
	if len(noteRepo.notes) != 2 || noteRepo.notes[0].Content != "First note." || noteRepo.notes[1].Content != "Second note." {
		t.Fatalf("notes were not saved in request order: %+v", noteRepo.notes)
	}
	if noteRepo.notes[0].ID == noteRepo.notes[1].ID {
		t.Fatal("notes must have unique IDs")
	}
	for _, note := range noteRepo.notes {
		if note.StudentID != student.ID || !sameUUIDPointer(note.TenantID, &tenant) || note.AuthorID != author {
			t.Fatalf("note context mismatch: %+v", note)
		}
	}
}

func TestStudentUsecaseEmptyStudentNotesDoesNotCreateNotes(t *testing.T) {
	parent := uuid.New()
	noteRepo := &studentNoteRepoStub{}
	_, err := newStudentUsecaseForTest(&studentRepoStub{}, noteRepo, &transactionStub{}).Create(context.Background(), nil, &parent, &domain.CreateStudentRequest{
		ParentID: parent, FirstName: "Student", DateOfBirth: "2015-01-01", StudentNotes: []domain.StudentNoteRequest{},
	})
	if err != nil || len(noteRepo.notes) != 0 {
		t.Fatalf("empty notes should be allowed without creation: err=%v notes=%d", err, len(noteRepo.notes))
	}
}

func TestStudentUsecaseRejectsInvalidAuthorForStudentNote(t *testing.T) {
	parent, tenant := uuid.New(), uuid.New()
	invalidAuthor := uuid.Nil
	_, err := newStudentUsecaseForTest(&studentRepoStub{}, &studentNoteRepoStub{}, &transactionStub{}).Create(context.Background(), &tenant, &invalidAuthor, &domain.CreateStudentRequest{
		ParentID: parent, FirstName: "Student", DateOfBirth: "2015-01-01", StudentNotes: []domain.StudentNoteRequest{{NoteType: "academic", Content: "Note"}},
	})
	if err != domain.ErrStudentNoteInvalid {
		t.Fatalf("error=%v want=%v", err, domain.ErrStudentNoteInvalid)
	}
}

func TestStudentUsecaseRejectsParentOwnedNoteForAnotherParent(t *testing.T) {
	parent, other := uuid.New(), uuid.New()
	_, err := newStudentUsecaseForTest(&studentRepoStub{}, &studentNoteRepoStub{}, &transactionStub{}).Create(context.Background(), nil, &other, &domain.CreateStudentRequest{
		ParentID: parent, FirstName: "Student", DateOfBirth: "2015-01-01", StudentNotes: []domain.StudentNoteRequest{{NoteType: "academic", Content: "Note"}},
	})
	if err != domain.ErrStudentForbidden {
		t.Fatalf("error=%v want=%v", err, domain.ErrStudentForbidden)
	}
}

func TestStudentUsecaseRejectsInvalidStudentNoteBeforeTransaction(t *testing.T) {
	parent, tenant, author := uuid.New(), uuid.New(), uuid.New()
	tests := []struct {
		name string
		note domain.StudentNoteRequest
		want error
	}{
		{name: "invalid type", note: domain.StudentNoteRequest{NoteType: "other", Content: "Note"}, want: domain.ErrStudentNoteInvalid},
		{name: "blank content", note: domain.StudentNoteRequest{NoteType: "academic", Content: "  "}, want: domain.ErrStudentNoteContentRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := &transactionStub{}
			_, err := newStudentUsecaseForTest(&studentRepoStub{}, &studentNoteRepoStub{}, tx).Create(context.Background(), &tenant, &author, &domain.CreateStudentRequest{
				ParentID: parent, FirstName: "Student", DateOfBirth: "2015-01-01", StudentNotes: []domain.StudentNoteRequest{test.note},
			})
			if err != test.want || tx.calls != 0 {
				t.Fatalf("error=%v want=%v transactions=%d", err, test.want, tx.calls)
			}
		})
	}
}

func TestStudentUsecaseCreateFailsWhenSecondNoteFails(t *testing.T) {
	parent, tenant, author := uuid.New(), uuid.New(), uuid.New()
	noteErr := domain.ErrStudentNoteInvalid
	noteRepo := &studentNoteRepoStub{err: noteErr, errAt: 1}
	tx := &transactionStub{}
	_, err := newStudentUsecaseForTest(&studentRepoStub{}, noteRepo, tx).Create(context.Background(), &tenant, &author, &domain.CreateStudentRequest{
		ParentID: parent, FirstName: "Student", DateOfBirth: "2015-01-01", StudentNotes: []domain.StudentNoteRequest{
			{NoteType: "academic", Content: "First"}, {NoteType: "behavioral", Content: "Second"},
		},
	})
	if err != noteErr || tx.calls != 1 || noteRepo.calls != 2 || len(noteRepo.notes) != 1 {
		t.Fatalf("second note failure was not propagated atomically: err=%v tx=%d calls=%d notes=%d", err, tx.calls, noteRepo.calls, len(noteRepo.notes))
	}
}

func TestStudentUsecaseUpdateAppendsMultipleStudentNotes(t *testing.T) {
	studentID, parent, tenant, author := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	repo := &studentRepoStub{student: &domain.Student{ID: studentID, ParentID: parent, FirstName: "Student"}}
	noteRepo := &studentNoteRepoStub{notes: []*domain.StudentNote{{ID: uuid.New(), StudentID: studentID, Content: "Old"}}}
	_, err := newStudentUsecaseForTest(repo, noteRepo, &transactionStub{}).Update(context.Background(), &tenant, &parent, &author, studentID, &domain.UpdateStudentRequest{
		FirstName: "Student", DateOfBirth: "2015-01-01", StudentNotes: []domain.StudentNoteRequest{
			{NoteType: "academic", Content: "New one"}, {NoteType: "medical", Content: "New two"},
		},
	})
	if err != nil || len(noteRepo.notes) != 3 || noteRepo.notes[0].Content != "Old" || noteRepo.notes[1].Content != "New one" || noteRepo.notes[2].Content != "New two" {
		t.Fatalf("update must append notes without changing old notes: err=%v notes=%+v", err, noteRepo.notes)
	}
}

func TestStudentRequestValidatesEachStudentNote(t *testing.T) {
	err := binding.Validator.ValidateStruct(&domain.CreateStudentRequest{
		ParentID: uuid.New(), FirstName: "Student", DateOfBirth: "2015-01-01",
		StudentNotes: []domain.StudentNoteRequest{{NoteType: "invalid", Content: "Note"}},
	})
	if err == nil {
		t.Fatal("expected validation error for invalid note in array")
	}
}

func sameUUIDPointer(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
