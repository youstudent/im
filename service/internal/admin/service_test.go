package admin

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"im/service/internal/config"
	"im/service/internal/pkg/jwt"
	"im/service/internal/pkg/log"
	"im/service/internal/pkg/pwd"
	"im/service/internal/store/mysql"
)

func TestMain(m *testing.M) {
	log.Init("error", "stdout")
	os.Exit(m.Run())
}

type mockAdminStore struct {
	admin    *mysql.AdminUser
	versions []*mysql.AppVersion
	viewsCleaned int64 // 解散群时被清理会话视图的群号（断言用）
}

func (m *mockAdminStore) GetAdminByUsername(u string) (*mysql.AdminUser, error) {
	if m.admin != nil && m.admin.Username == u {
		return m.admin, nil
	}
	return nil, mysql.ErrNotFound
}
func (m *mockAdminStore) CreateAdmin(a *mysql.AdminUser) error { m.admin = a; return nil }
func (m *mockAdminStore) ListAdmins() ([]*mysql.AdminUser, error) { return nil, nil }
func (m *mockAdminStore) CountUsers() (int64, error)              { return 10, nil }
func (m *mockAdminStore) CountGroups() (int64, error)             { return 3, nil }
func (m *mockAdminStore) CountMessages() (int64, error)           { return 100, nil }
func (m *mockAdminStore) CountOnlinePresence() (int64, error)     { return 5, nil }
func (m *mockAdminStore) UserTrendByDay(days int) ([]int64, error)  { return make([]int64, days), nil }
func (m *mockAdminStore) MessageTrendByDay(days int) ([]int64, error) { return make([]int64, days), nil }
func (m *mockAdminStore) ListUsers(offset, limit int, keyword string, status int64) ([]*mysql.User, error) {
	return []*mysql.User{{UID: 1, Account: "a", Nickname: "A", CreatedAt: time.Now()}}, nil
}
func (m *mockAdminStore) CountUsersTotal(keyword string, status int64) (int64, error) { return 10, nil }
func (m *mockAdminStore) ListAllGroups(offset, limit int, keyword string) ([]*mysql.Group, error) {
	return []*mysql.Group{{GUID: 1, Name: "g", MemberCount: 2, CreatedAt: time.Now()}}, nil
}
func (m *mockAdminStore) CountGroupsTotal(keyword string) (int64, error) { return 3, nil }
func (m *mockAdminStore) DisableUser(uid int64) error      { return nil }
func (m *mockAdminStore) EnableUser(uid int64) error       { return nil }
func (m *mockAdminStore) DeleteGroupByGUID(gUID int64) error { return nil }
func (m *mockAdminStore) ListGroupMembers(gUID int64) ([]int64, error) {
	return []int64{1001, 1002}, nil
}
func (m *mockAdminStore) DeleteAllGroupConversationViews(gUID int64) error {
	m.viewsCleaned = gUID
	return nil
}
func (m *mockAdminStore) GetGroupByGUID(gUID int64) (*mysql.Group, error) {
	return &mysql.Group{GUID: gUID, Name: "g", ConvID: 111, CreatedAt: time.Now()}, nil
}
func (m *mockAdminStore) ListMessagesBefore(convID, beforeSeq int64, limit int) ([]*mysql.Message, error) {
	return []*mysql.Message{
		{ID: 1, ConvID: convID, Seq: 1, SenderUID: 1001, Type: 1, Content: "hello", Status: 0, CreatedAt: time.Now()},
	}, nil
}
func (m *mockAdminStore) GetUserByUID(uid int64) (*mysql.User, error) {
	return &mysql.User{UID: uid, Nickname: "user", Account: "acc"}, nil
}
func (m *mockAdminStore) GetUserNames(uids []int64) map[int64]string {
	out := make(map[int64]string, len(uids))
	for _, u := range uids {
		out[u] = "user"
	}
	return out
}

// ---- 版本发布 mock ----
func (m *mockAdminStore) GetAdminByID(id int64) (*mysql.AdminUser, error) {
	if m.admin != nil && m.admin.ID == id {
		return m.admin, nil
	}
	return nil, mysql.ErrNotFound
}
func (m *mockAdminStore) UpdateAdminPassword(id int64, passwordHash string) error {
	if m.admin != nil && m.admin.ID == id {
		m.admin.PasswordHash = passwordHash
		m.admin.MustChangePwd = 0
	}
	return nil
}
func (m *mockAdminStore) CreateAppVersion(v *mysql.AppVersion) error {
	for _, old := range m.versions {
		if old.Version == v.Version {
			return fmt.Errorf("Duplicate entry '%s' for key 'uk_version'", v.Version)
		}
	}
	v.CreatedAt = time.Now()
	m.versions = append(m.versions, v)
	return nil
}
func (m *mockAdminStore) ListAppVersions(offset, limit int) ([]*mysql.AppVersion, error) {
	return m.versions, nil
}
func (m *mockAdminStore) CountAppVersions() (int64, error) { return int64(len(m.versions)), nil }
func (m *mockAdminStore) GetLatestAppVersion() (*mysql.AppVersion, error) {
	if len(m.versions) == 0 {
		return nil, nil
	}
	return m.versions[len(m.versions)-1], nil
}

func newTestAdmin() *Service {
	hash, _ := pwd.Hash("admin123")
	store := &mockAdminStore{admin: &mysql.AdminUser{ID: 1, Username: "admin", PasswordHash: hash, Nickname: "超级管理员", Role: 1, Status: 1}}
	jwtMgr := jwt.New(config.JWT{Secret: "test-secret-0123456789", Issuer: "im-test", AccessExpire: 7200})
	idSeq := &struct{ n int64 }{}
	return New(store, jwtMgr, func() int64 { idSeq.n++; return idSeq.n })
}

func TestAdminLogin(t *testing.T) {
	svc := newTestAdmin()
	res, err := svc.Login(&LoginReq{Username: "admin", Password: "admin123"})
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if res.AccessToken == "" || res.Admin.Username != "admin" {
		t.Fatalf("unexpected result: %+v", res)
	}
	// 密码错误
	if _, err := svc.Login(&LoginReq{Username: "admin", Password: "wrong"}); err == nil {
		t.Fatal("expected login failure on wrong password")
	}
}

func TestAdminMustChangePwd(t *testing.T) {
	svc := newTestAdmin()
	// 标记种子账号：登录返回 must_change_pwd=true
	svc.store.(*mockAdminStore).admin.MustChangePwd = 1
	res, err := svc.Login(&LoginReq{Username: "admin", Password: "admin123"})
	if err != nil || !res.MustChangePwd {
		t.Fatalf("expected must_change_pwd=true, got %+v err=%v", res, err)
	}
	// 旧密码错误拒绝
	if err := svc.ChangePassword(1, &ChangePwdReq{OldPassword: "wrong", NewPassword: "newpass123"}); err == nil {
		t.Fatal("expected wrong old password error")
	}
	// 弱新密码拒绝（不含数字/长度不足）
	if err := svc.ChangePassword(1, &ChangePwdReq{OldPassword: "admin123", NewPassword: "abcdefgh"}); err == nil {
		t.Fatal("expected weak password error")
	}
	// 新旧相同拒绝
	if err := svc.ChangePassword(1, &ChangePwdReq{OldPassword: "admin123", NewPassword: "admin123"}); err == nil {
		t.Fatal("expected same password error")
	}
	// 修改成功：新密码可登录，且标记已清零
	if err := svc.ChangePassword(1, &ChangePwdReq{OldPassword: "admin123", NewPassword: "newpass123"}); err != nil {
		t.Fatalf("change password failed: %v", err)
	}
	res, err = svc.Login(&LoginReq{Username: "admin", Password: "newpass123"})
	if err != nil || res.MustChangePwd {
		t.Fatalf("expected login with new password and flag cleared, got %+v err=%v", res, err)
	}
}

func TestDeleteGroupCleanup(t *testing.T) {
	svc := newTestAdmin()
	store := svc.store.(*mockAdminStore)
	var notified []int64
	svc.SetDismissNotifier(func(uid, gUID, convID int64) {
		if gUID != 77 || convID != 111 {
			t.Fatalf("unexpected dismiss args: uid=%d g_uid=%d conv=%d", uid, gUID, convID)
		}
		notified = append(notified, uid)
	})
	// 解散群：应清理成员会话视图并通知全部成员
	if err := svc.DeleteGroup(1, 77); err != nil {
		t.Fatalf("delete group failed: %v", err)
	}
	if store.viewsCleaned != 77 {
		t.Fatalf("conversation views not cleaned, got g_uid=%d", store.viewsCleaned)
	}
	if len(notified) != 2 {
		t.Fatalf("expected 2 members notified, got %d", len(notified))
	}
}

func TestDashboard(t *testing.T) {
	svc := newTestAdmin()
	d, err := svc.GetDashboard()
	if err != nil {
		t.Fatalf("dashboard failed: %v", err)
	}
	if d.Users != 10 || d.Groups != 3 || d.Messages != 100 {
		t.Fatalf("unexpected dashboard: %+v", d)
	}
}

func TestPublishAndLatestVersion(t *testing.T) {
	svc := newTestAdmin()
	testSha := strings.Repeat("ab", 32) // 64 位十六进制合法摘要
	// 发布成功，发布者记录为管理员用户名
	if err := svc.PublishVersion(1, &PublishVersionReq{Version: "1.1.0", DownloadURL: "https://example.com/wc.exe", Sha256: testSha, ReleaseNotes: "新增本地存储"}); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	// 重复版本号拒绝
	if err := svc.PublishVersion(1, &PublishVersionReq{Version: "1.1.0", DownloadURL: "https://x.com/a", Sha256: testSha}); err == nil {
		t.Fatal("expected duplicate version error")
	}
	// 非法版本号拒绝
	if err := svc.PublishVersion(1, &PublishVersionReq{Version: "abc", DownloadURL: "https://x.com/a", Sha256: testSha}); err == nil {
		t.Fatal("expected invalid version format error")
	}
	// 缺少/非法 SHA-256 拒绝（供应链安全：防无校验的自动更新）
	if err := svc.PublishVersion(1, &PublishVersionReq{Version: "1.2.0", DownloadURL: "https://x.com/a"}); err == nil {
		t.Fatal("expected sha256 required error")
	}
	if err := svc.PublishVersion(1, &PublishVersionReq{Version: "1.2.0", DownloadURL: "https://x.com/a", Sha256: "zz"}); err == nil {
		t.Fatal("expected invalid sha256 error")
	}
	// 最新版本
	v, err := svc.LatestVersion()
	if err != nil || v == nil {
		t.Fatalf("latest failed: v=%+v err=%v", v, err)
	}
	if v.Version != "1.1.0" || v.Publisher != "admin" || v.DownloadURL != "https://example.com/wc.exe" {
		t.Fatalf("unexpected latest: %+v", v)
	}
	// 列表
	list, total, err := svc.ListVersions(0, 10)
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("list failed: total=%d len=%d err=%v", total, len(list), err)
	}
}
