package infra

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/qiniu/go-sdk/v7/auth"
	"github.com/qiniu/go-sdk/v7/storage"

	"github.com/calliope/api/internal/config"
)

// OSSClient 封装七牛云对象存储操作。
type OSSClient struct {
	mac    *auth.Credentials
	cfg    config.OSSConfig
	region *storage.Zone
}

// NewOSSClient 初始化七牛云客户端，校验必要配置项。
func NewOSSClient(cfg config.OSSConfig) (*OSSClient, error) {
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("infra.NewOSSClient: QINIU_ACCESS_KEY and QINIU_SECRET_KEY are required")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("infra.NewOSSClient: QINIU_BUCKET is required")
	}

	zone, err := zoneFromRegion(cfg.Region)
	if err != nil {
		return nil, fmt.Errorf("infra.NewOSSClient: %w", err)
	}

	return &OSSClient{
		mac:    auth.New(cfg.AccessKey, cfg.SecretKey),
		cfg:    cfg,
		region: zone,
	}, nil
}

// Upload 将 data 上传到指定 key。
func (c *OSSClient) Upload(ctx context.Context, key string, data []byte) error {
	putPolicy := storage.PutPolicy{Scope: c.cfg.Bucket + ":" + key}
	upToken := putPolicy.UploadToken(c.mac)

	cfg := storage.Config{Zone: c.region, UseHTTPS: true}
	uploader := storage.NewFormUploader(&cfg)
	ret := storage.PutRet{}

	err := uploader.Put(ctx, &ret, upToken, key, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		return fmt.Errorf("infra.OSSClient.Upload: %w", err)
	}
	return nil
}

// Delete 删除指定 key 的文件，文件不存在时不报错（幂等）。
func (c *OSSClient) Delete(ctx context.Context, key string) error {
	cfg := storage.Config{Zone: c.region, UseHTTPS: true}
	mgr := storage.NewBucketManager(c.mac, &cfg)

	err := mgr.Delete(c.cfg.Bucket, key)
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("infra.OSSClient.Delete: %w", err)
	}
	return nil
}

// Exists 检查指定 key 的文件是否存在。
func (c *OSSClient) Exists(ctx context.Context, key string) (bool, error) {
	cfg := storage.Config{Zone: c.region, UseHTTPS: true}
	mgr := storage.NewBucketManager(c.mac, &cfg)

	_, err := mgr.Stat(c.cfg.Bucket, key)
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("infra.OSSClient.Exists: %w", err)
	}
	return true, nil
}

// SignURL 为私有 Bucket 中的 key 生成限时签名下载 URL。
func (c *OSSClient) SignURL(_ context.Context, key string, ttl time.Duration) (string, error) {
	if c.cfg.Domain == "" {
		return "", fmt.Errorf("infra.OSSClient.SignURL: QINIU_DOMAIN is required")
	}
	deadline := time.Now().Add(ttl).Unix()
	url := storage.MakePrivateURLv2(c.mac, c.cfg.Domain, key, deadline)
	return url, nil
}

// Copy 将 srcKey 复制到 dstKey（同 Bucket），目标已存在时强制覆盖。
func (c *OSSClient) Copy(_ context.Context, srcKey, dstKey string) error {
	cfg := storage.Config{Zone: c.region, UseHTTPS: true}
	mgr := storage.NewBucketManager(c.mac, &cfg)
	err := mgr.Copy(c.cfg.Bucket, srcKey, c.cfg.Bucket, dstKey, true)
	if err != nil {
		return fmt.Errorf("infra.OSSClient.Copy: %w", err)
	}
	return nil
}

// zoneFromRegion 将区域字符串映射到七牛云 Zone。
func zoneFromRegion(region string) (*storage.Zone, error) {
	switch region {
	case "z0":
		return &storage.ZoneHuadong, nil
	case "z1":
		return &storage.ZoneHuabei, nil
	case "z2":
		return &storage.ZoneHuanan, nil
	case "na0":
		return &storage.ZoneBeimei, nil
	case "as0":
		return &storage.ZoneXinjiapo, nil
	default:
		return nil, fmt.Errorf("unknown QINIU_REGION %q (valid: z0/z1/z2/na0/as0)", region)
	}
}

// isNotFound 判断七牛 SDK 返回的错误是否为 612（资源不存在）。
func isNotFound(err error) bool {
	if e, ok := err.(*storage.ErrorInfo); ok {
		return e.Code == 612
	}
	return false
}
