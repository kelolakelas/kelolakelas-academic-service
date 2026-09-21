package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/kelolakelas/kelolakelas-academic-service/docs"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/config"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/handler"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
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

	// Initialize gRPC Client
	tenantClient, err := grpcclient.NewTenantClient(cfg.IdentityGRPCHost, time.Duration(cfg.CatalogTenantInfoTimeout)*time.Millisecond)
	if err != nil {
		slog.Error("Failed to initialize identity gRPC client", "error", err)
		os.Exit(1)
	}
	defer tenantClient.Close()
	permissionClient, err := grpcclient.NewPermissionClient(cfg.IdentityGRPCHost)
	if err != nil {
		slog.Error("Failed to initialize identity permission client", "error", err)
		os.Exit(1)
	}
	defer permissionClient.Close()

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
	billingClient := billing.NewClient(cfg.BillingServiceURL, cfg.InternalServiceCredential)

	// Initialize Usecases
	categoryUsecase := usecase.NewCategoryUsecase(categoryRepo, tenantClient, txManager)
	classUsecase := usecase.NewClassUsecase(classRepo, scheduleRepo, sessionRepo, enrollmentRepo, tenantClient, txManager, categoryRepo)
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
	catalogHandler := handler.NewCatalogHandler(usecase.NewCatalogUsecase(repository.NewCatalogRepository(db), tenantClient, time.Duration(cfg.CatalogTenantInfoTTL)*time.Minute))

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
		apiV1.POST("/categories", middleware.RequirePermission(permissionClient, "category:create"), categoryHandler.Create)
		apiV1.DELETE("/categories/:id", middleware.RequirePermission(permissionClient, "category:delete"), categoryHandler.Delete)
		apiV1.GET("/classes", listHandler.ListClasses)
		apiV1.POST("/classes", middleware.RequirePermission(permissionClient, "class:create"), classHandler.Create)
		apiV1.POST("/classes/with-category", middleware.RequirePermission(permissionClient, "class:create"), classHandler.CreateWithCategory)
		apiV1.DELETE("/classes/:id", middleware.RequirePermission(permissionClient, "class:delete"), classHandler.Delete)
		apiV1.PATCH("/classes/:id", middleware.RequirePermission(permissionClient, "class:update"), classHandler.Update)
		apiV1.PATCH("/classes/:id/published", middleware.RequirePermission(permissionClient, "class:update"), classHandler.UpdatePublication)
		apiV1.GET("/schedules", listHandler.ListSchedules)
		// Student and enrollment routes are shared by tenant members and parents. Parents
		// hold ownership rather than a role, so the permission check applies only to
		// non-parent callers; the owned-resource rules inside each handler still decide
		// what a parent may reach. See ADR 0002.
		apiV1.GET("/students", middleware.RequirePermissionUnlessParent(permissionClient, "student:read"), studentHandler.List)
		apiV1.POST("/students", middleware.RequirePermissionUnlessParent(permissionClient, "student:create"), studentHandler.Create)
		apiV1.GET("/students/:id", middleware.RequirePermissionUnlessParent(permissionClient, "student:read"), studentHandler.Get)
		apiV1.PATCH("/students/:id", middleware.RequirePermissionUnlessParent(permissionClient, "student:update"), studentHandler.Update)
		apiV1.DELETE("/students/:id", middleware.RequirePermissionUnlessParent(permissionClient, "student:delete"), studentHandler.Delete)
		apiV1.GET("/attendance", attendanceHandler.List)
		apiV1.POST("/attendance", attendanceHandler.Create)
		apiV1.GET("/attendance/:id", attendanceHandler.Get)
		apiV1.PATCH("/attendance/:id", attendanceHandler.Update)
		apiV1.GET("/reports", reportHandler.List)
		apiV1.POST("/reports", reportHandler.Create)
		apiV1.GET("/reports/:id", reportHandler.Get)
		apiV1.PATCH("/reports/:id", reportHandler.Update)
		apiV1.DELETE("/reports/:id", reportHandler.Delete)
		apiV1.POST("/tenants/:tenant_id/enrollments", middleware.RequirePermissionUnlessParent(permissionClient, "enrollment:create"), enrollmentHandler.Create)
		apiV1.POST("/catalog/classes/:class_id/enrollments", enrollmentHandler.CreateCatalogEnrollment)
		apiV1.POST("/enrollments/:id/cancel", enrollmentHandler.Cancel)
		apiV1.GET("/enrollments", middleware.RequirePermissionUnlessParent(permissionClient, "enrollment:read"), enrollmentHandler.ListQuery)
		apiV1.GET("/enrollments/:id", middleware.RequirePermissionUnlessParent(permissionClient, "enrollment:read"), enrollmentHandler.GetQuery)
		apiV1.PATCH("/enrollments/:id/schedule", enrollmentHandler.AssignSchedule)

		// Schedule Routes
		apiV1.POST("/schedules", middleware.RequirePermission(permissionClient, "schedule:create"), scheduleHandler.CreateInitialSchedules)
		apiV1.DELETE("/schedules/:id", middleware.RequirePermission(permissionClient, "schedule:delete"), scheduleHandler.Delete)
		apiV1.PUT("/schedules/permanent", middleware.RequirePermission(permissionClient, "schedule:update"), scheduleHandler.ChangeSchedulePermanent)
		apiV1.PUT("/schedules/:id/permanent", middleware.RequirePermission(permissionClient, "schedule:update"), scheduleHandler.ChangeSchedulePermanent)
		apiV1.PATCH("/schedules/tutor-permanent", middleware.RequirePermission(permissionClient, "schedule:update"), scheduleHandler.ChangeTutorPermanent)
		apiV1.PATCH("/schedules/:id/tutor-permanent", middleware.RequirePermission(permissionClient, "schedule:update"), scheduleHandler.ChangeTutorPermanent)
		apiV1.PUT("/schedules/tutor-permanent", middleware.RequirePermission(permissionClient, "schedule:update"), scheduleHandler.ChangeTutorPermanent)
		apiV1.PUT("/schedules/:id/tutor-permanent", middleware.RequirePermission(permissionClient, "schedule:update"), scheduleHandler.ChangeTutorPermanent)

		// Session Routes
		apiV1.GET("/sessions", sessionHandler.ListSessions)
		apiV1.GET("/sessions/:id", sessionHandler.GetSession)
		apiV1.DELETE("/sessions/:id", middleware.RequirePermission(permissionClient, "schedule:update"), sessionHandler.DeleteSession)
		apiV1.POST("/sessions/reschedule", middleware.RequirePermission(permissionClient, "schedule:update"), scheduleHandler.RescheduleSession)
		apiV1.POST("/sessions/:id/reschedule", middleware.RequirePermission(permissionClient, "schedule:update"), scheduleHandler.RescheduleSession)
		apiV1.PATCH("/sessions/substitute-tutor", middleware.RequirePermission(permissionClient, "schedule:update"), scheduleHandler.ChangeTutorTemporary)
		apiV1.PATCH("/sessions/:id/substitute-tutor", middleware.RequirePermission(permissionClient, "schedule:update"), scheduleHandler.ChangeTutorTemporary)
		apiV1.GET("/sessions/:id/attendees", scheduleHandler.GetSessionAttendees)
	}
	internal := r.Group("/internal")
	internal.Use(middleware.InternalServiceAuth(cfg.InternalServiceCredential))
	internal.PUT("/enrollments/:id/activate", enrollmentHandler.ActivateInternal)
	internal.PUT("/enrollments/:id/release", enrollmentHandler.ReleaseInternal)

	slog.Info("Starting academic service", "port", cfg.Port)
	if err := r.Run("0.0.0.0:" + cfg.Port); err != nil {
		slog.Error("Failed to start academic service", "error", err)
		os.Exit(1)
	}
}
