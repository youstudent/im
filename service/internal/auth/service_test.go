package auth

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"im/service/internal/config"
	"im/service/internal/pkg/jwt"
	"im/service/internal/pkg/log"
	"im/service/internal/store/mysql"
)

func TestMain(m *testing.M) {
	log.Init("error", "stdout")
	os.Exit(m.Run())
}

// ---- 内存 mock：Store 与 Cache ----

type mockStore struct {
	mu    sync.Mutex
	users map[string]*mysql.User // by account
	byUID map[int64]*mysql.User
	seq   int64
}

func newMockStore() *mockStore {
	return &mockStore{users: map[string]*mysql.User{}, byUID: map[int64]*mysql.User{}}
}

func (m *mockStore) CreateUser(u *mysql.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	if u.ID == 0 {
		u.ID = m.seq
	}
	m.users[u.Account] = u
	m.byUID[u.UID] = u
	return nil
}

func (m *mockStore) GetUserByAccount(account string) (*mysql.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[account]
	if !ok {
		return nil, mysql.ErrNotFound
	}
	return u, nil
}

func (m *mockStore) GetUserByUID(uid int64) (*mysql.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byUID[uid]
	if !ok {
		return nil, mysql.ErrNotFound
	}
	return u, nil
}

func (m *mockStore) TouchLastSeen(uid int64, status int8) error { return nil }
func (m *mockStore) UIDExists(uid int64) (bool, error)          { return false, nil }

type mockCache struct {
	mu sync.Mutex
	m  map[string]string
}

func newMockCache() *mockCache { return &mockCache{m: map[string]string{}} }

func (c *mockCache) Set(key string, value interface{}, expiration time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = toString(value)
	return nil
}

func (c *mockCache) Get(key string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

func (c *mockCache) Del(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
	return nil
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func (c *mockCache) IncrWithTTL(key string, ttl time.Duration) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, _ := strconv.ParseInt(c.m[key], 10, 64)
	n++
	c.m[key] = strconv.FormatInt(n, 10)
	return n, nil
}

func newTestService() *Service {
	store := newMockStore()
	cache := newMockCache()
	jwtMgr := jwt.New(config.JWT{
		Secret:        "test-secret-0123456789",
		Issuer:        "im-test",
		AccessExpire:  7200,
		RefreshExpire: 86400,
	})
	idSeq := &struct{ n int64 }{}
	return New(store, cache, jwtMgr, func() int64 { idSeq.n++; return idSeq.n })
}

func TestRegisterAndLogin(t *testing.T) {
	svc := newTestService()

	res, err := svc.Register(&RegisterReq{Nickname: "张三", Account: "user1@example.com", Password: "pass1234"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if res.AccessToken == "" || res.RefreshToken == "" {
		t.Fatal("expected tokens after register")
	}
	if res.User.Nickname != "张三" {
		t.Fatalf("unexpected nickname: %s", res.User.Nickname)
	}

	// 重复注册应冲突
	if _, err := svc.Register(&RegisterReq{Nickname: "李四", Account: "user1@example.com", Password: "pass1234"}, "127.0.0.1"); err == nil {
		t.Fatal("expected conflict on duplicate register")
	}

	// 登录
	login, err := svc.Login(&LoginReq{Account: "user1@example.com", Password: "pass1234"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if login.User.UID != res.User.UID {
		t.Fatalf("uid mismatch: %d != %d", login.User.UID, res.User.UID)
	}

	// 密码错误
	if _, err := svc.Login(&LoginReq{Account: "user1@example.com", Password: "wrongpass"}, "127.0.0.1"); err == nil {
		t.Fatal("expected login failure on wrong password")
	}
}

func TestRefreshAndLogout(t *testing.T) {
	svc := newTestService()
	res, err := svc.Register(&RegisterReq{Nickname: "王五", Account: "13000000000", Password: "abc12345"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// refresh 成功签发新 token
	refreshed, err := svc.Refresh(&RefreshReq{RefreshToken: res.RefreshToken})
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if refreshed.AccessToken == "" {
		t.Fatal("expected new access token")
	}

	// 退出后 refresh 应失效
	if err := svc.Logout(&LogoutReq{RefreshToken: res.RefreshToken}); err != nil {
		t.Fatalf("logout failed: %v", err)
	}
	if _, err := svc.Refresh(&RefreshReq{RefreshToken: res.RefreshToken}); err == nil {
		t.Fatal("expected refresh to fail after logout")
	}
}

func TestQRCodeFlow(t *testing.T) {
	svc := newTestService()
	res, err := svc.Register(&RegisterReq{Nickname: "赵六", Account: "13011112222", Password: "xyz12345"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	qr, err := svc.CreateQR()
	if err != nil {
		t.Fatalf("create qr failed: %v", err)
	}
	if qr.QRCodeID == "" || qr.Payload == "" {
		t.Fatal("expected qrcode_id and payload")
	}

	// 初始 pending
	poll, err := svc.PollQR(&PollQRReq{QRCodeID: qr.QRCodeID})
	if err != nil {
		t.Fatalf("poll failed: %v", err)
	}
	if poll.Status != QRStatusPending {
		t.Fatalf("expected pending, got %s", poll.Status)
	}

	// 确认（uid 由 JWT 鉴权后注入，不来自请求体）
	if err := svc.ConfirmQR(res.User.UID, &ConfirmQRReq{QRCodeID: qr.QRCodeID}); err != nil {
		t.Fatalf("confirm failed: %v", err)
	}

	// 轮询到 confirmed 并签发 token
	poll, err = svc.PollQR(&PollQRReq{QRCodeID: qr.QRCodeID})
	if err != nil {
		t.Fatalf("poll confirmed failed: %v", err)
	}
	if poll.Status != QRStatusConfirmed || poll.Login == nil {
		t.Fatalf("expected confirmed with login, got status=%s", poll.Status)
	}
	if poll.Login.User.UID != res.User.UID {
		t.Fatalf("uid mismatch in qr login: %d != %d", poll.Login.User.UID, res.User.UID)
	}

	// 已消费，再次轮询返回 expired
	poll, err = svc.PollQR(&PollQRReq{QRCodeID: qr.QRCodeID})
	if err != nil {
		t.Fatalf("poll consumed failed: %v", err)
	}
	if poll.Status != QRStatusExpired {
		t.Fatalf("expected expired after consume, got %s", poll.Status)
	}
}

// TestRefreshRotationBlacklistsOldToken 验证令牌轮换（审计 P1）：
// refresh 后旧 refresh 立即失效，防旧令牌泄露后重放。
func TestRefreshRotationBlacklistsOldToken(t *testing.T) {
	svc := newTestService()
	res, err := svc.Register(&RegisterReq{Nickname: "轮换", Account: "rot@example.com", Password: "rot12345"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if _, err := svc.Refresh(&RefreshReq{RefreshToken: res.RefreshToken}); err != nil {
		t.Fatalf("first refresh failed: %v", err)
	}
	// 旧 refresh 已被轮换拉黑，重放应失败
	if _, err := svc.Refresh(&RefreshReq{RefreshToken: res.RefreshToken}); err == nil {
		t.Fatal("旧 refresh 轮换后重放应被拒绝")
	}
}

// TestLoginRateLimit 验证登录限流（审计 P1）：窗口内尝试超限后拒绝，成功登录清零。
func TestLoginRateLimit(t *testing.T) {
	svc := newTestService()
	if _, err := svc.Register(&RegisterReq{Nickname: "限流", Account: "rl@example.com", Password: "rl123456"}, "127.0.0.1"); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	// loginLimitMax 次尝试内不触发限流（含错误密码）
	for i := 0; i < loginLimitMax; i++ {
		_, err := svc.Login(&LoginReq{Account: "rl@example.com", Password: "wrong" + strconv.Itoa(i)}, "127.0.0.1")
		if err != nil && strings.Contains(err.Error(), "频繁") {
			t.Fatalf("第 %d 次不应触发限流: %v", i+1, err)
		}
	}
	// 超限：即使密码正确也拒绝
	_, err := svc.Login(&LoginReq{Account: "rl@example.com", Password: "rl123456"}, "127.0.0.1")
	if err == nil || !strings.Contains(err.Error(), "频繁") {
		t.Fatalf("超限后应被限流拒绝, got: %v", err)
	}
}

// TestLoginLimitResetOnSuccess 验证成功登录后失败计数清零。
func TestLoginLimitResetOnSuccess(t *testing.T) {
	svc := newTestService()
	if _, err := svc.Register(&RegisterReq{Nickname: "清零", Account: "clr@example.com", Password: "clr12345"}, "127.0.0.1"); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	// 若干次错误尝试
	for i := 0; i < loginLimitMax-1; i++ {
		_, _ = svc.Login(&LoginReq{Account: "clr@example.com", Password: "bad"}, "127.0.0.1")
	}
	// 成功登录清零
	if _, err := svc.Login(&LoginReq{Account: "clr@example.com", Password: "clr12345"}, "127.0.0.1"); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	// 清零后又可以完整尝试 loginLimitMax-1 次错误而不被限流
	for i := 0; i < loginLimitMax-1; i++ {
		if _, err := svc.Login(&LoginReq{Account: "clr@example.com", Password: "bad"}, "127.0.0.1"); err != nil && strings.Contains(err.Error(), "频繁") {
			t.Fatalf("清零后第 %d 次错误不应被限流: %v", i+1, err)
		}
	}
}

// TestRegisterRateLimit 验证注册频控（审计 P1）：同一 IP 每日超限后拒绝。
func TestRegisterRateLimit(t *testing.T) {
	svc := newTestService()
	for i := 0; i < registerLimitMax; i++ {
		acc := "bulk" + strconv.Itoa(i) + "@example.com"
		if _, err := svc.Register(&RegisterReq{Nickname: "批量", Account: acc, Password: "bulk12345"}, "10.0.0.9"); err != nil {
			t.Fatalf("第 %d 次注册不应被拒: %v", i+1, err)
		}
	}
	// 超限：同 IP 再注册被拒
	_, err := svc.Register(&RegisterReq{Nickname: "批量", Account: "bulkx@example.com", Password: "bulk12345"}, "10.0.0.9")
	if err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("超限注册应被拒绝, got: %v", err)
	}
	// 不同 IP 不受影响
	if _, err := svc.Register(&RegisterReq{Nickname: "批量", Account: "bulkx@example.com", Password: "bulk12345"}, "10.0.0.10"); err != nil {
		t.Fatalf("不同 IP 注册不应被拒: %v", err)
	}
}
