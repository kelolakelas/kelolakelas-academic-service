package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

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
// @description User JWT as `Bearer <token>`. Tenant-member tokens are also checked against the permission named in each operation's `x-permission`.
// @securityDefinitions.apikey InternalServiceCredential
// @in header
// @name X-Internal-Service-Credential
// @description Shared service-to-service credential; accepted only on `/internal` routes.
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
	permissionClient, err := grpcclient.NewPermissionClient(cfg.IdentityGRPCHost, time.Duration(cfg.IdentityPermissionTimeoutMs)*time.Millisecond)
	if err != nil {
		slog.Error("Failed to initialize identity permission client", "error", err)
		os.Exit(1)
	}
	defer permissionClient.Close()
	// KEL-135: the substitute-tutor guard validates the candidate against the
	// membership row in identity. It reuses the permission-check bound so a
	// slow identity fails the substitution closed instead of holding it open.
	membershipClient, err := grpcclient.NewMembershipClient(cfg.IdentityGRPCHost, time.Duration(cfg.IdentityPermissionTimeoutMs)*time.Millisecond)
	if err != nil {
		slog.Error("Failed to initialize identity membership client", "error", err)
		os.Exit(1)
	}
	defer membershipClient.Close()
	catalogPolicyClient, err := grpcclient.NewCatalogPolicyClient(cfg.IdentityGRPCHost, time.Duration(cfg.CatalogPolicyTimeout)*time.Millisecond)
	if err != nil {
		slog.Error("Failed to initialize public catalog policy client", "error", err)
		os.Exit(1)
	}
	defer catalogPolicyClient.Close()

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
	scheduleUsecase := usecase.NewScheduleUsecase(txManager, classRepo, scheduleRepo, sessionRepo, enrollmentRepo, membershipClient)
	enrollmentUsecase := usecase.NewEnrollmentUsecase(enrollmentRepo, studentRepo, classRepo, billingClient, txManager)

	// Initialize Handlers
	categoryHandler := handler.NewCategoryHandler(categoryUsecase)
	classHandler := handler.NewClassHandler(classUsecase, classCreationUsecase)
	scheduleHandler := handler.NewScheduleHandler(scheduleUsecase)
	listHandler := handler.NewListHandler(categoryUsecase, classUsecase, scheduleUsecase)
	enrollmentHandler := handler.NewEnrollmentHandler(enrollmentUsecase)
	privateRequestHandler := handler.NewPrivateScheduleRequestHandler(usecase.NewPrivateScheduleRequestUsecase(repository.NewPrivateScheduleRequestRepository(db), studentRepo, classRepo, txManager, enrollmentRepo, scheduleRepo, sessionRepo, billingClient))
	chatContextHandler := handler.NewChatContextHandler(repository.NewChatContextRepository(db))
	sessionHandler := handler.NewSessionHandler(scheduleUsecase)
	studentHandler := handler.NewStudentHandler(usecase.NewStudentUsecase(studentRepo, studentNoteRepo, txManager))
	attendanceHandler := handler.NewAttendanceHandler(usecase.NewAttendanceUsecase(repository.NewAttendanceRepository(db), sessionRepo, enrollmentRepo))
	reportHandler := handler.NewReportHandler(usecase.NewReportUsecase(repository.NewReportRepository(db), enrollmentRepo))
	catalogUsecase := usecase.NewCatalogUsecase(repository.NewCatalogRepository(db), tenantClient, time.Duration(cfg.CatalogTenantInfoTTL)*time.Minute, catalogPolicyClient, time.Duration(cfg.CatalogPolicyCacheTTL)*time.Second)
	catalogHandler := handler.NewCatalogHandler(catalogUsecase)
	reviewHandler := handler.NewReviewHandler(repository.NewReviewRepository(db), catalogUsecase)

	// Initialize Router
	sqlDB, err := db.DB()
	if err != nil {
		slog.Error("Failed to access database pool", "error", err)
		os.Exit(1)
	}
	r := gin.New()
	r.Use(middleware.RequestLog(), gin.Recovery())
	registerRoutes(r, routeHandlers{
		health:         healthHandler("academic-service"),
		ready:          readinessHandler(sqlDB, cfg.IdentityGRPCHost),
		catalog:        catalogHandler,
		review:         reviewHandler,
		category:       categoryHandler,
		class:          classHandler,
		list:           listHandler,
		student:        studentHandler,
		attendance:     attendanceHandler,
		report:         reportHandler,
		enrollment:     enrollmentHandler,
		privateRequest: privateRequestHandler,
		chatContext:    chatContextHandler,
		schedule:       scheduleHandler,
		session:        sessionHandler,
	}, cfg.JWTSecret, cfg.InternalServiceCredential, permissionClient)

	httpServer := newHTTPServer(cfg, r)
	listener, err := net.Listen("tcp", httpServer.Addr)
	if err != nil {
		slog.Error("Failed to start academic service", "error", err)
		os.Exit(1)
	}
	// The signal handler stays registered for the whole shutdown, so a second SIGTERM
	// does not cut the drain short; the shutdown timeout still bounds the exit.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Every replica may run the worker: schedules are claimed with FOR UPDATE SKIP
	// LOCKED and sessions are inserted idempotently, so replicas share the work.
	// Cancelling ctx on shutdown rolls back a schedule still in progress; it stays due.
	if cfg.SessionGenerationWorkerEnabled {
		sessionGenerationWorker := usecase.NewSessionGenerationWorker(
			txManager,
			repository.NewSessionGenerationRepository(db),
			cfg.SessionGenerationHorizonMonths,
			time.Duration(cfg.SessionGenerationIntervalMinutes)*time.Minute,
		)
		go sessionGenerationWorker.Run(ctx)
	} else {
		slog.Warn("Session generation worker disabled; sessions after the current month are not generated on this replica")
	}

	slog.Info("Starting academic service",
		"port", cfg.Port,
		"server_read_header_timeout_seconds", cfg.ServerReadHeaderTimeout,
		"server_read_timeout_seconds", cfg.ServerReadTimeout,
		"server_write_timeout_seconds", cfg.ServerWriteTimeout,
		"server_idle_timeout_seconds", cfg.ServerIdleTimeout,
		"server_shutdown_timeout_seconds", cfg.ServerShutdownTimeout,
		"session_generation_worker_enabled", cfg.SessionGenerationWorkerEnabled,
		"session_generation_interval_minutes", cfg.SessionGenerationIntervalMinutes,
		"session_generation_horizon_months", cfg.SessionGenerationHorizonMonths,
	)
	if err := serveUntilDone(ctx, httpServer, listener, time.Duration(cfg.ServerShutdownTimeout)*time.Second); err != nil {
		slog.Error("Academic service stopped with error", "error", err)
		// os.Exit skips deferred calls, so release the identity clients first.
		tenantClient.Close()
		permissionClient.Close()
		membershipClient.Close()
		catalogPolicyClient.Close()
		os.Exit(1)
	}
}
