package social

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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
	nicknames map[int64]map[int64]string // gUID -> uid -> 群内昵称（懒初始化）
	mutes    map[int64]map[int64]int64 // gUID -> uid -> 禁言截止（P2 G8）
	saved    map[int64]map[int64]int8  // gUID -> uid -> 保存到通讯录（P2 G10）
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
		mutes:    map[int64]map[int64]int64{},
		saved:    map[int64]map[int64]int8{},
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

// ---- 批量查询（P1 优化接口的 mock 实现）----
func (m *mockStore) GetUsersByUIDs(uids []int64) map[int64]*mysql.User {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[int64]*mysql.User, len(uids))
	for _, uid := range uids {
		if u, ok := m.users[uid]; ok {
			out[uid] = u
		}
	}
	return out
}
func (m *mockStore) GetGroupsByGUIDs(gUIDs []int64) map[int64]*mysql.Group {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[int64]*mysql.Group, len(gUIDs))
	for _, g := range gUIDs {
		if grp, ok := m.groups[g]; ok {
			out[g] = grp
		}
	}
	return out
}
func (m *mockStore) GroupMemberCounts(gUIDs []int64) map[int64]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[int64]int, len(gUIDs))
	for _, g := range gUIDs {
		out[g] = len(m.members[g])
	}
	return out
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
func (m *mockStore) HasPendingFriendRequest(from, to int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.requests {
		if r.FromUID == from && r.ToUID == to && r.Status == 0 {
			return true, nil
		}
	}
	return false, nil
}
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
func (m *mockStore) UpdateGroupMemberRole(gUID, uid int64, role int8) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.members[gUID] == nil || !m.members[gUID][uid] {
		return nil
	}
	if m.roles[gUID][uid] == 0 {
		return nil // 群主不可变更（镜像 SQL 的 role != 0 条件）
	}
	if m.roles[gUID] == nil {
		m.roles[gUID] = map[int64]int8{}
	}
	m.roles[gUID][uid] = role
	return nil
}
func (m *mockStore) ListGroupMemberRoles(gUID int64) (map[int64]int8, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[int64]int8)
	for uid := range m.members[gUID] {
		out[uid] = m.roles[gUID][uid]
	}
	return out, nil
}
func (m *mockStore) TransferGroupOwner(gUID, oldOwnerUID, newOwnerUID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if g, ok := m.groups[gUID]; ok {
		g.OwnerUID = newOwnerUID
	}
	if m.roles[gUID] == nil {
		m.roles[gUID] = map[int64]int8{}
	}
	m.roles[gUID][newOwnerUID] = 0
	if m.roles[gUID][oldOwnerUID] == 0 {
		m.roles[gUID][oldOwnerUID] = 2
	}
	return nil
}
func (m *mockStore) UpdateGroupMemberNickname(gUID, uid int64, nickname string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.nicknames == nil {
		m.nicknames = map[int64]map[int64]string{}
	}
	if m.nicknames[gUID] == nil {
		m.nicknames[gUID] = map[int64]string{}
	}
	if nickname == "" {
		delete(m.nicknames[gUID], uid)
	} else {
		m.nicknames[gUID][uid] = nickname
	}
	return nil
}
func (m *mockStore) ListGroupMemberNicknames(gUID int64) (map[int64]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[int64]string)
	for uid, nick := range m.nicknames[gUID] {
		out[uid] = nick
	}
	return out, nil
}
// P2 G8/G10/G7 群设置相关 mock
func (m *mockStore) UpdateGroupSettings(gUID int64, inviteConfirm, muteAll *int8) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.groups[gUID]
	if !ok {
		return mysql.ErrNotFound
	}
	if inviteConfirm != nil {
		g.InviteConfirm = *inviteConfirm
	}
	if muteAll != nil {
		g.MuteAll = *muteAll
	}
	return nil
}
func (m *mockStore) GetGroupMuteState(gUID int64) (int8, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if g, ok := m.groups[gUID]; ok {
		return g.MuteAll, nil
	}
	return 0, nil
}
func (m *mockStore) UpdateMemberMutedUntil(gUID, uid int64, until int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mutes[gUID] == nil {
		m.mutes[gUID] = map[int64]int64{}
	}
	m.mutes[gUID][uid] = until
	return nil
}
func (m *mockStore) ListGroupMemberMutes(gUID int64) (map[int64]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[int64]int64, len(m.mutes[gUID]))
	for uid, until := range m.mutes[gUID] {
		out[uid] = until
	}
	return out, nil
}
func (m *mockStore) UpdateGroupMemberSaved(gUID, uid int64, saved int8) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saved[gUID] == nil {
		m.saved[gUID] = map[int64]int8{}
	}
	m.saved[gUID][uid] = saved
	return nil
}
func (m *mockStore) ListGroupMemberSaved(uid int64, gUIDs []int64) (map[int64]int8, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[int64]int8, len(gUIDs))
	for _, g := range gUIDs {
		if v, ok := m.saved[g][uid]; ok {
			out[g] = v
		}
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

// TestSendFriendRequestMessageLimit 验证申请验证消息长度上限（防超大字符串冲刷申请/通知表）。
func TestSendFriendRequestMessageLimit(t *testing.T) {
	svc, store := newTestSvc()
	store.users[1] = &mysql.User{UID: 1, Nickname: "A"}
	store.users[2] = &mysql.User{UID: 2, Nickname: "B"}

	// 超限拒绝（含首尾空白按去空白后长度计）
	long := strings.Repeat("长", friendReqMsgMaxRunes+1)
	if err := svc.SendFriendRequest(1, 2, "  "+long+"  "); err == nil || !strings.Contains(err.Error(), "过长") {
		t.Fatalf("超长验证消息应被拒绝, got: %v", err)
	}
	// 恰好等于上限应放行
	if err := svc.SendFriendRequest(1, 2, strings.Repeat("长", friendReqMsgMaxRunes)); err != nil {
		t.Fatalf("等于上限的验证消息应允许: %v", err)
	}
	// 超限被拒后不应产生申请记录（仅等于上限那一条）
	if n := len(store.requests); n != 1 {
		t.Fatalf("requests=%d, want 1（超长拦截不应落库）", n)
	}
}

func TestSearchUser(t *testing.T) {
	svc, store := newTestSvc()
	store.users[1] = &mysql.User{UID: 1, Account: "13800000001", Nickname: "张三"}
	store.users[2] = &mysql.User{UID: 2, Account: "user2@x.com", Nickname: "李四"}
	store.users[3] = &mysql.User{UID: 3, Account: "13800000003", Nickname: "王五"}

	// 按手机号精确匹配（调用者 3 与对方无关系）
	u, err := svc.SearchUser(3, "13800000001")
	if err != nil {
		t.Fatalf("search by phone failed: %v", err)
	}
	if u.UID != 1 || u.Nickname != "张三" {
		t.Fatalf("unexpected result: %+v", u)
	}
	if u.IsFriend || u.RequestSent {
		t.Fatalf("unexpected relation status: %+v", u)
	}

	// 按邮箱精确匹配
	u2, err := svc.SearchUser(3, "user2@x.com")
	if err != nil {
		t.Fatalf("search by email failed: %v", err)
	}
	if u2.UID != 2 {
		t.Fatalf("unexpected email result: %+v", u2)
	}

	// 已是好友：is_friend 置位
	_ = store.AddFriend(3, 2)
	u2f, err := svc.SearchUser(3, "user2@x.com")
	if err != nil || !u2f.IsFriend {
		t.Fatalf("expected is_friend=true, got %+v, err=%v", u2f, err)
	}

	// 已发送待处理申请：request_sent 置位
	if err := svc.SendFriendRequest(3, 1, "hi"); err != nil {
		t.Fatalf("send request failed: %v", err)
	}
	u1p, err := svc.SearchUser(3, "13800000001")
	if err != nil || !u1p.RequestSent {
		t.Fatalf("expected request_sent=true, got %+v, err=%v", u1p, err)
	}

	// 未找到
	if _, err := svc.SearchUser(3, "nobody"); err == nil {
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

// TestRemoveMember 验证移除成员：群主可移除任何人，管理员仅可移除普通成员，
// 不能移除自己，移除后会话视图清理 + 剩余成员收到系统消息。
func TestRemoveMember(t *testing.T) {
	svc, store := newTestSvc()
	store.users[1] = &mysql.User{UID: 1, Nickname: "群主"}
	store.users[2] = &mysql.User{UID: 2, Nickname: "管理员"}
	store.users[3] = &mysql.User{UID: 3, Nickname: "成员甲"}
	store.users[4] = &mysql.User{UID: 4, Nickname: "成员乙"}

	var sysMsgTo []int64
	svc.SetGroupSysMsgToSender(func(gUID, convID int64, content, extra string, recipients []int64) {
		sysMsgTo = append(sysMsgTo, recipients...)
	})

	g, err := svc.CreateGroup(1, "移除测试", []int64{2, 3, 4}, "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	store.mu.Lock()
	store.roles[g.GUID][2] = 1 // 2 提升为管理员
	store.mu.Unlock()

	// 不能移除自己
	if err := svc.RemoveMember(1, g.GUID, 1); err == nil {
		t.Fatal("移除自己应被拒绝")
	}
	// 管理员不能移除群主 / 其他管理员
	if err := svc.RemoveMember(2, g.GUID, 1); err == nil {
		t.Fatal("管理员移除群主应被拒绝")
	}
	// 普通成员无权移除
	if err := svc.RemoveMember(3, g.GUID, 4); err == nil {
		t.Fatal("普通成员移除他人应被拒绝")
	}
	// 管理员可移除普通成员
	if err := svc.RemoveMember(2, g.GUID, 4); err != nil {
		t.Fatalf("管理员移除普通成员应成功: %v", err)
	}
	if ok, _ := store.IsGroupMember(g.GUID, 4); ok {
		t.Fatal("被移除后应不再是成员")
	}
	if !store.deletedConvViews[fk(4, g.GUID)] {
		t.Fatal("被移除后应删除其群会话视图")
	}
	// 系统消息应发给剩余成员（1/2/3，不含被移除的 4）
	gotSet := map[int64]bool{}
	for _, u := range sysMsgTo {
		gotSet[u] = true
	}
	if len(sysMsgTo) != 3 || gotSet[4] || !gotSet[1] || !gotSet[2] || !gotSet[3] {
		t.Fatalf("移出系统消息应发给剩余成员，实际: %v", sysMsgTo)
	}
	// 群主可移除管理员
	if err := svc.RemoveMember(1, g.GUID, 2); err != nil {
		t.Fatalf("群主移除管理员应成功: %v", err)
	}
}

// TestSetMemberRole 验证设/撤管理员：仅群主可操作，不能改自己/群主角色，
// 非法 role 拒绝；成功后 GetGroup 的 member_roles 同步更新。
func TestSetMemberRole(t *testing.T) {
	s, ms := newTestSvc()
	ms.users[1] = &mysql.User{UID: 1, Nickname: "群主"}
	ms.users[2] = &mysql.User{UID: 2, Nickname: "成员"}
	ms.users[3] = &mysql.User{UID: 3, Nickname: "路人"}

	g, err := s.CreateGroup(1, "角色测试", []int64{2}, "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	// 非群主无权
	if err := s.SetMemberRole(2, g.GUID, 2, 1); err == nil {
		t.Fatal("非群主设管理员应被拒绝")
	}
	// 不能改自己 / 非法 role
	if err := s.SetMemberRole(1, g.GUID, 1, 1); err == nil {
		t.Fatal("修改自己角色应被拒绝")
	}
	if err := s.SetMemberRole(1, g.GUID, 2, 3); err == nil {
		t.Fatal("非法 role 应被拒绝")
	}
	// 非成员拒绝
	if err := s.SetMemberRole(1, g.GUID, 3, 1); err == nil {
		t.Fatal("非成员设角色应被拒绝")
	}
	// 群主设为管理员
	if err := s.SetMemberRole(1, g.GUID, 2, 1); err != nil {
		t.Fatalf("设管理员应成功: %v", err)
	}
	got, err := s.GetGroup(1, g.GUID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if got.MemberRoles[2] != 1 {
		t.Fatalf("member_roles 应反映管理员角色，实际: %v", got.MemberRoles)
	}
	// 撤销管理员
	if err := s.SetMemberRole(1, g.GUID, 2, 2); err != nil {
		t.Fatalf("撤管理员应成功: %v", err)
	}
}

// TestTransferOwnership 验证转让群主：仅现任群主可转让，目标必须是成员，
// 转让后新群主 role=0、原群主自动变普通成员。
func TestTransferOwnership(t *testing.T) {
	svc, store := newTestSvc()
	store.users[1] = &mysql.User{UID: 1, Nickname: "老群主"}
	store.users[2] = &mysql.User{UID: 2, Nickname: "新群主"}
	store.users[9] = &mysql.User{UID: 9, Nickname: "外人"}

	g, err := svc.CreateGroup(1, "转让测试", []int64{2}, "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	// 非群主转让被拒
	if err := svc.TransferOwnership(2, g.GUID, 1); err == nil {
		t.Fatal("非群主转让应被拒绝")
	}
	// 转让给自己 / 非成员被拒
	if err := svc.TransferOwnership(1, g.GUID, 1); err == nil {
		t.Fatal("转让给自己应被拒绝")
	}
	if err := svc.TransferOwnership(1, g.GUID, 9); err == nil {
		t.Fatal("转让给非成员应被拒绝")
	}
	// 群主转让成功：角色互换
	if err := svc.TransferOwnership(1, g.GUID, 2); err != nil {
		t.Fatalf("转让群主应成功: %v", err)
	}
	if r, _ := store.GetGroupMemberRole(g.GUID, 2); r != 0 {
		t.Fatalf("新群主 role 应为 0，实际: %d", r)
	}
	if r, _ := store.GetGroupMemberRole(g.GUID, 1); r != 2 {
		t.Fatalf("原群主应自动变普通成员(2)，实际: %d", r)
	}
	// 原群主不再可转让
	if err := svc.TransferOwnership(1, g.GUID, 2); err == nil {
		t.Fatal("原群主转让后不应再拥有转让权")
	}
}

// TestNotifyMentioned 验证 @ 提及通知落库：仅为有效被 @ 成员写 mention 通知，
// 发送者自身不通知；摘要含群名与发送者，action 携带 conv_id/msg_id。
func TestNotifyMentioned(t *testing.T) {
	s, ms := newTestSvc()
	ms.users[1] = &mysql.User{UID: 1, Nickname: "群主"}
	ms.users[2] = &mysql.User{UID: 2, Nickname: "成员"}
	ms.users[3] = &mysql.User{UID: 3, Nickname: "路人"}

	g, err := s.CreateGroup(1, "提及测试", []int64{2, 3}, "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	// 群主 @ 成员 2 与自身（自身应被跳过）
	s.NotifyMentioned(g.GUID, 888, 999, 1, "群主", []int64{2, 1}, "hello")
	var mentions []*mysql.Notification
	for _, n := range ms.notifs {
		if n.Type == "mention" {
			mentions = append(mentions, n)
		}
	}
	if len(mentions) != 1 {
		t.Fatalf("应生成 1 条 mention 通知（发送者自身不通知），实际: %d", len(mentions))
	}
	n := mentions[0]
	if n.UID != 2 || n.Title != "有人@了你" {
		t.Fatalf("通知接收者/标题不符: uid=%d title=%s", n.UID, n.Title)
	}
	if !strings.Contains(n.Summary, "提及测试") || !strings.Contains(n.Summary, "群主") {
		t.Fatalf("摘要应含群名与发送者: %s", n.Summary)
	}
	if !strings.Contains(n.Action, `"conv_id":"888"`) || !strings.Contains(n.Action, `"msg_id":"999"`) {
		t.Fatalf("action 应携带 conv_id/msg_id: %s", n.Action)
	}
}

// TestSetMyNickname 验证群内昵称：成员可设置/清除自己的群昵称，
// GetGroup 返回 member_nicknames 与 my_nickname；非成员拒绝。
func TestSetMyNickname(t *testing.T) {
	svc, store := newTestSvc()
	store.users[1] = &mysql.User{UID: 1, Nickname: "群主"}
	store.users[2] = &mysql.User{UID: 2, Nickname: "成员"}
	store.users[9] = &mysql.User{UID: 9, Nickname: "外人"}

	g, err := svc.CreateGroup(1, "昵称测试", []int64{2}, "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	// 非成员设置被拒
	if err := svc.SetMyNickname(9, g.GUID, "外部人"); err == nil {
		t.Fatal("非成员设群昵称应被拒绝")
	}
	// 设置成功：GetGroup 可查到
	if err := svc.SetMyNickname(2, g.GUID, "群里的名字"); err != nil {
		t.Fatalf("设群昵称应成功: %v", err)
	}
	got, err := svc.GetGroup(2, g.GUID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if got.MyNickname != "群里的名字" || got.MemberNicknames[2] != "群里的名字" {
		t.Fatalf("群昵称应回显，实际 my=%q map=%v", got.MyNickname, got.MemberNicknames)
	}
	// 清除（空值）回落用户昵称
	if err := svc.SetMyNickname(2, g.GUID, "  "); err != nil {
		t.Fatalf("清除群昵称应成功: %v", err)
	}
	got, _ = svc.GetGroup(2, g.GUID)
	if got.MyNickname != "" {
		t.Fatalf("清除后群昵称应为空，实际: %q", got.MyNickname)
	}
}

// ---- 第三期（P2）：G7 入群确认 / G8 群禁言 / G10 保存到通讯录 ----

// p2SetupGroup 构造测试群：群主 1、管理员 2、普通成员 3；用户 4 为外部待邀请者。
func p2SetupGroup(t *testing.T) (*Service, *mockStore, int64) {
	t.Helper()
	svc, store := newTestSvc()
	store.users[1] = &mysql.User{UID: 1, Nickname: "A"}
	store.users[2] = &mysql.User{UID: 2, Nickname: "B"}
	store.users[3] = &mysql.User{UID: 3, Nickname: "C"}
	store.users[4] = &mysql.User{UID: 4, Nickname: "D"}
	if err := store.CreateGroup(&mysql.Group{ID: 1, GUID: 101, Name: "测试群", OwnerUID: 1, MemberCount: 3, ConvID: 1001}); err != nil {
		t.Fatalf("create group: %v", err)
	}
	_ = store.AddGroupMember(101, 1, 0) // 群主
	_ = store.AddGroupMember(101, 2, 1) // 管理员
	_ = store.AddGroupMember(101, 3, 2) // 普通成员
	return svc, store, 101
}

// G7 入群确认：开启后邀请不直接入群 → 通知群主/管理员 → 同意后入群。
func TestG7InviteConfirm(t *testing.T) {
	svc, store, gid := p2SetupGroup(t)
	one := int8(1)
	if err := svc.UpdateGroupSettings(1, gid, &one, nil); err != nil {
		t.Fatalf("enable invite confirm: %v", err)
	}
	if g, _ := store.GetGroupByGUID(gid); g.InviteConfirm != 1 {
		t.Fatalf("invite_confirm 未持久化")
	}
	// 普通成员 3 邀请 4：不直接入群，返回提示
	if err := svc.InviteToGroup(3, gid, []int64{4}); err == nil {
		t.Fatal("开启入群确认后邀请应被延迟")
	}
	if in, _ := store.IsGroupMember(gid, 4); in {
		t.Fatal("确认前成员不应入群")
	}
	found := false
	for _, n := range store.notifs {
		if n.Type == "group_invite_confirm" {
			found = true
		}
	}
	if !found {
		t.Fatal("应写入入群确认通知")
	}
	// 普通成员无权决定
	if err := svc.DecideInvite(3, gid, 4, true); err == nil {
		t.Fatal("普通成员不应能处理入群申请")
	}
	// 管理员同意入群
	if err := svc.DecideInvite(2, gid, 4, true); err != nil {
		t.Fatalf("管理员同意入群应成功: %v", err)
	}
	if in, _ := store.IsGroupMember(gid, 4); !in {
		t.Fatal("同意后成员应入群")
	}
	// 重复同意幂等
	if err := svc.DecideInvite(2, gid, 4, true); err != nil {
		t.Fatalf("重复同意应幂等: %v", err)
	}
	// 拒绝分支
	if err := svc.DecideInvite(2, gid, 3, false); err != nil {
		t.Fatalf("拒绝应成功: %v", err)
	}
}

// G8 群禁言：全员禁言拦截普通成员豁免管理员；个人禁言对管理员同样生效；管理员不能禁言群主。
func TestG8MuteGuard(t *testing.T) {
	svc, _, gid := p2SetupGroup(t)
	one := int8(1)
	if err := svc.UpdateGroupSettings(1, gid, nil, &one); err != nil {
		t.Fatalf("enable mute all: %v", err)
	}
	if muted, _ := svc.GroupMuteCheck(gid, 3); !muted {
		t.Fatal("全员禁言下普通成员应被拦截")
	}
	if muted, _ := svc.GroupMuteCheck(gid, 2); muted {
		t.Fatal("全员禁言不应拦截管理员")
	}
	if muted, _ := svc.GroupMuteCheck(gid, 1); muted {
		t.Fatal("全员禁言不应拦截群主")
	}
	// 个人禁言：管理员也被拦截
	future := time.Now().Add(time.Hour).UnixMilli()
	if err := svc.SetMemberMutedUntil(1, gid, 2, future); err != nil {
		t.Fatalf("禁言成员应成功: %v", err)
	}
	if muted, _ := svc.GroupMuteCheck(gid, 2); !muted {
		t.Fatal("个人禁言应拦截管理员")
	}
	// 管理员不能禁言群主/其他管理员
	if err := svc.SetMemberMutedUntil(2, gid, 1, future); err == nil {
		t.Fatal("管理员不应能禁言群主")
	}
	// 解除禁言
	if err := svc.SetMemberMutedUntil(1, gid, 2, 0); err != nil {
		t.Fatalf("解除禁言应成功: %v", err)
	}
	if muted, _ := svc.GroupMuteCheck(gid, 2); muted {
		t.Fatal("解除后管理员不应再被拦截")
	}
}

// G10 保存到通讯录：任何成员可操作自己的行；群列表回显 saved；非成员拒绝。
func TestG10Saved(t *testing.T) {
	svc, _, gid := p2SetupGroup(t)
	if err := svc.UpdateGroupSaved(3, gid, 0); err != nil {
		t.Fatalf("关闭保存到通讯录应成功: %v", err)
	}
	groups, err := svc.ListUserGroups(3)
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	if len(groups) != 1 || groups[0].Saved != 0 {
		t.Fatalf("saved 未回显到群列表: %+v", groups)
	}
	if err := svc.UpdateGroupSaved(4, gid, 0); err == nil {
		t.Fatal("非成员不应能修改 saved")
	}
	// 重新开启
	if err := svc.UpdateGroupSaved(3, gid, 1); err != nil {
		t.Fatalf("重新开启应成功: %v", err)
	}
}
