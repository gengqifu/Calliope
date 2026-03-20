package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	DB    DBConfig
	Redis RedisConfig
	OSS   OSSConfig
	App   AppConfig
	Auth  AuthConfig
	Task  TaskConfig
}

type TaskConfig struct {
	InternalCallbackSecret  string
	QueueDepthMax           int
	TaskTimeoutSec          int
	SignedURLTTL            time.Duration
	ExpectedInferenceSec    int           // 伪进度计算用，推理预期耗时
	TimeoutScanInterval     time.Duration // 超时扫描定时任务间隔
}

type AuthConfig struct {
	JWTSecret        string
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
	RefreshTokenLong time.Duration
	MaxLoginAttempts int
	LockDuration     time.Duration
}

type OSSConfig struct {
	AccessKey string
	SecretKey string
	Bucket    string
	Domain    string // 下载域名，用于生成签名 URL
	Region    string // 存储区域，如 z0/z1/z2
}

type DBConfig struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type AppConfig struct {
	Env      string
	LogLevel string
}

// Load reads configuration from .env file and environment variables.
// Environment variables take precedence over .env file values.
func Load() (*Config, error) {
	v := viper.New()
	v.AutomaticEnv()

	v.SetDefault("DB_MAX_OPEN_CONNS", 25)
	v.SetDefault("DB_MAX_IDLE_CONNS", 10)
	v.SetDefault("DB_CONN_MAX_LIFETIME", "5m")
	v.SetDefault("REDIS_ADDR", "localhost:6379")
	v.SetDefault("REDIS_PASSWORD", "")
	v.SetDefault("REDIS_DB", 0)
	v.SetDefault("APP_ENV", "development")
	v.SetDefault("APP_LOG_LEVEL", "info")
	v.SetDefault("QINIU_REGION", "z2")
	v.SetDefault("AUTH_ACCESS_TOKEN_TTL", "15m")
	v.SetDefault("AUTH_REFRESH_TOKEN_TTL", "168h")  // 7 days
	v.SetDefault("AUTH_REFRESH_TOKEN_LONG", "720h") // 30 days
	v.SetDefault("AUTH_MAX_LOGIN_ATTEMPTS", 5)
	v.SetDefault("AUTH_LOCK_DURATION", "15m")
	v.SetDefault("TASK_QUEUE_DEPTH_MAX", 20)
	v.SetDefault("TASK_TIMEOUT_SEC", 180)
	v.SetDefault("TASK_SIGNED_URL_TTL", "1h")
	v.SetDefault("TASK_EXPECTED_INFERENCE_SEC", 120)
	v.SetDefault("TASK_TIMEOUT_SCAN_INTERVAL", "30s")

	v.SetConfigFile(".env")
	v.SetConfigType("env")
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Ignore missing .env file; real errors are reported
			return nil, fmt.Errorf("config.Load: %w", err)
		}
	}

	if v.GetString("DB_DSN") == "" {
		return nil, fmt.Errorf("config.Load: DB_DSN is required")
	}
	if v.GetString("JWT_SECRET") == "" {
		return nil, fmt.Errorf("config.Load: JWT_SECRET is required")
	}
	if v.GetString("INTERNAL_CALLBACK_SECRET") == "" {
		return nil, fmt.Errorf("config.Load: INTERNAL_CALLBACK_SECRET is required")
	}

	connMaxLifetime, err := time.ParseDuration(v.GetString("DB_CONN_MAX_LIFETIME"))
	if err != nil {
		return nil, fmt.Errorf("config.Load: invalid DB_CONN_MAX_LIFETIME: %w", err)
	}

	accessTTL, err := time.ParseDuration(v.GetString("AUTH_ACCESS_TOKEN_TTL"))
	if err != nil {
		return nil, fmt.Errorf("config.Load: invalid AUTH_ACCESS_TOKEN_TTL: %w", err)
	}
	refreshTTL, err := time.ParseDuration(v.GetString("AUTH_REFRESH_TOKEN_TTL"))
	if err != nil {
		return nil, fmt.Errorf("config.Load: invalid AUTH_REFRESH_TOKEN_TTL: %w", err)
	}
	refreshLong, err := time.ParseDuration(v.GetString("AUTH_REFRESH_TOKEN_LONG"))
	if err != nil {
		return nil, fmt.Errorf("config.Load: invalid AUTH_REFRESH_TOKEN_LONG: %w", err)
	}
	lockDuration, err := time.ParseDuration(v.GetString("AUTH_LOCK_DURATION"))
	if err != nil {
		return nil, fmt.Errorf("config.Load: invalid AUTH_LOCK_DURATION: %w", err)
	}

	signedURLTTL, err := time.ParseDuration(v.GetString("TASK_SIGNED_URL_TTL"))
	if err != nil {
		return nil, fmt.Errorf("config.Load: invalid TASK_SIGNED_URL_TTL: %w", err)
	}
	timeoutScanInterval, err := time.ParseDuration(v.GetString("TASK_TIMEOUT_SCAN_INTERVAL"))
	if err != nil {
		return nil, fmt.Errorf("config.Load: invalid TASK_TIMEOUT_SCAN_INTERVAL: %w", err)
	}

	return &Config{
		DB: DBConfig{
			DSN:             v.GetString("DB_DSN"),
			MaxOpenConns:    v.GetInt("DB_MAX_OPEN_CONNS"),
			MaxIdleConns:    v.GetInt("DB_MAX_IDLE_CONNS"),
			ConnMaxLifetime: connMaxLifetime,
		},
		Redis: RedisConfig{
			Addr:     v.GetString("REDIS_ADDR"),
			Password: v.GetString("REDIS_PASSWORD"),
			DB:       v.GetInt("REDIS_DB"),
		},
		OSS: OSSConfig{
			AccessKey: v.GetString("QINIU_ACCESS_KEY"),
			SecretKey: v.GetString("QINIU_SECRET_KEY"),
			Bucket:    v.GetString("QINIU_BUCKET"),
			Domain:    v.GetString("QINIU_DOMAIN"),
			Region:    v.GetString("QINIU_REGION"),
		},
		App: AppConfig{
			Env:      v.GetString("APP_ENV"),
			LogLevel: v.GetString("APP_LOG_LEVEL"),
		},
		Auth: AuthConfig{
			JWTSecret:        v.GetString("JWT_SECRET"),
			AccessTokenTTL:   accessTTL,
			RefreshTokenTTL:  refreshTTL,
			RefreshTokenLong: refreshLong,
			MaxLoginAttempts: v.GetInt("AUTH_MAX_LOGIN_ATTEMPTS"),
			LockDuration:     lockDuration,
		},
		Task: TaskConfig{
			InternalCallbackSecret: v.GetString("INTERNAL_CALLBACK_SECRET"),
			QueueDepthMax:          v.GetInt("TASK_QUEUE_DEPTH_MAX"),
			TaskTimeoutSec:         v.GetInt("TASK_TIMEOUT_SEC"),
			SignedURLTTL:           signedURLTTL,
			ExpectedInferenceSec:   v.GetInt("TASK_EXPECTED_INFERENCE_SEC"),
			TimeoutScanInterval:    timeoutScanInterval,
		},
	}, nil
}
