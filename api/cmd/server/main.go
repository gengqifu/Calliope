package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/calliope/api/internal/config"
	"github.com/calliope/api/internal/handler"
	"github.com/calliope/api/internal/infra"
	"github.com/calliope/api/internal/middleware"
	"github.com/calliope/api/internal/repository"
	"github.com/calliope/api/internal/scheduler"
	"github.com/calliope/api/internal/service"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := infra.NewDB(cfg.DB)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("failed to get sql.DB: %v", err)
	}
	defer sqlDB.Close()

	rdbClient, err := infra.NewRedisClient(cfg.Redis)
	if err != nil {
		log.Fatalf("failed to connect to Redis: %v", err)
	}
	defer rdbClient.Close()

	// Wire dependencies
	rdb := infra.NewRedisAdapter(rdbClient)
	userRepo := repository.NewUserRepository(db)
	authSvc := service.NewAuthService(service.AuthConfig{
		JWTSecret:        cfg.Auth.JWTSecret,
		AccessTokenTTL:   cfg.Auth.AccessTokenTTL,
		RefreshTokenTTL:  cfg.Auth.RefreshTokenTTL,
		RefreshTokenLong: cfg.Auth.RefreshTokenLong,
		MaxLoginAttempts: cfg.Auth.MaxLoginAttempts,
		LockDuration:     cfg.Auth.LockDuration,
	}, userRepo, rdb)
	authHandler := handler.NewAuthHandler(authSvc)

	ossClient, err := infra.NewOSSClient(cfg.OSS)
	if err != nil {
		log.Fatalf("failed to init OSS client: %v", err)
	}

	taskRepo := repository.NewTaskRepository(db)
	creditRepo := repository.NewCreditRepository(db)
	taskSvc := service.NewTaskService(service.TaskServiceConfig{
		QueueDepthMax:        cfg.Task.QueueDepthMax,
		TaskTimeoutSec:       cfg.Task.TaskTimeoutSec,
		SignedURLTTL:         cfg.Task.SignedURLTTL,
		ExpectedInferenceSec: cfg.Task.ExpectedInferenceSec,
	}, taskRepo, creditRepo, rdb, ossClient)
	taskHandler := handler.NewTaskHandler(taskSvc)
	internalHandler := handler.NewInternalHandler(taskSvc)

	workRepo := repository.NewWorkRepository(db)
	workSvc := service.NewWorkService(service.WorkServiceConfig{
		SignedURLTTL: cfg.Task.SignedURLTTL,
	}, workRepo, taskRepo, ossClient)
	workHandler := handler.NewWorkHandler(workSvc)

	// Background scheduler: fix timed-out tasks
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheduler.StartTaskTimeoutScheduler(ctx, taskSvc, cfg.Task.TimeoutScanInterval)

	// Router
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(middleware.Error())

	v1 := r.Group("/api/v1")
	{
		auth := v1.Group("/auth")
		auth.POST("/register", authHandler.Register)
		auth.POST("/login", authHandler.Login)
		auth.POST("/refresh", authHandler.Refresh)
		auth.POST("/logout", middleware.Auth(cfg.Auth.JWTSecret), authHandler.Logout)

		tasks := v1.Group("/tasks", middleware.Auth(cfg.Auth.JWTSecret))
		tasks.POST("", taskHandler.Create)
		tasks.GET("/:task_id", taskHandler.Get)

		works := v1.Group("/works", middleware.Auth(cfg.Auth.JWTSecret))
		works.POST("", workHandler.Save)
		works.GET("", workHandler.List)
		works.GET("/:work_id", workHandler.Get)
		works.GET("/:work_id/download", workHandler.GetDownloadURL)
		works.DELETE("/:work_id", workHandler.Delete)
	}

	// Internal routes (Python Worker → Go API), no JWT, shared secret auth
	internal := r.Group("/internal", handler.InternalAuth(cfg.Task.InternalCallbackSecret))
	internal.POST("/tasks/:task_id/status", internalHandler.UpdateStatus)

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background
	go func() {
		log.Printf("Calliope API server starting on :8080 (env=%s)...", cfg.App.Env)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Wait for SIGINT / SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Stop scheduler first (no new timeout scans)
	cancel()

	// Give in-flight HTTP requests up to 10 seconds to finish
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("server forced to shutdown: %v", err)
	}
	log.Println("Server exited")
}
