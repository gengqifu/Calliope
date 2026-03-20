//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/calliope/api/internal/config"
	"github.com/calliope/api/internal/infra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getTestOSSConfig() (config.OSSConfig, bool) {
	ak := os.Getenv("TEST_QINIU_ACCESS_KEY")
	sk := os.Getenv("TEST_QINIU_SECRET_KEY")
	if ak == "" || sk == "" {
		return config.OSSConfig{}, false
	}
	bucket := os.Getenv("TEST_QINIU_BUCKET")
	if bucket == "" {
		bucket = "calliope-dev"
	}
	return config.OSSConfig{
		AccessKey: ak,
		SecretKey: sk,
		Bucket:    bucket,
		Domain:    os.Getenv("TEST_QINIU_DOMAIN"),
		Region:    "z2",
	}, true
}

// TestOSSClient_UploadAndDelete 验证上传后文件存在，删除后文件消失
func TestOSSClient_UploadAndDelete(t *testing.T) {
	cfg, ok := getTestOSSConfig()
	if !ok {
		t.Skip("TEST_QINIU_ACCESS_KEY / TEST_QINIU_SECRET_KEY not set, skipping OSS tests")
	}

	client, err := infra.NewOSSClient(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	key := "test/calliope-oss-test.txt"
	content := []byte("calliope oss integration test")

	// 上传
	err = client.Upload(ctx, key, content)
	require.NoError(t, err)

	// 确认文件存在
	exists, err := client.Exists(ctx, key)
	require.NoError(t, err)
	assert.True(t, exists, "file should exist after upload")

	// 删除
	err = client.Delete(ctx, key)
	require.NoError(t, err)

	// 确认文件已删除
	exists, err = client.Exists(ctx, key)
	require.NoError(t, err)
	assert.False(t, exists, "file should not exist after delete")
}

// TestOSSClient_SignURL 验证签名 URL 可生成且包含 key 路径
func TestOSSClient_SignURL(t *testing.T) {
	cfg, ok := getTestOSSConfig()
	if !ok {
		t.Skip("TEST_QINIU_ACCESS_KEY / TEST_QINIU_SECRET_KEY not set, skipping OSS tests")
	}
	if cfg.Domain == "" {
		t.Skip("TEST_QINIU_DOMAIN not set, skipping SignURL test")
	}

	client, err := infra.NewOSSClient(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := "audio/test-file.mp3"
	url, err := client.SignURL(ctx, key, 10*time.Minute)
	require.NoError(t, err)
	assert.True(t, strings.Contains(url, key), "signed URL should contain the key path")
	assert.True(t, strings.HasPrefix(url, "http"), "signed URL should be a valid http(s) URL")
}

// TestOSSClient_DeleteNonExistent 验证删除不存在的文件不报错（幂等）
func TestOSSClient_DeleteNonExistent(t *testing.T) {
	cfg, ok := getTestOSSConfig()
	if !ok {
		t.Skip("TEST_QINIU_ACCESS_KEY / TEST_QINIU_SECRET_KEY not set, skipping OSS tests")
	}

	client, err := infra.NewOSSClient(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = client.Delete(ctx, "test/non-existent-file-xyz.txt")
	assert.NoError(t, err, "deleting non-existent file should be idempotent")
}
