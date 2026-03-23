//go:build integration

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/calliope/api/internal/infra"
	"github.com/calliope/api/internal/service"
)

// newTestClient 返回指向测试 DB 的原生 redis.Client（用于断言底层数据）。
func newTestClient(t *testing.T) *redis.Client {
	t.Helper()
	client, err := infra.NewRedisClient(getTestRedisConfig())
	require.NoError(t, err)
	t.Cleanup(func() { client.Close() })
	return client
}

// streamKeys 返回本次测试独占的 stream / depth key，避免并发测试互相干扰。
func streamKeys(t *testing.T) (streamKey, depthKey string) {
	suffix := t.Name()
	return "calliope:test:stream:" + suffix,
		"calliope:test:queue:depth:" + suffix
}

// evalXADD 封装 Lua 脚本调用，返回 [streamID, depth] 或 error。
func evalXADD(ctx context.Context, client *redis.Client, streamKey, depthKey string,
	taskID, userID uint64, prompt, mode string, createdAt time.Time, maxDepth int,
) (string, int64, error) {
	result, err := client.Eval(ctx, service.XaddIncrScript,
		[]string{streamKey, depthKey},
		strconv.FormatUint(taskID, 10),
		strconv.FormatUint(userID, 10),
		prompt,
		mode,
		createdAt.UTC().Format(time.RFC3339),
		strconv.Itoa(maxDepth),
	).Result()
	if err != nil {
		return "", 0, err
	}
	arr, ok := result.([]interface{})
	if !ok || len(arr) != 2 {
		return "", 0, fmt.Errorf("unexpected result shape: %v", result)
	}
	streamID, _ := arr[0].(string)
	depth, _ := arr[1].(int64)
	return streamID, depth, nil
}

// TestStream_XADDLua_Success 验证正常入队：depth 递增、stream 有消息、字段正确。
func TestStream_XADDLua_Success(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	streamKey, depthKey := streamKeys(t)
	defer client.Del(ctx, streamKey, depthKey)

	streamID, depth, err := evalXADD(ctx, client, streamKey, depthKey,
		42, 1, "electronic beats", "vocal", time.Now(), 20)

	require.NoError(t, err)
	assert.NotEmpty(t, streamID, "应返回 stream entry ID")
	assert.Equal(t, int64(1), depth, "depth 应为 1")

	// 验证 stream 中的消息字段
	msgs, err := client.XRange(ctx, streamKey, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "42", msgs[0].Values["task_id"])
	assert.Equal(t, "1", msgs[0].Values["user_id"])
	assert.Equal(t, "electronic beats", msgs[0].Values["prompt"])
	assert.Equal(t, "vocal", msgs[0].Values["mode"])
	assert.NotEmpty(t, msgs[0].Values["created_at"])
}

// TestStream_XADDLua_MultipleEnqueues 验证多次入队后 depth 累加、stream 消息数正确。
func TestStream_XADDLua_MultipleEnqueues(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	streamKey, depthKey := streamKeys(t)
	defer client.Del(ctx, streamKey, depthKey)

	for i := 1; i <= 3; i++ {
		_, depth, err := evalXADD(ctx, client, streamKey, depthKey,
			uint64(i), 1, "prompt", "instrumental", time.Now(), 20)
		require.NoError(t, err)
		assert.Equal(t, int64(i), depth)
	}

	msgs, err := client.XRange(ctx, streamKey, "-", "+").Result()
	require.NoError(t, err)
	assert.Len(t, msgs, 3, "stream 应有 3 条消息")
}

// TestStream_XADDLua_QueueFull 验证队满时返回 QUEUE_FULL、depth 不变、stream 不增。
func TestStream_XADDLua_QueueFull(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	streamKey, depthKey := streamKeys(t)
	defer client.Del(ctx, streamKey, depthKey)

	// 先入队一条，depth=1，maxDepth=1 → 再入队应触发 QUEUE_FULL
	_, _, err := evalXADD(ctx, client, streamKey, depthKey,
		1, 1, "first", "vocal", time.Now(), 1)
	require.NoError(t, err)

	// 第二次入队，超上限
	_, _, err = evalXADD(ctx, client, streamKey, depthKey,
		2, 1, "second", "vocal", time.Now(), 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "QUEUE_FULL")

	// depth 回滚至 1，不应变为 2
	depthStr, err := client.Get(ctx, depthKey).Result()
	require.NoError(t, err)
	assert.Equal(t, "1", depthStr, "depth 应回滚，保持为 1")

	// stream 仍只有 1 条消息
	msgs, err := client.XRange(ctx, streamKey, "-", "+").Result()
	require.NoError(t, err)
	assert.Len(t, msgs, 1, "队满时不应写入 stream")
}

// TestStream_XADDLua_ExactLimit 验证 depth == maxDepth 时仍可入队（边界值）。
func TestStream_XADDLua_ExactLimit(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	streamKey, depthKey := streamKeys(t)
	defer client.Del(ctx, streamKey, depthKey)

	// maxDepth=3，入队 3 次都应成功
	for i := 1; i <= 3; i++ {
		_, depth, err := evalXADD(ctx, client, streamKey, depthKey,
			uint64(i), 1, "prompt", "vocal", time.Now(), 3)
		require.NoError(t, err, "第 %d 次入队应成功", i)
		assert.Equal(t, int64(i), depth)
	}

	// 第 4 次应失败
	_, _, err := evalXADD(ctx, client, streamKey, depthKey,
		4, 1, "prompt", "vocal", time.Now(), 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "QUEUE_FULL")
}

// TestStream_Publish_Subscribe 验证 Worker 回调后 WebSocket 频道能收到消息。
func TestStream_Publish_Subscribe(t *testing.T) {
	client := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	channel := "calliope:ws:task:999"

	sub := client.Subscribe(ctx, channel)
	defer sub.Close()

	// 等待订阅就绪
	_, err := sub.Receive(ctx)
	require.NoError(t, err)

	// 发布消息（模拟 task_service.publishWSMessage）
	payload, _ := json.Marshal(map[string]interface{}{
		"task_id": 999,
		"status":  "completed",
	})
	err = client.Publish(ctx, channel, string(payload)).Err()
	require.NoError(t, err)

	msg, err := sub.ReceiveMessage(ctx)
	require.NoError(t, err)
	assert.Equal(t, channel, msg.Channel)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(msg.Payload), &body))
	assert.Equal(t, float64(999), body["task_id"])
	assert.Equal(t, "completed", body["status"])
}

// TestStream_Publish_Subscribe_Failed 验证 failed 状态携带 fail_reason 字段。
func TestStream_Publish_Subscribe_Failed(t *testing.T) {
	client := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	channel := "calliope:ws:task:888"

	sub := client.Subscribe(ctx, channel)
	defer sub.Close()

	_, err := sub.Receive(ctx)
	require.NoError(t, err)

	reason := "推理超时"
	payload, _ := json.Marshal(map[string]interface{}{
		"task_id":     888,
		"status":      "failed",
		"fail_reason": reason,
	})
	err = client.Publish(ctx, channel, string(payload)).Err()
	require.NoError(t, err)

	msg, err := sub.ReceiveMessage(ctx)
	require.NoError(t, err)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(msg.Payload), &body))
	assert.Equal(t, "failed", body["status"])
	assert.Equal(t, reason, body["fail_reason"])
}
