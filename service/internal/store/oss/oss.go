// Package oss 封装阿里云 OSS，提供预签名上传/下载 URL。
package oss

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"

	"im/service/internal/config"
)

// Client 封装阿里云 OSS 客户端。
type Client struct {
	cfg    config.OSS
	cli    *oss.Client
	bucket *oss.Bucket
}

// New 根据配置创建 OSS 客户端，并尝试获取 Bucket。
func New(cfg config.OSS) (*Client, error) {
	if cfg.Endpoint == "" || cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" || cfg.Bucket == "" {
		return nil, fmt.Errorf("oss: incomplete configuration")
	}
	cli, err := oss.New(cfg.Endpoint, cfg.AccessKeyID, cfg.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("oss new client: %w", err)
	}
	bucket, err := cli.Bucket(cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("oss get bucket: %w", err)
	}
	return &Client{cfg: cfg, cli: cli, bucket: bucket}, nil
}

// PresignPut 生成 objectKey 的预签名上传（PUT）URL。
// 签名会绑定 contentType，客户端上传时必须携带相同的 Content-Type 头，否则 OSS 校验签名失败。
func (c *Client) PresignPut(objectKey string, contentType string) (string, error) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return c.bucket.SignURL(objectKey, oss.HTTPPut, int64(c.cfg.PresignExpireSec), oss.ContentType(contentType))
}

// PresignGet 生成 objectKey 的预签名下载（GET）URL。
func (c *Client) PresignGet(objectKey string) (string, error) {
	return c.bucket.SignURL(objectKey, oss.HTTPGet, int64(c.cfg.PresignExpireSec))
}

// PutObject 服务端直传对象（备用）。
func (c *Client) PutObject(objectKey string, data []byte) error {
	return c.bucket.PutObject(objectKey, bytes.NewReader(data))
}

// EnsureCORS 为 Bucket 配置跨域规则，允许前端直传（PUT）与读取（GET）。
// OSS 默认不允许浏览器跨域直传，不配置会导致 CORS 拦截（403 No 'Access-Control-Allow-Origin'）。
func (c *Client) EnsureCORS(allowOrigins []string) error {
	if len(allowOrigins) == 0 {
		allowOrigins = []string{"*"}
	}
	rules := []oss.CORSRule{
		{
			AllowedOrigin: allowOrigins,
			AllowedMethod: []string{"GET", "PUT", "POST", "DELETE", "HEAD"},
			AllowedHeader: []string{"*"},
			ExposeHeader:  []string{"ETag", "Content-Length", "x-oss-request-id"},
			MaxAgeSeconds: 600,
		},
	}
	return c.cli.SetBucketCORS(c.cfg.Bucket, rules)
}

// PublicURL 返回 objectKey 的固定访问 URL（适用于 Bucket 公共读场景，永不过期）。
// 格式：https://<bucket>.<endpoint>/<objectKey>
func (c *Client) PublicURL(objectKey string) string {
	endpoint := c.cfg.Endpoint
	// endpoint 可能带协议前缀（如 https://oss-cn-beijing.aliyuncs.com），去掉后拼接
	endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	return "https://" + c.cfg.Bucket + "." + endpoint + "/" + objectKey
}

// ExpireSeconds 返回预签名有效期（秒）。
func (c *Client) ExpireSeconds() time.Duration {
	return time.Duration(c.cfg.PresignExpireSec) * time.Second
}
