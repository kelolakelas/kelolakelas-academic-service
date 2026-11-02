package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/handler"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
	"gorm.io/gorm"
)

type selectionAcceptanceBilling struct{}

func (selectionAcceptanceBilling) GenerateInvoice(_ context.Context, request billing.InvoiceRequest) (*billing.InvoiceResponse, error) {
	return &billing.InvoiceResponse{TransactionID: request.EnrollmentID, CheckoutSessionURL: "https://pay.example.test/selected", GrossAmount: request.SubtotalAmount}, nil
}

func (selectionAcceptanceBilling) CancelEnrollmentPayment(context.Context, uuid.UUID) (*billing.CancelResponse, error) {
	return &billing.CancelResponse{}, nil
}

func configurePrivateSelectionAcceptance(t *testing.T, db *gorm.DB, router *gin.Engine, protected *gin.RouterGroup, tenant, category uuid.UUID, secret string) map[string]string {
	t.Helper()
	parent, student, class := uuid.New(), uuid.New(), uuid.New()
	if err := db.Exec("INSERT INTO students(id,parent_id,first_name) VALUES(?,?,'Selection')", student, parent).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO classes(id,tenant_id,category_id,name,type,price,is_published,enrollment_status,private_slot_limit) VALUES(?,?,?,'Selection','private',100000,true,'open',1)", class, tenant, category).Error; err != nil {
		t.Fatal(err)
	}
	parentToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"user_id": parent.String(), "is_parent": true, "email": "parent@example.test", "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	students, classes, enrollments := repository.NewStudentRepository(db), repository.NewClassRepository(db), repository.NewEnrollmentRepository(db)
	transactions := repository.NewTransactionManager(db)
	requests := handler.NewPrivateScheduleRequestHandler(usecase.NewPrivateScheduleRequestUsecase(repository.NewPrivateScheduleRequestRepositoryWithChat(db), students, classes, transactions, enrollments, repository.NewScheduleRepository(db), repository.NewSessionRepository(db), selectionAcceptanceBilling{}))
	protected.POST("/catalog/classes/:class_id/schedule-requests", requests.Create)
	protected.POST("/schedule-requests/:id/approve", requests.Approve)
	protected.GET("/schedule-requests/:id", requests.Get)
	enrollmentUC := usecase.NewEnrollmentUsecase(enrollments, students, classes, selectionAcceptanceBilling{}, transactions)
	protected.GET("/enrollments/:id", handler.NewEnrollmentQueryHandler(enrollmentUC).GetQuery)
	router.POST("/test/paid/:id", middleware.InternalServiceAuth("isolated-selection-callback"), handler.NewEnrollmentHandler(enrollmentUC).ActivateInternal)
	return map[string]string{"parentToken": parentToken, "student": student.String(), "class": class.String()}
}
