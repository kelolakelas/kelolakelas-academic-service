package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/kelolakelas/kelolakelas-academic-service/docs"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/config"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/handler"
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
	db, err := database.NewPostgresDB(cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName)
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
		&domain.Enrollment{},
		&domain.ClassSchedule{},
		&domain.ClassSession{},
	); err != nil {
		slog.Error("Auto-migration failed", "error", err)
		os.Exit(1)
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
	scheduleRepo := repository.NewScheduleRepository(db)
	sessionRepo := repository.NewSessionRepository(db)
	enrollmentRepo := repository.NewEnrollmentRepository(db)
	studentRepo := repository.NewStudentRepository(db)
	billingClient := billing.NewClient(cfg.BillingServiceURL)

	// Initialize Usecases
	categoryUsecase := usecase.NewCategoryUsecase(categoryRepo, tenantClient)
	classUsecase := usecase.NewClassUsecase(classRepo, tenantClient)
	scheduleUsecase := usecase.NewScheduleUsecase(txManager, classRepo, scheduleRepo, sessionRepo, enrollmentRepo)
	enrollmentUsecase := usecase.NewEnrollmentUsecase(enrollmentRepo, studentRepo, classRepo, billingClient)

	// Initialize Handlers
	categoryHandler := handler.NewCategoryHandler(categoryUsecase)
	classHandler := handler.NewClassHandler(classUsecase)
	scheduleHandler := handler.NewScheduleHandler(scheduleUsecase)
	enrollmentHandler := handler.NewEnrollmentHandler(enrollmentUsecase)

	// Initialize Router
	r := gin.New()
	r.Use(gin.Recovery())

	// Health check endpoint
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "academic-service",
		})
	})

	// Swagger UI
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Routes
	apiV1 := r.Group("/api/v1")
	{
		apiV1.POST("/categories", categoryHandler.Create)
		apiV1.POST("/classes", classHandler.Create)
		apiV1.POST("/tenants/:tenant_id/enrollments", enrollmentHandler.Create)

		// Enrollment Routes
		apiV1.PUT("/enrollments/:id/status", enrollmentHandler.UpdateStatus)

		// Schedule Routes
		apiV1.POST("/schedules", scheduleHandler.CreateInitialSchedules)
		apiV1.PUT("/schedules/permanent", scheduleHandler.ChangeSchedulePermanent)
		apiV1.PUT("/schedules/:id/permanent", scheduleHandler.ChangeSchedulePermanent)
		apiV1.PATCH("/schedules/tutor-permanent", scheduleHandler.ChangeTutorPermanent)
		apiV1.PATCH("/schedules/:id/tutor-permanent", scheduleHandler.ChangeTutorPermanent)
		apiV1.PUT("/schedules/tutor-permanent", scheduleHandler.ChangeTutorPermanent)
		apiV1.PUT("/schedules/:id/tutor-permanent", scheduleHandler.ChangeTutorPermanent)

		// Session Routes
		apiV1.POST("/sessions/reschedule", scheduleHandler.RescheduleSession)
		apiV1.POST("/sessions/:id/reschedule", scheduleHandler.RescheduleSession)
		apiV1.PATCH("/sessions/substitute-tutor", scheduleHandler.ChangeTutorTemporary)
		apiV1.PATCH("/sessions/:id/substitute-tutor", scheduleHandler.ChangeTutorTemporary)
		apiV1.GET("/sessions/:id/attendees", scheduleHandler.GetSessionAttendees)
	}

	slog.Info("Starting academic service", "port", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		slog.Error("Failed to start academic service", "error", err)
		os.Exit(1)
	}
}
