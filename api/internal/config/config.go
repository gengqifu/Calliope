package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	DB    DBConfig
	Redis RedisConfig
	App   AppConfig
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

	connMaxLifetime, err := time.ParseDuration(v.GetString("DB_CONN_MAX_LIFETIME"))
	if err != nil {
		return nil, fmt.Errorf("config.Load: invalid DB_CONN_MAX_LIFETIME: %w", err)
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
		App: AppConfig{
			Env:      v.GetString("APP_ENV"),
			LogLevel: v.GetString("APP_LOG_LEVEL"),
		},
	}, nil
}
