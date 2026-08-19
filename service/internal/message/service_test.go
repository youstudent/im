package message

import (
	"os"
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

// ---- 内存 mock Store ----

type mockStore struct {
	mu           sync.Mutex
	convs        map[int64]*mysql.Conversation // key: ownerUID:targetID
	msgs         map[int64][]*mysql.Message    // key: convID
	readSeq      map[string]int64              // key: uid:convID
	convSeq      int64
	msgSeq       map[int64]int64
	missingUIDs  map[int64]bool // 视为“不存在”的 uid（发送守卫测试用）
	disabledUIDs map[int64]bool // 视为“已禁用”的 uid（发送守卫测试用）
}

func newMockStore() *mockStore {
	return &mockStore{convs: map[int64]*mysql.Conversation{}, msgs: map[int64][]*mysql.Message{}, readSeq: map[string]int64{}, msgSeq: map[int64]int64{}, missingUIDs: map[int64]bool{}, disabledUIDs: map[int64]bool{}}
}

func convKey(a, b int64) int64 { return a*1000000 + b }

func (m *mockStore) GetOrCreateConversation(ownerUID, targetID int64, typ int8, newID int64) (*mysql.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := convKey(ownerUID, targetID)
	if c, ok := m.convs[k]; ok {
		return c, nil
	}
	m.convSeq++
	c := &mysql.Conversation{ID: m.convSeq, Type: typ, OwnerUID: ownerUID, TargetID: targetID}
	m.convs[k] = c
	return c, nil
}
func (m *mockStore) EnsureConversationID(ownerUID, targetID int64, typ int8, convID int64) (*mysql.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := convKey(ownerUID, targetID)
	if c, ok := m.convs[k]; ok {
		if c.ID == convID {
			return c, nil
		}
		// 已存在但 ID 不同：统一到新 convID（模拟 store 的迁移语义）
		c.ID = convID
		return c, nil
	}
	c := &mysql.Conversation{ID: convID, Type: typ, OwnerUID: ownerUID, TargetID: targetID}
	m.convs[k] = c
	return c, nil
}
func (m *mockStore) GetConversationByID(convID int64) (*mysql.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.convs {
		if c.ID == convID {
			return c, nil
		}
	}
	return nil, mysql.ErrNotFound
}
func (m *mockStore) GetConversation(ownerUID, targetID int64) (*mysql.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.convs[convKey(ownerUID, targetID)]
	if !ok {
		return nil, mysql.ErrNotFound
	}
	return c, nil
}
func (m *mockStore) IsConversationMember(uid, convID int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.convs {
		if c.ID == convID && c.OwnerUID == uid {
			return true, nil
		}
	}
	return false, nil
}
func (m *mockStore) ListConversations(ownerUID int64) ([]*mysql.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []*mysql.Conversation
	for _, c := range m.convs {
		if c.OwnerUID == ownerUID {
			list = append(list, c)
		}
	}
	return list, nil
}
func (m *mockStore) ListConversationsChangedSince(ownerUID int64, since time.Time) ([]*mysql.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []*mysql.Conversation
	for _, c := range m.convs {
		if c.OwnerUID != ownerUID {
			continue
		}
		// 空会话（无消息）视为“新变化”一并返回，与服务端语义一致
		if c.LastMsgTime == nil || c.LastMsgTime.After(since) {
			list = append(list, c)
		}
	}
	return list, nil
}
func (m *mockStore) UpdateConversationLastMsg(ownerUID, targetID int64, lastMsgID int64, text string, t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.convs[convKey(ownerUID, targetID)]; ok {
		lastMsgIDCopy := lastMsgID
		textCopy := text
		timeCopy := t
		c.LastMsgID = lastMsgIDCopy
		c.LastMsgText = textCopy
		c.LastMsgTime = &timeCopy
	}
	return nil
}
func (m *mockStore) UpdateConversationSyncedSeq(ownerUID, targetID, seq int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.convs[convKey(ownerUID, targetID)]; ok {
		c.LastSyncedSeq = seq
	}
	return nil
}

// ---- 群聊批量操作（镜像 store 的 SQL 语义） ----

func (m *mockStore) GetUserNames(uids []int64) map[int64]string {
	out := make(map[int64]string)
	for _, uid := range uids {
		out[uid] = m.GetUserName(uid)
	}
	return out
}

func (m *mockStore) EnsureGroupConversationViews(convID, gUID int64, memberUIDs []int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, uid := range memberUIDs {
		k := convKey(uid, gUID)
		if _, ok := m.convs[k]; ok {
			continue // INSERT IGNORE 语义
		}
		m.convs[k] = &mysql.Conversation{ID: convID, Type: 2, OwnerUID: uid, TargetID: gUID}
	}
	return nil
}

func (m *mockStore) UpdateGroupConversationsLastMsg(gUID, lastMsgID int64, text string, t time.Time) error {
	return m.updateGroupLastMsg(gUID, 0, false, lastMsgID, text, t)
}

func (m *mockStore) UpdateGroupConversationsLastMsgExcept(gUID, excludeUID, lastMsgID int64, text string, t time.Time) error {
	return m.updateGroupLastMsg(gUID, excludeUID, true, lastMsgID, text, t)
}

func (m *mockStore) updateGroupLastMsg(gUID, excludeUID int64, exclude bool, lastMsgID int64, text string, t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.convs {
		if c.Type != 2 || c.TargetID != gUID {
			continue
		}
		if exclude && c.OwnerUID == excludeUID {
			continue
		}
		timeCopy := t
		c.LastMsgID = lastMsgID
		c.LastMsgText = text
		c.LastMsgTime = &timeCopy
	}
	return nil
}

func (m *mockStore) UpdateGroupConversationsSyncedSeq(gUID, seq, excludeUID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.convs {
		if c.Type != 2 || c.TargetID != gUID || c.OwnerUID == excludeUID {
			continue
		}
		if c.LastSyncedSeq < seq {
			c.LastSyncedSeq = seq
		}
	}
	return nil
}

// ---- 未读计数（unread_count 列镜像） ----

func (m *mockStore) BumpConversationUnread(ownerUID, targetID, delta int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.convs[convKey(ownerUID, targetID)]; ok {
		c.UnreadCount = clampMin0(c.UnreadCount + delta)
	}
	return nil
}

func (m *mockStore) BumpGroupConversationsUnread(gUID, excludeUID, delta int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.convs {
		if c.Type != 2 || c.TargetID != gUID || c.OwnerUID == excludeUID {
			continue
		}
		c.UnreadCount = clampMin0(c.UnreadCount + delta)
	}
	return nil
}

func clampMin0(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
}

func (m *mockStore) CreateMessage(msg *mysql.Message) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgSeq[msg.ConvID]++
	msg.Seq = m.msgSeq[msg.ConvID]
	m.msgs[msg.ConvID] = append(m.msgs[msg.ConvID], msg)
	return msg.Seq, nil
}
func (m *mockStore) NextSeq(convID int64) (int64, error) { return 0, nil }
func (m *mockStore) GetMessage(convID, msgID int64) (*mysql.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, msg := range m.msgs[convID] {
		if msg.ID == msgID {
			return msg, nil
		}
	}
	return nil, mysql.ErrNotFound
}
func (m *mockStore) UpdateMessageStatus(convID, msgID int64, status int8) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, msg := range m.msgs[convID] {
		if msg.ID == msgID {
			msg.Status = status
		}
	}
	return nil
}
func (m *mockStore) GetLastActiveMessage(convID int64) (*mysql.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var last *mysql.Message
	for _, msg := range m.msgs[convID] {
		if msg.Status != 0 {
			continue
		}
		if last == nil || msg.Seq > last.Seq {
			last = msg
		}
	}
	return last, nil
}
func (m *mockStore) ListMessages(convID, afterSeq int64, limit int) ([]*mysql.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*mysql.Message
	for _, msg := range m.msgs[convID] {
		if msg.Seq > afterSeq {
			out = append(out, msg)
		}
	}
	return out, nil
}
func (m *mockStore) ListMessagesBefore(convID, beforeSeq int64, limit int) ([]*mysql.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*mysql.Message
	for _, msg := range m.msgs[convID] {
		if beforeSeq <= 0 || msg.Seq < beforeSeq {
			out = append(out, msg)
		}
	}
	return out, nil
}
func (m *mockStore) MessageExists(convID, msgID int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, msg := range m.msgs[convID] {
		if msg.ID == msgID {
			return true, nil
		}
	}
	return false, nil
}
func (m *mockStore) GetLastReadSeq(uid, convID int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readSeq[itoa(uid)+":"+itoa(convID)], nil
}
func (m *mockStore) UpsertReadSeq(uid, convID, seq int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readSeq[itoa(uid)+":"+itoa(convID)] = seq
	// 已读清零（镜像 store：UPDATE conversations SET unread_count=0）
	for _, c := range m.convs {
		if c.OwnerUID == uid && c.ID == convID {
			c.UnreadCount = 0
		}
	}
	return nil
}
func (m *mockStore) SearchMessages(keyword string, msgType int8, convIDs []int64, limit int) ([]*mysql.SearchResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(convIDs) == 0 {
		return nil, nil // 镜像 DAO 层兜底：无会话范围不搜
	}
	allowed := make(map[int64]bool, len(convIDs))
	for _, id := range convIDs {
		allowed[id] = true
	}
	var out []*mysql.SearchResult
	for convID, msgs := range m.msgs {
		if !allowed[convID] {
			continue
		}
		for _, msg := range msgs {
			if keyword != "" && !strings.Contains(msg.Content, keyword) {
				continue
			}
			if msgType > 0 && msg.Type != msgType {
				continue
			}
			out = append(out, &mysql.SearchResult{Message: msg, ConvID: convID})
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}
func (m *mockStore) GetUserName(uid int64) string {
	return "用户" + itoa(uid)
}

// GetUserByUID 默认所有 uid 存在（与 GetUserName 语义一致）；
// missingUIDs/disabledUIDs 集合用于模拟不存在/已禁用用户（发送守卫测试）。
func (m *mockStore) GetUserByUID(uid int64) (*mysql.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.missingUIDs[uid] {
		return nil, mysql.ErrNotFound
	}
	u := &mysql.User{UID: uid, Nickname: "用户" + itoa(uid)}
	if m.disabledUIDs[uid] {
		u.Disabled = 1
	}
	return u, nil
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func newTestSvc() (*Service, *mockStore, *[]*MessageDTO) {
	store := newMockStore()
	pushed := &[]*MessageDTO{}
	idSeq := &struct{ n int64 }{}
	svc := New(store, func() int64 { idSeq.n++; return idSeq.n }, func(_ int64, m *MessageDTO) {
		*pushed = append(*pushed, m)
	}, nil)
	return svc, store, pushed
}

func TestRecallMessage(t *testing.T) {
	svc, store, _ := newTestSvc()
	// 1001 向 1002 发送单聊消息（发送时间设为当前）
	_, _, err := svc.Send(1001, &SendReq{TargetID: 1002, ConvType: 1, Type: 1, Content: "撤回测试"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	conv, _ := store.GetConversation(1001, 1002)
	msgs, _ := store.ListMessages(conv.ID, 0, 0)
	if len(msgs) == 0 {
		t.Fatal("no message")
	}
	msgID := msgs[0].ID

	// 1) 非发送者撤回 → 拒绝
	if _, err := svc.RecallMessage(1002, conv.ID, msgID); err == nil {
		t.Fatal("非发送者应被拒绝撤回")
	}

	// 2) 发送者撤回 → 成功，status=1
	dto, err := svc.RecallMessage(1001, conv.ID, msgID)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if dto.Status != 1 {
		t.Fatalf("recall status=%d, want 1", dto.Status)
	}

	// 3) 重复撤回 → 拒绝
	if _, err := svc.RecallMessage(1001, conv.ID, msgID); err == nil {
		t.Fatal("重复撤回应被拒绝")
	}
}

func TestRecallMessageTimeout(t *testing.T) {
	svc, store, _ := newTestSvc()
	_, _, err := svc.Send(1001, &SendReq{TargetID: 1002, ConvType: 1, Type: 1, Content: "旧消息"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	conv, _ := store.GetConversation(1001, 1002)
	msgs, _ := store.ListMessages(conv.ID, 0, 0)
	msgID := msgs[0].ID

	// 把消息发送时间改到 3 分钟前，触发超时限制
	store.mu.Lock()
	for _, msg := range store.msgs[conv.ID] {
		if msg.ID == msgID {
			msg.CreatedAt = time.Now().Add(-3 * time.Minute)
		}
	}
	store.mu.Unlock()

	if _, err := svc.RecallMessage(1001, conv.ID, msgID); err == nil {
		t.Fatal("超过 2 分钟应被拒绝撤回")
	} else if !strings.Contains(err.Error(), "2 分钟") {
		t.Fatalf("错误信息不含超时提示: %v", err)
	}
}

func TestSendAndHistory(t *testing.T) {
	svc, _, pushed := newTestSvc()

	// 用户 1 发消息给用户 2
	dto, _, err := svc.Send(1001, &SendReq{TargetID: 1002, Type: 1, Content: "你好"})
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if dto.Seq != 1 || dto.Content != "你好" || dto.SenderUID != 1001 {
		t.Fatalf("unexpected dto: %+v", dto)
	}
	// publish 回调应收到推送
	if len(*pushed) != 1 || (*pushed)[0].Seq != 1 {
		t.Fatalf("expected 1 push, got %d", len(*pushed))
	}

	// 再发一条
	_, _, _ = svc.Send(1002, &SendReq{TargetID: 1001, Type: 1, Content: "在的"})
	if len(*pushed) != 2 {
		t.Fatalf("expected 2 pushes, got %d", len(*pushed))
	}
}

func TestMsgIDDedup(t *testing.T) {
	svc, _, pushed := newTestSvc()
	req := &SendReq{TargetID: 1002, Type: 1, Content: "同一条", MsgID: 999}
	if _, _, err := svc.Send(1001, req); err != nil {
		t.Fatalf("send1: %v", err)
	}
	// 相同 msgID 重复发送应幂等
	_, isNew, err := svc.Send(1001, req)
	if err != nil {
		t.Fatalf("send2: %v", err)
	}
	if isNew {
		t.Fatalf("expected dedup send to be not-new")
	}
	if len(*pushed) != 1 {
		t.Fatalf("expected dedup to 1 push, got %d", len(*pushed))
	}
}

func TestMarkRead(t *testing.T) {
	svc, _, _ := newTestSvc()
	if err := svc.MarkRead(1002, 5, 3); err != nil {
		t.Fatalf("markread: %v", err)
	}
}

// TestGroupSendSyncsAllMembersConvLastMsg 验证：群聊发送消息后，
// 群内每个成员（含发送方）的会话 last_msg_id/text/time 都被更新为同一条消息。
func TestGroupSendSyncsAllMembersConvLastMsg(t *testing.T) {
	svc, store, _ := newTestSvc()
	// 注入群成员查询：群 g_uid=5001 有成员 1001（发送方）、1002、1003
	svc.SetGroupMembers(func(gUID int64) ([]int64, error) {
		if gUID == 5001 {
			return []int64{1001, 1002, 1003}, nil
		}
		return nil, nil
	})
	// 群消息由 1001 发送给群 5001
	if _, _, err := svc.Send(1001, &SendReq{TargetID: 5001, ConvType: 2, Type: 1, Content: "群公告"}); err != nil {
		t.Fatalf("send failed: %v", err)
	}

	// 校验三个成员的会话 last_msg 都同步到同一条消息
	for _, owner := range []int64{1001, 1002, 1003} {
		conv, err := store.GetConversation(owner, 5001)
		if err != nil {
			t.Fatalf("conversation for owner %d not found: %v", owner, err)
		}
		if conv.LastMsgText != "群公告" || conv.LastMsgID == 0 {
			t.Fatalf("owner %d conv last_msg not updated: text=%q id=%d", owner, conv.LastMsgText, conv.LastMsgID)
		}
		// 接收方（1002/1003）已同步游标应更新为最新 seq；发送方（1001）不更新，避免误显未读
		wantSeq := int64(1)
		if owner == 1001 {
			wantSeq = 0
		}
		if conv.LastSyncedSeq != wantSeq {
			t.Fatalf("owner %d conv synced seq=%d, want %d", owner, conv.LastSyncedSeq, wantSeq)
		}
	}

	// 一致性：所有成员 last_msg_id 相同
	id0 := int64(0)
	for _, owner := range []int64{1001, 1002, 1003} {
		conv, _ := store.GetConversation(owner, 5001)
		if id0 == 0 {
			id0 = conv.LastMsgID
		} else if conv.LastMsgID != id0 {
			t.Fatalf("members last_msg_id inconsistent: owner %d id=%d, base=%d", owner, conv.LastMsgID, id0)
		}
	}
}

// TestGroupSendUnreadBumpAndRecall 验证群聊未读计数（P2 unread_count）：
// 发送后除发送者外成员 +1；撤回后 -1 回落；幂等重发不重复累加。
func TestGroupSendUnreadBumpAndRecall(t *testing.T) {
	svc, store, _ := newTestSvc()
	svc.SetGroupMembers(func(gUID int64) ([]int64, error) {
		if gUID == 5001 {
			return []int64{1001, 1002, 1003}, nil
		}
		return nil, nil
	})
	dto, _, err := svc.Send(1001, &SendReq{TargetID: 5001, ConvType: 2, Type: 1, Content: "群消息"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	for owner, want := range map[int64]int64{1001: 0, 1002: 1, 1003: 1} {
		conv, err := store.GetConversation(owner, 5001)
		if err != nil {
			t.Fatalf("conv for %d: %v", owner, err)
		}
		if conv.UnreadCount != want {
			t.Fatalf("owner %d unread=%d, want %d", owner, conv.UnreadCount, want)
		}
	}
	// 幂等重发（同 msg_id）不重复累加未读
	if _, isNew, err := svc.Send(1001, &SendReq{TargetID: 5001, ConvType: 2, Type: 1, Content: "群消息", MsgID: dto.ID}); err != nil || isNew {
		t.Fatalf("dedup send: isNew=%v err=%v", isNew, err)
	}
	if conv, _ := store.GetConversation(1002, 5001); conv.UnreadCount != 1 {
		t.Fatalf("dedup send should not bump unread, got %d", conv.UnreadCount)
	}
	// 撤回后接收方未读回落 0
	if _, err := svc.RecallMessage(1001, dto.ConvID, dto.ID); err != nil {
		t.Fatalf("recall: %v", err)
	}
	for owner, want := range map[int64]int64{1001: 0, 1002: 0, 1003: 0} {
		conv, _ := store.GetConversation(owner, 5001)
		if conv.UnreadCount != want {
			t.Fatalf("after recall owner %d unread=%d, want %d", owner, conv.UnreadCount, want)
		}
	}
}

// TestUnreadCountLifecycle 验证单聊未读全链路（P2 unread_count 列）：
// 发消息接收方 +1、会话列表直读列、已读清零、撤回递减不虚高。
func TestUnreadCountLifecycle(t *testing.T) {
	svc, store, _ := newTestSvc()
	if _, _, err := svc.Send(1001, &SendReq{TargetID: 1002, ConvType: 1, Type: 1, Content: "第一条"}); err != nil {
		t.Fatalf("send1: %v", err)
	}
	if _, _, err := svc.Send(1001, &SendReq{TargetID: 1002, ConvType: 1, Type: 1, Content: "第二条"}); err != nil {
		t.Fatalf("send2: %v", err)
	}
	conv, err := store.GetConversation(1002, 1001)
	if err != nil {
		t.Fatalf("receiver conv: %v", err)
	}
	if conv.UnreadCount != 2 {
		t.Fatalf("receiver unread=%d, want 2", conv.UnreadCount)
	}
	// 会话列表未读数直接来自 unread_count 列
	list, err := svc.ListConversations(1002)
	if err != nil || len(list) != 1 {
		t.Fatalf("list convs: %v len=%d", err, len(list))
	}
	if list[0].Unread != 2 {
		t.Fatalf("list unread=%d, want 2", list[0].Unread)
	}
	// 已读清零
	if err := svc.MarkRead(1002, conv.ID, 2); err != nil {
		t.Fatalf("markread: %v", err)
	}
	if conv.UnreadCount != 0 {
		t.Fatalf("after markread unread=%d, want 0", conv.UnreadCount)
	}
	// 再来一条后撤回：未读先 +1 再 -1 回落 0（旧 seq 差值方案此处会虚高）
	dto, _, err := svc.Send(1001, &SendReq{TargetID: 1002, ConvType: 1, Type: 1, Content: "将被撤回"})
	if err != nil {
		t.Fatalf("send3: %v", err)
	}
	if conv.UnreadCount != 1 {
		t.Fatalf("unread=%d, want 1", conv.UnreadCount)
	}
	if _, err := svc.RecallMessage(1001, conv.ID, dto.ID); err != nil {
		t.Fatalf("recall: %v", err)
	}
	if conv.UnreadCount != 0 {
		t.Fatalf("after recall unread=%d, want 0 (不虚高)", conv.UnreadCount)
	}
}

// TestSendGroupSystemMessageBatchViews 验证建群系统消息（审计 P1 批量化）：
// 所有成员视图由单条批量语义创建，last_msg 为 [系统消息]，且不累加未读。
func TestSendGroupSystemMessageBatchViews(t *testing.T) {
	svc, store, _ := newTestSvc()
	var pushedTo []int64
	svc.SetPushFunc(func(uid int64, _ *MessageDTO) { pushedTo = append(pushedTo, uid) })

	svc.SendGroupSystemMessage(1001, 6001, 88888, "群「项目群」已创建", "", []int64{1002, 1003})

	for _, owner := range []int64{1001, 1002, 1003} {
		conv, err := store.GetConversation(owner, 6001)
		if err != nil {
			t.Fatalf("conv for %d: %v", owner, err)
		}
		if conv.ID != 88888 {
			t.Fatalf("owner %d conv id=%d, want shared 88888", owner, conv.ID)
		}
		if conv.LastMsgText != "[系统消息]" {
			t.Fatalf("owner %d last_msg_text=%q, want [系统消息]", owner, conv.LastMsgText)
		}
		if conv.UnreadCount != 0 {
			t.Fatalf("owner %d unread=%d, system msg should not bump unread", owner, conv.UnreadCount)
		}
	}
	if len(pushedTo) != 3 {
		t.Fatalf("pushed to %d members, want 3", len(pushedTo))
	}
}

// TestSearchScopedAndBatchNames 验证搜索（审计 P1）：
// 仅命中当前用户自己会话内的消息（防越权），且昵称批量组装正确。
func TestSearchScopedAndBatchNames(t *testing.T) {
	svc, _, _ := newTestSvc()
	if _, _, err := svc.Send(1001, &SendReq{TargetID: 1002, ConvType: 1, Type: 1, Content: "项目会议安排"}); err != nil {
		t.Fatalf("send1: %v", err)
	}
	if _, _, err := svc.Send(1003, &SendReq{TargetID: 1004, ConvType: 1, Type: 1, Content: "项目机密文档"}); err != nil {
		t.Fatalf("send2: %v", err)
	}
	// 1001 搜 "项目"：只能看到自己会话内的消息，搜不到 1003 的
	res, err := svc.Search(1001, "项目", 0, 50)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("search hits=%d, want 1 (仅限自身会话)", len(res))
	}
	if res[0].Message.Content != "项目会议安排" {
		t.Fatalf("unexpected hit: %q", res[0].Message.Content)
	}
	if res[0].Message.SenderName != "用户1001" {
		t.Fatalf("sender name=%q, want 用户1001", res[0].Message.SenderName)
	}
	// 无会话的用户搜索返回空（DAO 层兜底不全表扫）
	empty, err := svc.Search(9999, "项目", 0, 50)
	if err != nil || len(empty) != 0 {
		t.Fatalf("search for user without convs: %v len=%d", err, len(empty))
	}
}

// TestSendGuards 验证发送守卫（审计 P1）：
// 单聊目标不存在/已禁用拒绝；群聊非成员拒绝。
func TestSendGuards(t *testing.T) {
	svc, store, _ := newTestSvc()
	svc.SetGroupMembers(func(gUID int64) ([]int64, error) {
		if gUID == 5001 {
			return []int64{1001, 1002}, nil
		}
		return nil, mysql.ErrNotFound
	})
	store.missingUIDs[8888] = true
	store.disabledUIDs[7777] = true

	// 1) 单聊：目标不存在
	if _, _, err := svc.Send(1001, &SendReq{TargetID: 8888, ConvType: 1, Type: 1, Content: "hi"}); err == nil {
		t.Fatal("目标不存在的单聊应被拒绝")
	}
	// 2) 单聊：目标已禁用
	if _, _, err := svc.Send(1001, &SendReq{TargetID: 7777, ConvType: 1, Type: 1, Content: "hi"}); err == nil {
		t.Fatal("目标已禁用的单聊应被拒绝")
	}
	// 3) 群聊：非成员发送被拒
	if _, _, err := svc.Send(9999, &SendReq{TargetID: 5001, ConvType: 2, Type: 1, Content: "灌水"}); err == nil {
		t.Fatal("非群成员发送应被拒绝")
	}
	// 4) 群聊：群不存在（成员查询报错）被拒
	if _, _, err := svc.Send(1001, &SendReq{TargetID: 9999, ConvType: 2, Type: 1, Content: "hi"}); err == nil {
		t.Fatal("群不存在时发送应被拒绝")
	}
	// 5) 群聊：成员正常发送不受影响
	if _, _, err := svc.Send(1001, &SendReq{TargetID: 5001, ConvType: 2, Type: 1, Content: "正常消息"}); err != nil {
		t.Fatalf("群成员发送不应被拒: %v", err)
	}
}

// TestHistoryRequiresMembership 验证历史拉取归属校验（审计 P0）：
// 仅会话参与者（双方视图持有者）可拉取，非参与者拒绝（防越权读取任意会话）。
func TestHistoryRequiresMembership(t *testing.T) {
	svc, store, _ := newTestSvc()
	if _, _, err := svc.Send(1001, &SendReq{TargetID: 1002, ConvType: 1, Type: 1, Content: "私密消息"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	conv, err := store.GetConversation(1001, 1002)
	if err != nil {
		t.Fatalf("conv: %v", err)
	}

	// 参与者（发送方/接收方）均可拉取
	if list, err := svc.GetHistory(1001, conv.ID, 0, 50); err != nil || len(list) != 1 {
		t.Fatalf("sender GetHistory: %v len=%d", err, len(list))
	}
	if list, err := svc.GetHistory(1002, conv.ID, 0, 50); err != nil || len(list) != 1 {
		t.Fatalf("receiver GetHistory: %v len=%d", err, len(list))
	}
	if list, err := svc.GetHistoryAfterSeq(1002, conv.ID, 0, 50); err != nil || len(list) != 1 {
		t.Fatalf("receiver GetHistoryAfterSeq: %v len=%d", err, len(list))
	}

	// 非参与者：倒序/增量均被拒绝
	if _, err := svc.GetHistory(9999, conv.ID, 0, 50); err == nil {
		t.Fatal("非参与者应被拒绝拉取历史")
	}
	if _, err := svc.GetHistoryAfterSeq(9999, conv.ID, 0, 50); err == nil {
		t.Fatal("非参与者应被拒绝增量拉取")
	}
}
