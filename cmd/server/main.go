package main

import (
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/kelolakelas/kelolakelas-academic-service/docs"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/config"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/handler"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/domain"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/repository"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/usecase"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/billing"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/database"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

// @title KelolaKelas Academic Service API
// @version 1.0
// @description Academic Management Service for KelolaKelas Platform
// @BasePath /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	// Initialize JSON logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Load Configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Initialize DB Connection
	db, err := database.NewPostgresDB(cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSSLMode, cfg.DBChannelBinding)
	if err != nil {
		slog.Error("Database connection failed", "error", err)
		os.Exit(1)
	}

	// Auto-migrate schema
	slog.Info("Running auto-migration...")
	if err := db.AutoMigrate(
		&domain.Category{},
		&domain.Class{},
		&domain.Student{},
		&domain.StudentNote{},
		&domain.Enrollment{},
		&domain.ClassSchedule{},
		&domain.ClassSession{},
		&domain.ClassTeacher{},
		&domain.Attendance{},
		&domain.Report{},
		&domain.TenantLocationSnapshot{},
	); err != nil {
		slog.Error("Auto-migration failed", "error", err)
		os.Exit(1)
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_classes_catalog_filters ON classes (is_published, enrollment_status, type, price, created_at) WHERE deleted_at IS NULL`,
		`DROP INDEX IF EXISTS idx_student_class`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_student_class_active ON enrollments (student_id, class_id) WHERE status IN ('pending', 'active') AND deleted_at IS NULL`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			slog.Error("Marketplace migration failed", "error", err)
			os.Exit(1)
		}
	}

	// Initialize gRPC Client
	tenantClient, err := grpcclient.NewTenantClient(cfg.IdentityGRPCHost)
	if err != nil {
		slog.Error("Failed to initialize identity gRPC client", "error", err)
		os.Exit(1)
	}
	defer tenantClient.Close()

	// Initialize Repositories
	txManager := repository.NewTransactionManager(db)
	categoryRepo := repository.NewCategoryRepository(db)
	classRepo := repository.NewClassRepository(db)
	classTeacherRepo := repository.NewClassTeacherRepository(db)
	scheduleRepo := repository.NewScheduleRepository(db)
	sessionRepo := repository.NewSessionRepository(db)
	enrollmentRepo := repository.NewEnrollmentRepository(db)
	studentRepo := repository.NewStudentRepository(db)
	studentNoteRepo := repository.NewStudentNoteRepository(db)
	billingClient := billing.NewClient(cfg.BillingServiceURL)

	// Initialize Usecases
	categoryUsecase := usecase.NewCategoryUsecase(categoryRepo, tenantClient, txManager)
	classUsecase := usecase.NewClassUsecase(classRepo, scheduleRepo, sessionRepo, enrollmentRepo, tenantClient, txManager)
	classCreationUsecase := usecase.NewClassCreationUsecase(txManager, categoryRepo, classRepo, classTeacherRepo, scheduleRepo, sessionRepo, tenantClient)
	scheduleUsecase := usecase.NewScheduleUsecase(txManager, classRepo, scheduleRepo, sessionRepo, enrollmentRepo)
	enrollmentUsecase := usecase.NewEnrollmentUsecase(enrollmentRepo, studentRepo, classRepo, billingClient, txManager)

	// Initialize Handlers
	categoryHandler := handler.NewCategoryHandler(categoryUsecase)
	classHandler := handler.NewClassHandler(classUsecase, classCreationUsecase)
	scheduleHandler := handler.NewScheduleHandler(scheduleUsecase)
	listHandler := handler.NewListHandler(categoryUsecase, classUsecase, scheduleUsecase)
	enrollmentHandler := handler.NewEnrollmentHandler(enrollmentUsecase)
	sessionHandler := handler.NewSessionHandler(scheduleUsecase)
	studentHandler := handler.NewStudentHandler(usecase.NewStudentUsecase(studentRepo, studentNoteRepo, txManager))
	attendanceHandler := handler.NewAttendanceHandler(usecase.NewAttendanceUsecase(repository.NewAttendanceRepository(db), sessionRepo))
	reportHandler := handler.NewReportHandler(usecase.NewReportUsecase(repository.NewReportRepository(db), enrollmentRepo))
	catalogHandler := handler.NewCatalogHandler(usecase.NewCatalogUsecase(repository.NewCatalogRepository(db), tenantClient))

	// Initialize Router
	r := gin.New()
	r.Use(gin.Recovery())

	// Health check endpoint
	r.GET("/health", healthHandler("academic-service"))

	// Swagger UI
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Routes
	apiV1 := r.Group("/api/v1")
	apiV1.GET("/catalog/classes", catalogHandler.ListClasses)
	apiV1.GET("/catalog/classes/:id", catalogHandler.GetClass)
	apiV1.Use(middleware.AuthMiddleware(cfg.JWTSecret))
	{
		apiV1.GET("/categories", listHandler.ListCategories)
		apiV1.POST("/categories", categoryHandler.Create)
		apiV1.DELETE("/categories/:id", categoryHandler.Delete)
		apiV1.GET("/classes", listHandler.ListClasses)
		apiV1.POST("/classes", classHandler.Create)
		apiV1.POST("/classes/with-category", classHandler.CreateWithCategory)
		apiV1.DELETE("/classes/:id", classHandler.Delete)
		apiV1.PATCH("/classes/:id/published", classHandler.UpdatePublication)
		apiV1.GET("/schedules", listHandler.ListSchedules)
		apiV1.GET("/students", studentHandler.List)
		apiV1.POST("/students", studentHandler.Create)
		apiV1.GET("/students/:id", studentHandler.Get)
		apiV1.PATCH("/students/:id", studentHandler.Update)
		apiV1.DELETE("/students/:id", studentHandler.Delete)
		apiV1.GET("/attendance", attendanceHandler.List)
		apiV1.POST("/attendance", attendanceHandler.Create)
		apiV1.GET("/attendance/:id", attendanceHandler.Get)
		apiV1.PATCH("/attendance/:id", attendanceHandler.Update)
		apiV1.GET("/reports", reportHandler.List)
		apiV1.POST("/reports", reportHandler.Create)
		apiV1.GET("/reports/:id", reportHandler.Get)
		apiV1.PATCH("/reports/:id", reportHandler.Update)
		apiV1.DELETE("/reports/:id", reportHandler.Delete)
		apiV1.POST("/tenants/:tenant_id/enrollments", enrollmentHandler.Create)
		apiV1.POST("/catalog/classes/:class_id/enrollments", enrollmentHandler.CreateCatalogEnrollment)
		apiV1.GET("/enrollments", enrollmentHandler.ListQuery)
		apiV1.GET("/enrollments/:id", enrollmentHandler.GetQuery)

		// Enrollment Routes
		apiV1.PUT("/enrollments/:id/status", enrollmentHandler.UpdateStatus)

		// Schedule Routes
		apiV1.POST("/schedules", scheduleHandler.CreateInitialSchedules)
		apiV1.DELETE("/schedules/:id", scheduleHandler.Delete)
		apiV1.PUT("/schedules/permanent", scheduleHandler.ChangeSchedulePermanent)
		apiV1.PUT("/schedules/:id/permanent", scheduleHandler.ChangeSchedulePermanent)
		apiV1.PATCH("/schedules/tutor-permanent", scheduleHandler.ChangeTutorPermanent)
		apiV1.PATCH("/schedules/:id/tutor-permanent", scheduleHandler.ChangeTutorPermanent)
		apiV1.PUT("/schedules/tutor-permanent", scheduleHandler.ChangeTutorPermanent)
		apiV1.PUT("/schedules/:id/tutor-permanent", scheduleHandler.ChangeTutorPermanent)

		// Session Routes
		apiV1.GET("/sessions", sessionHandler.ListSessions)
		apiV1.GET("/sessions/:id", sessionHandler.GetSession)
		apiV1.DELETE("/sessions/:id", sessionHandler.DeleteSession)
		apiV1.POST("/sessions/reschedule", scheduleHandler.RescheduleSession)
		apiV1.POST("/sessions/:id/reschedule", scheduleHandler.RescheduleSession)
		apiV1.PATCH("/sessions/substitute-tutor", scheduleHandler.ChangeTutorTemporary)
		apiV1.PATCH("/sessions/:id/substitute-tutor", scheduleHandler.ChangeTutorTemporary)
		apiV1.GET("/sessions/:id/attendees", scheduleHandler.GetSessionAttendees)
	}

	slog.Info("Starting academic service", "port", cfg.Port)
	if err := r.Run("0.0.0.0:" + cfg.Port); err != nil {
		slog.Error("Failed to start academic service", "error", err)
		os.Exit(1)
	}
}
