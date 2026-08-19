package admin

import (
	"strings"
	"testing"
	"time"
)

// mockLoginCache 内存版限流缓存（模拟 Redis IncrWithTTL/Del）。
type mockLoginCache struct{ counts map[string]int64 }

func (m *mockLoginCache) IncrWithTTL(key string, ttl time.Duration) (int64, error) {
	if m.counts == nil {
		m.counts = map[string]int64{}
	}
	m.counts[key]++
	return m.counts[key], nil
}

func (m *mockLoginCache) Del(key string) error { delete(m.counts, key); return nil }

// TestAdminLoginRateLimit 审计 H2：窗口内尝试超限后即使密码正确也拒绝。
func TestAdminLoginRateLimit(t *testing.T) {
	svc := newTestAdmin()
	svc.SetLoginCache(&mockLoginCache{})
	// adminLoginLimitMax 次尝试内不触发限流（错误密码）
	for i := 0; i < adminLoginLimitMax; i++ {
		if _, err := svc.Login(&LoginReq{Username: "admin", Password: "wrong"}); err != nil && strings.Contains(err.Error(), "频繁") {
			t.Fatalf("第 %d 次不应触发限流: %v", i+1, err)
		}
	}
	// 超限：即使密码正确也拒绝
	if _, err := svc.Login(&LoginReq{Username: "admin", Password: "admin123"}); err == nil || !strings.Contains(err.Error(), "频繁") {
		t.Fatalf("超限后应被限流拒绝, got: %v", err)
	}
}

// TestAdminLoginLimitResetOnSuccess 审计 H2：登录成功清零失败计数。
func TestAdminLoginLimitResetOnSuccess(t *testing.T) {
	svc := newTestAdmin()
	svc.SetLoginCache(&mockLoginCache{})
	for i := 0; i < adminLoginLimitMax-1; i++ {
		_, _ = svc.Login(&LoginReq{Username: "admin", Password: "bad"})
	}
	if _, err := svc.Login(&LoginReq{Username: "admin", Password: "admin123"}); err != nil {
		t.Fatalf("成功登录应清零计数: %v", err)
	}
	// 清零后又可以完整尝试 adminLoginLimitMax-1 次错误而不被限流
	for i := 0; i < adminLoginLimitMax-1; i++ {
		if _, err := svc.Login(&LoginReq{Username: "admin", Password: "bad"}); err != nil && strings.Contains(err.Error(), "频繁") {
			t.Fatalf("清零后第 %d 次错误不应被限流: %v", i+1, err)
		}
	}
}

// TestAdminLoginNoCache 未注入限流缓存时不阻断登录（降级放行）。
func TestAdminLoginNoCache(t *testing.T) {
	svc := newTestAdmin()
	for i := 0; i < adminLoginLimitMax+3; i++ {
		_, _ = svc.Login(&LoginReq{Username: "admin", Password: "bad"})
	}
	if _, err := svc.Login(&LoginReq{Username: "admin", Password: "admin123"}); err != nil {
		t.Fatalf("无缓存时不应限流: %v", err)
	}
}
