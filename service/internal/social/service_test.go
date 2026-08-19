package social

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"im/service/internal/pkg/log"
	"im/service/internal/store/mysql"
)

func TestMain(m *testing.M) {
	log.Init("error", "stdout")
	os.Exit(m.Run())
}

// ---- 内存 mock ----

type mockStore struct {
	mu       sync.Mutex
	users    map[int64]*mysql.User
	friends  map[string]bool
	requests map[int64]*mysql.FriendRequest
	groups   map[int64]*mysql.Group
	members  map[int64]map[int64]bool // gUID -> uid
	roles    map[int64]map[int64]int8 // gUID -> uid -> role
	deletedConvViews map[string]bool  // "uid:gUID" 已删除的群会话视图
	notifs   []*mysql.Notification
	seq      int64
}

func newMockStore() *mockStore {
	return &mockStore{
		users:    map[int64]*mysql.User{},
		friends:  map[string]bool{},
		requests: map[int64]*mysql.FriendRequest{},
		groups:   map[int64]*mysql.Group{},
		members:  map[int64]map[int64]bool{},
		roles:    map[int64]map[int64]int8{},
		deletedConvViews: map[string]bool{},
	}
}

func (m *mockStore) GetUserByUID(uid int64) (*mysql.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[uid]; ok {
		return u, nil
	}
	return nil, mysql.ErrNotFound
}
func (m *mockStore) GetUserByAccount(account string) (*mysql.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Account == account {
			return u, nil
		}
	}
	return nil, mysql.ErrNotFound
}
func (m *mockStore) SearchUsers(keyword string, limit int) ([]*mysql.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*mysql.User
	for _, u := range m.users {
		if containsStr(u.Account, keyword) || containsStr(u.Nickname, keyword) {
			out = append(out, u)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || strings.Contains(s, sub))
}

func (m *mockStore) AddFriend(uid, f int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.friends[fk(uid, f)] = true
	return nil
}
func (m *mockStore) DeleteFriend(uid, f int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.friends, fk(uid, f))
	return nil
}
func (m *mockStore) ListFriends(uid int64) ([]*mysql.Friend, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*mysql.Friend
	for k := range m.friends {
		if k[:len(itoa(uid))+1] == itoa(uid)+":" {
			f, _ := parseFk(k)
			out = append(out, &mysql.Friend{UID: uid, FriendUID: f})
		}
	}
	return out, nil
}
func (m *mockStore) AreFriends(uid, f int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.friends[fk(uid, f)], nil
}
func (m *mockStore) UpdateFriendRemark(uid, f int64, remark string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.friends[fk(uid, f)] {
		return mysql.ErrNotFound
	}
	return nil
}
func (m *mockStore) CreateFriendRequest(r *mysql.FriendRequest) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.requests {
		if x.FromUID == r.FromUID && x.ToUID == r.ToUID && x.Status == 0 {
			return x.ID, nil
		}
	}
	m.seq++
	r.ID = m.seq
	m.requests[r.ID] = r
	return r.ID, nil
}
func (m *mockStore) GetFriendRequest(id int64) (*mysql.FriendRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.requests[id]
	if !ok {
		return nil, mysql.ErrNotFound
	}
	return r, nil
}
func (m *mockStore) ListFriendRequests(to int64) ([]*mysql.FriendRequest, error) { return nil, nil }
func (m *mockStore) UpdateFriendRequestStatus(id int64, s int8) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.requests[id]; ok {
		r.Status = s
	}
	return nil
}
func (m *mockStore) CreateGroup(g *mysql.Group) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.groups[g.GUID] = g
	return nil
}
func (m *mockStore) GetGroupByGUID(gUID int64) (*mysql.Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[gUID]
	if !ok {
		return nil, mysql.ErrNotFound
	}
	return g, nil
}
func (m *mockStore) AddGroupMember(gUID, uid int64, role int8) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.members[gUID] == nil {
		m.members[gUID] = map[int64]bool{}
		m.roles[gUID] = map[int64]int8{}
	}
	m.members[gUID][uid] = true
	m.roles[gUID][uid] = role
	return nil
}
func (m *mockStore) RemoveGroupMember(gUID, uid int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.members[gUID], uid)
	return nil
}
func (m *mockStore) IsGroupMember(gUID, uid int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.members[gUID] != nil && m.members[gUID][uid], nil
}
func (m *mockStore) GetGroupMemberRole(gUID, uid int64) (int8, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.members[gUID] == nil || !m.members[gUID][uid] {
		return -1, nil
	}
	return m.roles[gUID][uid], nil
}
func (m *mockStore) ListGroupMembers(gUID int64) ([]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []int64
	for uid := range m.members[gUID] {
		out = append(out, uid)
	}
	return out, nil
}
func (m *mockStore) ListUserGroups(uid int64) ([]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []int64
	for gUID, set := range m.members {
		if set[uid] {
			out = append(out, gUID)
		}
	}
	return out, nil
}
func (m *mockStore) UpdateGroup(gUID int64, name, a string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if g, ok := m.groups[gUID]; ok {
		g.Name = name
		g.Announcement = a
	}
	return nil
}
func (m *mockStore) GUIDExists(gUID int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.groups[gUID]
	return ok, nil
}
func (m *mockStore) DeleteGroupConversationView(ownerUID, gUID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedConvViews[fk(ownerUID, gUID)] = true
	return nil
}
func (m *mockStore) CreateNotification(n *mysql.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifs = append(m.notifs, n)
	return nil
}
func (m *mockStore) ListNotifications(uid int64) ([]*mysql.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*mysql.Notification
	for _, n := range m.notifs {
		if n.UID == uid {
			out = append(out, n)
		}
	}
	return out, nil
}
func (m *mockStore) MarkNotificationRead(id, uid int64) error { return nil }
func (m *mockStore) MarkAllNotificationsRead(uid int64) error { return nil }
func (m *mockStore) CountUnreadNotifications(uid int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, x := range m.notifs {
		if x.UID == uid && x.Read == 0 {
			n++
		}
	}
	return n, nil
}
func (m *mockStore) ClearNotifications(uid int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*mysql.Notification
	for _, x := range m.notifs {
		if x.UID != uid {
			out = append(out, x)
		}
	}
	m.notifs = out
	return nil
}
func (m *mockStore) GetOrCreateConversation(ownerUID, targetID int64, typ int8, newID int64) (*mysql.Conversation, error) {
	return &mysql.Conversation{ID: newID, Type: typ, OwnerUID: ownerUID, TargetID: targetID}, nil
}

func fk(a, b int64) string { return itoa(a) + ":" + itoa(b) }
func parseFk(k string) (int64, error) {
	parts := strings.Split(k, ":")
	if len(parts) != 2 {
		return 0, strconv.ErrSyntax
	}
	return strconv.ParseInt(parts[1], 10, 64)
}

func newTestSvc() (*Service, *mockStore) {
	store := newMockStore()
	idSeq := &struct{ n int64 }{}
	svc := New(store, func() int64 { idSeq.n++; return idSeq.n }, nil)
	return svc, store
}

func TestFriendRequestFlow(t *testing.T) {
	svc, store := newTestSvc()
	store.users[1] = &mysql.User{UID: 1, Nickname: "A"}
	store.users[2] = &mysql.User{UID: 2, Nickname: "B"}

	if err := svc.SendFriendRequest(1, 2, "hi"); err != nil {
		t.Fatalf("send request: %v", err)
	}
	// 收到通知
	if n := len(store.notifs); n == 0 {
		t.Fatal("expected notification")
	}
	// 接收方处理通过
	reqs := store.requests
	var reqID int64
	for id, r := range reqs {
		if r.FromUID == 1 && r.ToUID == 2 {
			reqID = id
		}
	}
	if err := svc.HandleFriendRequest(2, reqID, true); err != nil {
		t.Fatalf("handle request: %v", err)
	}
	if ok, _ := store.AreFriends(1, 2); !ok {
		t.Fatal("expected friends after accept")
	}
}

// TestSendFriendRequestGuard 验证好友申请守卫：
// 加自己拒绝、目标用户不存在拒绝、已是好友拒绝再发申请。
func TestSendFriendRequestGuard(t *testing.T) {
	svc, store := newTestSvc()
	store.users[1] = &mysql.User{UID: 1, Nickname: "A"}
	store.users[2] = &mysql.User{UID: 2, Nickname: "B"}

	// 1) 不能添加自己
	if err := svc.SendFriendRequest(1, 1, ""); err == nil {
		t.Fatal("添加自己应被拒绝")
	}
	// 2) 目标用户不存在
	if err := svc.SendFriendRequest(1, 999, ""); err == nil {
		t.Fatal("不存在的用户应被拒绝")
	}
	// 3) 已是好友：先正常添加成为好友，再申请应被拦截
	if err := svc.SendFriendRequest(1, 2, "hi"); err != nil {
		t.Fatalf("first request: %v", err)
	}
	var reqID int64
	for id, r := range store.requests {
		if r.FromUID == 1 && r.ToUID == 2 {
			reqID = id
		}
	}
	if err := svc.HandleFriendRequest(2, reqID, true); err != nil {
		t.Fatalf("accept: %v", err)
	}
	err := svc.SendFriendRequest(1, 2, "again")
	if err == nil {
		t.Fatal("已是好友的申请应被拒绝")
	}
	if !strings.Contains(err.Error(), "已是") {
		t.Fatalf("错误信息不符: %v", err)
	}
	// 拦截后不应新增申请记录（仍只有首次那一条）
	if n := len(store.requests); n != 1 {
		t.Fatalf("requests=%d, want 1（已是好友不应新建申请）", n)
	}
}

func TestSearchUser(t *testing.T) {
	svc, store := newTestSvc()
	store.users[1] = &mysql.User{UID: 1, Account: "13800000001", Nickname: "张三"}
	store.users[2] = &mysql.User{UID: 2, Account: "user2@x.com", Nickname: "李四"}

	// 按手机号精确匹配
	u, err := svc.SearchUser("13800000001")
	if err != nil {
		t.Fatalf("search by phone failed: %v", err)
	}
	if u.UID != 1 || u.Nickname != "张三" {
		t.Fatalf("unexpected result: %+v", u)
	}

	// 按邮箱精确匹配
	u2, err := svc.SearchUser("user2@x.com")
	if err != nil {
		t.Fatalf("search by email failed: %v", err)
	}
	if u2.UID != 2 {
		t.Fatalf("unexpected email result: %+v", u2)
	}

	// 未找到
	if _, err := svc.SearchUser("nobody"); err == nil {
		t.Fatal("expected not found")
	}
}

func TestCreateGroup(t *testing.T) {
	svc, store := newTestSvc()
	store.users[1] = &mysql.User{UID: 1, Nickname: "A"}
	store.users[2] = &mysql.User{UID: 2, Nickname: "B"}
	store.users[3] = &mysql.User{UID: 3, Nickname: "C"}

	g, err := svc.CreateGroup(1, "测试群", []int64{2, 3}, "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if g.Name != "测试群" || g.OwnerUID != 1 {
		t.Fatalf("unexpected group: %+v", g)
	}
	members, _ := svc.GetGroupMembers(g.GUID)
	if len(members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(members))
	}
	// 被邀请者收到 invite 通知
	invites := 0
	for _, n := range store.notifs {
		if n.Type == "invite" {
			invites++
		}
	}
	if invites != 2 {
		t.Fatalf("expected 2 invite notifications, got %d", invites)
	}
}

// TestGetGroupMembership 验证群信息查询守卫（审计 P1）：
// 仅群成员可查群信息，非成员拒绝（防枚举群成员列表）。
func TestGetGroupMembership(t *testing.T) {
	svc, store := newTestSvc()
	store.users[1] = &mysql.User{UID: 1, Nickname: "A"}
	store.users[2] = &mysql.User{UID: 2, Nickname: "B"}
	store.users[9] = &mysql.User{UID: 9, Nickname: "外人"}

	g, err := svc.CreateGroup(1, "守卫群", []int64{2}, "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	// 成员（群主/被邀请者）可查
	if _, err := svc.GetGroup(1, g.GUID); err != nil {
		t.Fatalf("群主应可查群信息: %v", err)
	}
	if _, err := svc.GetGroup(2, g.GUID); err != nil {
		t.Fatalf("成员应可查群信息: %v", err)
	}
	// 非成员拒绝
	if _, err := svc.GetGroup(9, g.GUID); err == nil {
		t.Fatal("非成员查群信息应被拒绝")
	}
}

// TestUpdateGroupInfoPermission 验证群设置权限：仅群主/管理员可改群名与公告，
// 普通成员与非成员拒绝（后端鉴权，不依赖前端隐藏入口）。
func TestUpdateGroupInfoPermission(t *testing.T) {
	svc, store := newTestSvc()
	store.users[1] = &mysql.User{UID: 1, Nickname: "群主"}
	store.users[2] = &mysql.User{UID: 2, Nickname: "管理员"}
	store.users[3] = &mysql.User{UID: 3, Nickname: "成员"}
	store.users[9] = &mysql.User{UID: 9, Nickname: "外人"}

	g, err := svc.CreateGroup(1, "设置群", []int64{2, 3}, "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	// 把 2 提升为管理员（role=1）
	store.mu.Lock()
	store.roles[g.GUID][2] = 1
	store.mu.Unlock()

	// 普通成员修改被拒
	if err := svc.UpdateGroupInfo(3, g.GUID, "新群名", "新公告"); err == nil {
		t.Fatal("普通成员不应能修改群设置")
	}
	// 非成员修改被拒
	if err := svc.UpdateGroupInfo(9, g.GUID, "新群名", ""); err == nil {
		t.Fatal("非成员不应能修改群设置")
	}
	// 管理员可修改群名与公告
	if err := svc.UpdateGroupInfo(2, g.GUID, "管理员改名", "管理员公告"); err != nil {
		t.Fatalf("管理员修改应成功: %v", err)
	}
	// 群主可清空公告
	if err := svc.UpdateGroupInfo(1, g.GUID, "群主定稿", ""); err != nil {
		t.Fatalf("群主修改应成功: %v", err)
	}
	// 空群名拒绝
	if err := svc.UpdateGroupInfo(1, g.GUID, "  ", ""); err == nil {
		t.Fatal("空群名应被拒绝")
	}
	got, err := svc.GetGroup(1, g.GUID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if got.Name != "群主定稿" || got.Announcement != "" || got.MyRole != 0 {
		t.Fatalf("unexpected group after update: %+v", got)
	}
}

// TestLeaveGroup 验证退群：成员可退（清理会话视图 + 定向系统消息仅发群主），
// 群主不能退群，非成员拒绝。
func TestLeaveGroup(t *testing.T) {
	svc, store := newTestSvc()
	store.users[1] = &mysql.User{UID: 1, Nickname: "群主"}
	store.users[2] = &mysql.User{UID: 2, Nickname: "退群人"}
	store.users[9] = &mysql.User{UID: 9, Nickname: "外人"}

	var sysMsgTo []int64
	svc.SetGroupSysMsgToSender(func(gUID, convID int64, content, extra string, recipients []int64) {
		sysMsgTo = append(sysMsgTo, recipients...)
	})

	g, err := svc.CreateGroup(1, "退群测试", []int64{2}, "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	// 群主不能退群
	if err := svc.LeaveGroup(1, g.GUID); err == nil {
		t.Fatal("群主退群应被拒绝")
	}
	// 非成员退群拒绝
	if err := svc.LeaveGroup(9, g.GUID); err == nil {
		t.Fatal("非成员退群应被拒绝")
	}
	// 成员退群成功：成员关系移除 + 会话视图清理 + 系统消息仅发群主
	if err := svc.LeaveGroup(2, g.GUID); err != nil {
		t.Fatalf("成员退群应成功: %v", err)
	}
	if ok, _ := store.IsGroupMember(g.GUID, 2); ok {
		t.Fatal("退群后应不再是成员")
	}
	if !store.deletedConvViews[fk(2, g.GUID)] {
		t.Fatal("退群后应删除退群者的群会话视图")
	}
	if len(sysMsgTo) != 1 || sysMsgTo[0] != 1 {
		t.Fatalf("退群系统消息应仅发给群主(1)，实际: %v", sysMsgTo)
	}
}
