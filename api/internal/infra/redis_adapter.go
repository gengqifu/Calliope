package infra

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisAdapter wraps *redis.Client and satisfies service.RedisClient.
type RedisAdapter struct {
	client *redis.Client
}

// NewRedisAdapter returns a RedisAdapter backed by the given client.
func NewRedisAdapter(client *redis.Client) *RedisAdapter {
	return &RedisAdapter{client: client}
}

func (a *RedisAdapter) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return a.client.Set(ctx, key, value, ttl).Err()
}

func (a *RedisAdapter) Get(ctx context.Context, key string) (string, error) {
	return a.client.Get(ctx, key).Result()
}

func (a *RedisAdapter) Del(ctx context.Context, keys ...string) error {
	return a.client.Del(ctx, keys...).Err()
}

func (a *RedisAdapter) Incr(ctx context.Context, key string) (int64, error) {
	return a.client.Incr(ctx, key).Result()
}

func (a *RedisAdapter) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return a.client.Expire(ctx, key, ttl).Err()
}

func (a *RedisAdapter) Exists(ctx context.Context, key string) (bool, error) {
	n, err := a.client.Exists(ctx, key).Result()
	return n > 0, err
}

func (a *RedisAdapter) Decr(ctx context.Context, key string) (int64, error) {
	return a.client.Decr(ctx, key).Result()
}

func (a *RedisAdapter) Eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error) {
	return a.client.Eval(ctx, script, keys, args...).Result()
}

func (a *RedisAdapter) Publish(ctx context.Context, channel string, message interface{}) error {
	return a.client.Publish(ctx, channel, message).Err()
}
