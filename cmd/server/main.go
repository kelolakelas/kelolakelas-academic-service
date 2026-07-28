package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"github.com/tutorin-id/tutorin-academic-service/internal/config"
	"github.com/tutorin-id/tutorin-academic-service/internal/delivery/http/handler"
	"github.com/tutorin-id/tutorin-academic-service/internal/domain"
	"github.com/tutorin-id/tutorin-academic-service/internal/repository"
	"github.com/tutorin-id/tutorin-academic-service/internal/usecase"
	"github.com/tutorin-id/tutorin-academic-service/pkg/database"
	"github.com/tutorin-id/tutorin-academic-service/pkg/grpcclient"
)

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
	categoryRepo := repository.NewCategoryRepository(db)
	classRepo := repository.NewClassRepository(db)

	// Initialize Usecases
	categoryUsecase := usecase.NewCategoryUsecase(categoryRepo, tenantClient)
	classUsecase := usecase.NewClassUsecase(classRepo, tenantClient)

	// Initialize Handlers
	categoryHandler := handler.NewCategoryHandler(categoryUsecase)
	classHandler := handler.NewClassHandler(classUsecase)

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

	// Routes
	r.POST("/api/v1/categories", categoryHandler.Create)
	r.POST("/api/v1/classes", classHandler.Create)

	slog.Info("Starting academic service", "port", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		slog.Error("Failed to start academic service", "error", err)
		os.Exit(1)
	}
}
