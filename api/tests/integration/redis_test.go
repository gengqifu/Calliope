//go:build integration

package integration

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/calliope/api/internal/config"
	"github.com/calliope/api/internal/infra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getTestRedisConfig() config.RedisConfig {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	db := 1 // 与生产/开发默认的 DB 0 隔离，避免测试数据干扰
	if v := os.Getenv("TEST_REDIS_DB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			db = n
		}
	}
	return config.RedisConfig{
		Addr:     addr,
		Password: os.Getenv("TEST_REDIS_PASSWORD"),
		DB:       db,
	}
}

// TestNewRedisClient_Ping 验证连接可达
func TestNewRedisClient_Ping(t *testing.T) {
	client, err := infra.NewRedisClient(getTestRedisConfig())
	require.NoError(t, err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = client.Ping(ctx).Err()
	assert.NoError(t, err)
}

// TestNewRedisClient_SetGet 验证基本读写
func TestNewRedisClient_SetGet(t *testing.T) {
	client, err := infra.NewRedisClient(getTestRedisConfig())
	require.NoError(t, err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	key := "calliope:test:redis_module"
	err = client.Set(ctx, key, "ok", 10*time.Second).Err()
	require.NoError(t, err)
	defer client.Del(ctx, key)

	val, err := client.Get(ctx, key).Result()
	require.NoError(t, err)
	assert.Equal(t, "ok", val)
}

// TestNewRedisClient_Expiry 验证 TTL 写入
func TestNewRedisClient_Expiry(t *testing.T) {
	client, err := infra.NewRedisClient(getTestRedisConfig())
	require.NoError(t, err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	key := "calliope:test:expiry"
	err = client.Set(ctx, key, "ttl", 5*time.Second).Err()
	require.NoError(t, err)
	defer client.Del(ctx, key)

	ttl, err := client.TTL(ctx, key).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl.Seconds(), float64(0), "TTL should be positive")
}

// TestNewRedisClient_BadAddr 验证连接失败时返回错误
func TestNewRedisClient_BadAddr(t *testing.T) {
	_, err := infra.NewRedisClient(config.RedisConfig{
		Addr: "localhost:19999", // 不存在的端口
		DB:   0,
	})
	assert.Error(t, err)
}
