package main

import (
	"log"

	"github.com/gin-gonic/gin"

	"github.com/calliope/api/internal/config"
	"github.com/calliope/api/internal/handler"
	"github.com/calliope/api/internal/infra"
	"github.com/calliope/api/internal/middleware"
	"github.com/calliope/api/internal/repository"
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
	}

	log.Printf("Calliope API server starting on :8080 (env=%s)...", cfg.App.Env)
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
