// Package redis 封装 Redis 客户端，承载在线状态/未读计数/路由/离线队列。
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"im/service/internal/config"
)

// Client 封装 go-redis 客户端。
type Client struct {
	*redis.Client
}

// New 根据配置创建 Redis 客户端并 Ping 验证连通性。
func New(cfg config.Redis) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		DialTimeout:  time.Duration(cfg.DialTimeout) * time.Second,
		ReadTimeout:  time.Duration(cfg.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.WriteTimeout) * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Client{Client: rdb}, nil
}

// Set 写入键值并设置过期时间。
func (c *Client) Set(key string, value interface{}, expiration time.Duration) error {
	return c.Client.Set(context.Background(), key, value, expiration).Err()
}

// Get 读取键值，不存在时返回空字符串。
func (c *Client) Get(key string) (string, error) {
	return c.Client.Get(context.Background(), key).Result()
}

// Del 删除一个或多个键。
func (c *Client) Del(key string) error {
	return c.Client.Del(context.Background(), key).Err()
}

// IncrWithTTL 原子自增计数（首次写入时设置 TTL），用于登录/注册限流。
func (c *Client) IncrWithTTL(key string, ttl time.Duration) (int64, error) {
	ctx := context.Background()
	n, err := c.Client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		_ = c.Client.Expire(ctx, key, ttl).Err()
	}
	return n, nil
}

// ---------- 离线消息队列 ----------

// offlineKey 离线消息队列键。
func offlineKey(uid int64) string { return fmt.Sprintf("offline:%d", uid) }

// PushOffline 将消息帧存入指定用户的离线队列（头插）。
func (c *Client) PushOffline(uid int64, payload []byte) error {
	return c.Client.LPush(context.Background(), offlineKey(uid), payload).Err()
}

// PopOffline 从队列尾部取出最多 limit 条离线消息（先进先出），取出后即删除。
func (c *Client) PopOffline(uid int64, limit int64) ([][]byte, error) {
	if limit <= 0 {
		return nil, nil
	}
	// 用事务批量 RPOP，保证原子性
	cmds := make([]*redis.StringCmd, 0, limit)
	pipe := c.Client.TxPipeline()
	for i := int64(0); i < limit; i++ {
		cmds = append(cmds, pipe.RPop(context.Background(), offlineKey(uid)))
	}
	_, err := pipe.Exec(context.Background())
	if err != nil && err != redis.Nil {
		return nil, err
	}
	var out [][]byte
	for _, cmd := range cmds {
		v, e := cmd.Result()
		if e == redis.Nil || v == "" {
			break // 队列已空
		}
		out = append(out, []byte(v))
	}
	return out, nil
}

// CountOffline 返回用户离线消息数量。
func (c *Client) CountOffline(uid int64) int64 {
	n, err := c.Client.LLen(context.Background(), offlineKey(uid)).Result()
	if err != nil {
		return 0
	}
	return n
}

// ClearOffline 清空用户离线队列。
func (c *Client) ClearOffline(uid int64) error {
	return c.Del(offlineKey(uid))
}
