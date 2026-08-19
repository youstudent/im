// Package social 实现社交领域：好友、群组、通知。
package social

import (
	"encoding/json"
	"strings"
	"time"

	apperr "im/service/internal/pkg/err"
	"im/service/internal/store/mysql"
)

// Store 社交模块依赖的存储接口。
type Store interface {
	// 好友
	AddFriend(uid, friendUID int64) error
	DeleteFriend(uid, friendUID int64) error
	ListFriends(uid int64) ([]*mysql.Friend, error)
	AreFriends(uid, friendUID int64) (bool, error)
	UpdateFriendRemark(uid, friendUID int64, remark string) error
	// 申请
	CreateFriendRequest(r *mysql.FriendRequest) (int64, error)
	GetFriendRequest(id int64) (*mysql.FriendRequest, error)
	ListFriendRequests(toUID int64) ([]*mysql.FriendRequest, error)
	UpdateFriendRequestStatus(id int64, status int8) error
	// 群
	CreateGroup(g *mysql.Group) error
	GetGroupByGUID(gUID int64) (*mysql.Group, error)
	AddGroupMember(gUID, uid int64, role int8) error
	RemoveGroupMember(gUID, uid int64) error
	IsGroupMember(gUID, uid int64) (bool, error)
	GetGroupMemberRole(gUID, uid int64) (int8, error)
	ListGroupMembers(gUID int64) ([]int64, error)
	ListUserGroups(uid int64) ([]int64, error)
	UpdateGroup(gUID int64, name, announcement string) error
	GUIDExists(gUID int64) (bool, error)
	DeleteGroupConversationView(ownerUID, gUID int64) error
	// 通知
	CreateNotification(n *mysql.Notification) error
	ListNotifications(uid int64) ([]*mysql.Notification, error)
	MarkNotificationRead(id, uid int64) error
	MarkAllNotificationsRead(uid int64) error
	CountUnreadNotifications(uid int64) (int, error)
	ClearNotifications(uid int64) error
	// 用户
	GetUserByUID(uid int64) (*mysql.User, error)
	GetUserByAccount(account string) (*mysql.User, error)
	SearchUsers(keyword string, limit int) ([]*mysql.User, error)
	// 会话（接受好友申请时建立双方会话视图）
	GetOrCreateConversation(ownerUID, targetID int64, typ int8, newID int64) (*mysql.Conversation, error)
}

// Notifier 推送通知：由上层注入，向用户实时推送通知/事件（可选）。
type Notifier func(uid int64, event string, data interface{})

// GroupSysMsgSender 群系统消息发送能力（由上层注入消息服务实现，可选）。
// content 为共享存储文案（其他成员视角）；extra 携带结构化邀请信息，客户端按查看者身份渲染个性化文案。
type GroupSysMsgSender func(ownerUID, gUID, convID int64, content, extra string, memberUIDs []int64)

// GroupSysMsgToSender 仅对指定成员可见的群系统消息发送能力（如退群消息仅群主可见，可选）。
type GroupSysMsgToSender func(gUID, convID int64, content, extra string, recipientUIDs []int64)

// Service 社交服务。
type Service struct {
	store    Store
	genID    func() int64
	notify   Notifier
	groupSysMsg GroupSysMsgSender // 可选；建群后发送系统消息
	groupSysMsgTo GroupSysMsgToSender // 可选；定向可见的群系统消息（退群）
}

// New 创建社交服务。
func New(store Store, genID func() int64, notify Notifier) *Service {
	return &Service{store: store, genID: genID, notify: notify}
}

// SetGroupSysMsgSender 运行时注入群系统消息发送能力。
func (s *Service) SetGroupSysMsgSender(fn GroupSysMsgSender) {
	s.groupSysMsg = fn
}

// SetGroupSysMsgToSender 运行时注入定向可见的群系统消息发送能力。
func (s *Service) SetGroupSysMsgToSender(fn GroupSysMsgToSender) {
	s.groupSysMsgTo = fn
}

// ---- 好友 ----

// FriendDTO 好友信息（含对方用户摘要）。
type FriendDTO struct {
	UID      int64  `json:"uid"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar,omitempty"`
	Remark   string `json:"remark,omitempty"`
	Online   bool   `json:"online"`
}

// FriendRequestDTO 好友申请（含申请人信息）。
// ID 为雪花 ID（19 位），用字符串传输避免前端 JS Number 精度丢失。
type FriendRequestDTO struct {
	ID      string `json:"id"`
	FromUID int64  `json:"from_uid"`
	Message string `json:"message"`
	Nickname string `json:"nickname"`
}

// SearchUser 按手机号/邮箱/昵称搜索用户（用于加好友），返回用户 uid 信息。
func (s *Service) SearchUser(account string) (*FriendDTO, error) {
	if account == "" {
		return nil, apperr.BadRequest("搜索关键字不能为空")
	}
	// 优先精确匹配账号（手机号/邮箱）
	u, err := s.store.GetUserByAccount(account)
	if err != nil {
		// 账号未精确匹配时，尝试按 uid 或昵称模糊搜索
		list, serr := s.store.SearchUsers(account, 1)
		if serr != nil || len(list) == 0 {
			return nil, apperr.NotFound("未找到该用户")
		}
		u = list[0]
	}
	return &FriendDTO{
		UID:      u.UID,
		Nickname: u.Nickname,
		Avatar:   u.Avatar,
		Remark:   u.Account,
	}, nil
}

// ListFriends 好友列表。
func (s *Service) ListFriends(uid int64) ([]*FriendDTO, error) {
	friends, err := s.store.ListFriends(uid)
	if err != nil {
		return nil, apperr.WrapInternal("获取好友列表失败", err)
	}
	list := make([]*FriendDTO, 0, len(friends))
	for _, f := range friends {
		u, err := s.store.GetUserByUID(f.FriendUID)
		if err != nil {
			continue
		}
		list = append(list, &FriendDTO{
			UID:      f.FriendUID,
			Nickname: u.Nickname,
			Avatar:   u.Avatar,
			Remark:   f.Remark,
		})
	}
	return list, nil
}

// SendFriendRequest 发送好友申请（不重复申请，写通知给接收方）。
// 拦截：不能加自己、对方已是好友、目标用户不存在。
func (s *Service) SendFriendRequest(fromUID, toUID int64, message string) error {
	if fromUID == toUID {
		return apperr.BadRequest("不能添加自己为好友")
	}
	// 目标用户必须存在，防止对任意 uid 创建申请/通知
	if _, err := s.store.GetUserByUID(toUID); err != nil {
		return apperr.BadRequest("目标用户不存在")
	}
	// 已是好友直接拦截，不再创建申请
	if ok, err := s.store.AreFriends(fromUID, toUID); err == nil && ok {
		return apperr.BadRequest("对方已是你的好友，无需重复添加")
	}
	id, err := s.store.CreateFriendRequest(&mysql.FriendRequest{
		ID: s.genID(), FromUID: fromUID, ToUID: toUID, Message: message, Status: 0,
	})
	if err != nil {
		return apperr.WrapInternal("发送好友申请失败", err)
	}
	// 通知接收方
	from, _ := s.store.GetUserByUID(fromUID)
	_ = s.store.CreateNotification(&mysql.Notification{
		ID: s.genID(), UID: toUID, Type: "friend", Title: "新的好友申请",
		Summary: from.Nickname + " 请求添加你为好友", Action: `{"req_id":"` + itoa(id) + `"}`,
	})
	if s.notify != nil {
		s.notify(toUID, "friend.request", ginMap("from_uid", fromUID, "message", message))
	}
	return nil
}

// HasPendingFriendRequest 判断当前用户是否有待处理的好友申请（登录返回红点状态用）。
func (s *Service) HasPendingFriendRequest(uid int64) bool {
	list, err := s.store.ListFriendRequests(uid)
	if err != nil {
		return false
	}
	return len(list) > 0
}

// ListFriendRequests 我收到的待处理好友申请（含申请人昵称）。
func (s *Service) ListFriendRequests(uid int64) ([]*FriendRequestDTO, error) {
	list, err := s.store.ListFriendRequests(uid)
	if err != nil {
		return nil, apperr.WrapInternal("获取好友申请失败", err)
	}
	out := make([]*FriendRequestDTO, 0, len(list))
	for _, r := range list {
		nick := ""
		if u, err := s.store.GetUserByUID(r.FromUID); err == nil {
			nick = u.Nickname
		}
		out = append(out, &FriendRequestDTO{
			ID: itoa(r.ID), FromUID: r.FromUID, Message: r.Message, Nickname: nick,
		})
	}
	return out, nil
}

// Info 返回用户昵称（供展示）。
func (s *Service) Info(uid int64) string {
	if u, err := s.store.GetUserByUID(uid); err == nil {
		return u.Nickname
	}
	return ""
}

// HandleFriendRequest 处理好友申请：接受或拒绝。
func (s *Service) HandleFriendRequest(uid int64, reqID int64, accept bool) error {
	req, err := s.store.GetFriendRequest(reqID)
	if err != nil {
		return apperr.NotFound("申请不存在")
	}
	if req.ToUID != uid {
		return apperr.Forbidden("无权处理该申请")
	}
	if req.Status != 0 {
		return apperr.Conflict("该申请已处理")
	}
	if accept {
		if err := s.store.AddFriend(req.FromUID, req.ToUID); err != nil {
			return apperr.WrapInternal("添加好友失败", err)
		}
		_ = s.store.UpdateFriendRequestStatus(reqID, 1)

		// 建立双方单聊会话视图（双方各一条，共享同一个会话 ID，便于唯一标识一对聊天关系）
		from, _ := s.store.GetUserByUID(req.FromUID)
		to, _ := s.store.GetUserByUID(req.ToUID)
		convID := s.genID()
		_, _ = s.store.GetOrCreateConversation(req.FromUID, req.ToUID, 1, convID)
		_, _ = s.store.GetOrCreateConversation(req.ToUID, req.FromUID, 1, convID)

		// 通知申请人：申请已通过
		_ = s.store.CreateNotification(&mysql.Notification{
			ID: s.genID(), UID: req.FromUID, Type: "friend", Title: "好友申请已通过",
			Summary: to.Nickname + " 已接受你的好友申请",
		})
		if s.notify != nil {
			// WS 推送：申请人收到好友通过 + 新会话通知（conv_id 传真实会话 ID 字符串，
			// 客户端据此增量插入会话项，无需全量重载会话列表）
			s.notify(req.FromUID, "friend.accepted", ginMap("uid", req.ToUID, "nickname", to.Nickname))
			s.notify(req.FromUID, "conversation.created", ginMap("conv_id", itoa(convID), "target_id", req.ToUID, "target_name", to.Nickname, "type", 1))
			// WS 推送：被接受方（自己）新增会话
			s.notify(req.ToUID, "conversation.created", ginMap("conv_id", itoa(convID), "target_id", req.FromUID, "target_name", from.Nickname, "type", 1))
		}
	} else {
		_ = s.store.UpdateFriendRequestStatus(reqID, 2)
		// 通知申请人被拒绝
		to, _ := s.store.GetUserByUID(req.ToUID)
		_ = s.store.CreateNotification(&mysql.Notification{
			ID: s.genID(), UID: req.FromUID, Type: "friend", Title: "好友申请未通过",
			Summary: to.Nickname + " 拒绝了你的好友申请",
		})
		if s.notify != nil {
			s.notify(req.FromUID, "friend.rejected", ginMap("uid", req.ToUID))
		}
	}
	return nil
}

// DeleteFriend 删除好友。
func (s *Service) DeleteFriend(uid, friendUID int64) error {
	return s.store.DeleteFriend(uid, friendUID)
}

// SetFriendRemark 设置好友备注（空字符串清除）。仅好友可设置；防越权改他人关系行。
func (s *Service) SetFriendRemark(uid, friendUID int64, remark string) error {
	if uid == friendUID || friendUID <= 0 {
		return apperr.BadRequest("备注目标无效")
	}
	remark = strings.TrimSpace(remark)
	if len([]rune(remark)) > 32 {
		return apperr.BadRequest("备注过长（最多 32 字）")
	}
	if ok, err := s.store.AreFriends(uid, friendUID); err != nil || !ok {
		return apperr.Forbidden("对方不是你的好友")
	}
	if err := s.store.UpdateFriendRemark(uid, friendUID, remark); err != nil {
		return apperr.WrapInternal("更新备注失败", err)
	}
	return nil
}

// ---- 群组 ----

// GroupDTO 群信息。
type GroupDTO struct {
	GUID      int64    `json:"g_uid"`
	Name      string   `json:"name"`
	OwnerUID  int64    `json:"owner_uid"`
	Announcement string `json:"announcement,omitempty"`
	MemberCount int    `json:"member_count"`
	Avatar    string   `json:"avatar,omitempty"`
	Members   []int64  `json:"members,omitempty"`
	MyRole    int8     `json:"my_role"` // 请求者在群内的角色：0 群主 / 1 管理员 / 2 成员（仅 GetGroup 填充）
}

// CreateGroup 建群（邀请成员）。avatar 为群头像 URL，可为空。
func (s *Service) CreateGroup(ownerUID int64, name string, memberUIDs []int64, avatar string) (*GroupDTO, error) {
	if name == "" {
		return nil, apperr.BadRequest("群名不能为空")
	}
	if len([]rune(name)) > 20 {
		return nil, apperr.BadRequest("群名过长")
	}
	// 生成唯一群号
	var gUID int64
	for i := 0; i < 10; i++ {
		cand := randGUID()
		exists, _ := s.store.GUIDExists(cand)
		if !exists {
			gUID = cand
			break
		}
	}
	if gUID == 0 {
		return nil, apperr.Internal("生成群号失败")
	}
	convID := s.genID() // 统一群会话 ID（所有成员共享，供邀请新成员时创建同一会话）
	g := &mysql.Group{ID: s.genID(), GUID: gUID, Name: name, OwnerUID: ownerUID, MemberCount: 1, Avatar: avatar, ConvID: convID}
	if err := s.store.CreateGroup(g); err != nil {
		return nil, apperr.WrapInternal("创建群失败", err)
	}
	_ = s.store.AddGroupMember(gUID, ownerUID, 0) // 群主
	// 邀请成员
	for _, m := range memberUIDs {
		if m == ownerUID {
			continue
		}
		_ = s.store.AddGroupMember(gUID, m, 2)
		// 通知被邀请者
		_ = s.store.CreateNotification(&mysql.Notification{
			ID: s.genID(), UID: m, Type: "invite", Title: "群邀请", Summary: "你被邀请加入群「" + name + "」",
		})
		if s.notify != nil {
			s.notify(m, "group.invite", ginMap("g_uid", gUID, "group_name", name))
		}
	}
	// 群创建成功：为所有成员落库系统消息并实时推送（邀请语义，客户端按身份渲染文案）
	if s.groupSysMsg != nil {
		invited := excludeUID(memberUIDs, ownerUID)
		if len(invited) > 0 {
			content, extra := s.inviteSysMsg(ownerUID, invited, name)
			s.groupSysMsg(ownerUID, gUID, convID, content, extra, invited)
		} else {
			s.groupSysMsg(ownerUID, gUID, convID, "群聊「"+name+"」已创建", "", nil)
		}
	}
	members, _ := s.store.ListGroupMembers(gUID)
	return &GroupDTO{GUID: gUID, Name: name, OwnerUID: ownerUID, Avatar: avatar, MemberCount: len(members), Members: members}, nil
}

// InviteToGroup 邀请成员入群（需已是群成员）。
func (s *Service) InviteToGroup(operatorUID, gUID int64, memberUIDs []int64) error {
	ok, err := s.store.IsGroupMember(gUID, operatorUID)
	if err != nil || !ok {
		return apperr.Forbidden("不是群成员")
	}
	// 读取群统一会话 ID，用于为新成员创建同一会话视图
	g, err := s.store.GetGroupByGUID(gUID)
	if err != nil {
		return apperr.NotFound("群不存在")
	}
	convID := g.ConvID
	var invited []int64
	for _, m := range memberUIDs {
		if m == operatorUID {
			continue
		}
		_ = s.store.AddGroupMember(gUID, m, 2)
		invited = append(invited, m)
		_ = s.store.CreateNotification(&mysql.Notification{
			ID: s.genID(), UID: m, Type: "invite", Title: "群邀请", Summary: "你被邀请加入群「" + g.Name + "」",
		})
		if s.notify != nil {
			s.notify(m, "group.invite", ginMap("g_uid", gUID, "group_name", g.Name))
		}
	}
	// 邀请系统消息：推送给全体群成员（含老成员），客户端按身份渲染：
	// 被邀请者看到"你被邀请加入群聊…"，邀请者看到"你邀请了xx进入群聊"，其他成员看到"xx邀请了xx进入群聊"
	if len(invited) > 0 && s.groupSysMsg != nil {
		content, extra := s.inviteSysMsg(operatorUID, invited, g.Name)
		members, _ := s.store.ListGroupMembers(gUID)
		s.groupSysMsg(operatorUID, gUID, convID, content, extra, members)
	}
	return nil
}

// inviteSysMsg 构造邀请入群系统消息的共享文案与结构化扩展：
// content 为其他成员视角文案；extra 携带邀请人/被邀请人/群名，供客户端按查看者身份渲染。
func (s *Service) inviteSysMsg(inviterUID int64, inviteeUIDs []int64, groupName string) (string, string) {
	inviterName := s.Info(inviterUID)
	inviteeNames := make([]string, 0, len(inviteeUIDs))
	for _, u := range inviteeUIDs {
		inviteeNames = append(inviteeNames, s.Info(u))
	}
	content := inviterName + " 邀请了 " + strings.Join(inviteeNames, "、") + " 进入群聊"
	b, err := json.Marshal(map[string]interface{}{
		"kind":          "group_invite",
		"inviter_uid":   inviterUID,
		"inviter_name":  inviterName,
		"invitee_uids":  inviteeUIDs,
		"invitee_names": inviteeNames,
		"group_name":    groupName,
	})
	if err != nil {
		return content, ""
	}
	return content, string(b)
}

// excludeUID 从 uid 列表中排除指定 uid（建群时过滤群主自己）。
func excludeUID(uids []int64, exclude int64) []int64 {
	out := make([]int64, 0, len(uids))
	for _, u := range uids {
		if u != exclude {
			out = append(out, u)
		}
	}
	return out
}

// LeaveGroup 退群：校验成员身份 → 移除成员 → 删除退群者会话视图（清理）。
// 群主不能退群；"xx 退出群聊" 系统消息仅群主可见（落共享历史 + 仅推送群主，
// extra 携带 owner_uid，客户端非群主不渲染），其他成员看不到也收不到。
func (s *Service) LeaveGroup(uid, gUID int64) error {
	role, err := s.store.GetGroupMemberRole(gUID, uid)
	if err != nil {
		return apperr.WrapInternal("查询群角色失败", err)
	}
	if role < 0 {
		return apperr.Forbidden("你不是群成员，无需退群")
	}
	if role == 0 {
		return apperr.BadRequest("群主不能退出群聊")
	}
	g, err := s.store.GetGroupByGUID(gUID)
	if err != nil {
		return apperr.NotFound("群不存在")
	}
	if err := s.store.RemoveGroupMember(gUID, uid); err != nil {
		return apperr.WrapInternal("退出群聊失败", err)
	}
	// 清理：删除退群者的群会话视图（会话列表不再展示该群）
	_ = s.store.DeleteGroupConversationView(uid, gUID)
	// "xx 退出群聊" 系统消息：仅群主可见/可收（其他成员无推送；历史加载时客户端也按身份过滤）
	if s.groupSysMsgTo != nil && g.OwnerUID != uid {
		leaverName := s.Info(uid)
		if leaverName == "" {
			leaverName = "用户" + itoa(uid)
		}
		b, jerr := json.Marshal(map[string]interface{}{
			"kind":       "group_leave",
			"leaver_uid":  uid,
			"leaver_name": leaverName,
			"owner_uid":   g.OwnerUID,
		})
		extra := ""
		if jerr == nil {
			extra = string(b)
		}
		s.groupSysMsgTo(gUID, g.ConvID, leaverName+" 退出群聊", extra, []int64{g.OwnerUID})
	}
	// 通知退群者（其他设备）清理会话
	if s.notify != nil {
		s.notify(uid, "group.left", ginMap("g_uid", gUID, "conv_id", itoa(g.ConvID)))
	}
	return nil
}

// GetGroup 查询群信息：仅群成员可查（审计 P1：防任意用户枚举群信息与成员列表）。
func (s *Service) GetGroup(uid, gUID int64) (*GroupDTO, error) {
	if ok, err := s.store.IsGroupMember(gUID, uid); err != nil || !ok {
		return nil, apperr.Forbidden("你不是群成员，无法查看群信息")
	}
	g, err := s.store.GetGroupByGUID(gUID)
	if err != nil {
		return nil, apperr.NotFound("群不存在")
	}
	members, _ := s.store.ListGroupMembers(gUID)
	myRole, _ := s.store.GetGroupMemberRole(gUID, uid)
	return &GroupDTO{
		GUID: g.GUID, Name: g.Name, OwnerUID: g.OwnerUID,
		Announcement: g.Announcement, MemberCount: len(members), Avatar: g.Avatar, Members: members, MyRole: myRole,
	}, nil
}

// UpdateGroupInfo 修改群名/群公告：仅群主或管理员可操作（后端鉴权，防越权）。
// 成功后向全体群成员推送 group.updated 事件，客户端据此刷新群名/公告。
func (s *Service) UpdateGroupInfo(uid, gUID int64, name, announcement string) error {
	role, err := s.store.GetGroupMemberRole(gUID, uid)
	if err != nil {
		return apperr.WrapInternal("查询群角色失败", err)
	}
	if role < 0 {
		return apperr.Forbidden("你不是群成员，无法修改群资料")
	}
	if role > 1 {
		return apperr.Forbidden("仅群主或管理员可修改群资料")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return apperr.BadRequest("群名不能为空")
	}
	if len([]rune(name)) > 20 {
		return apperr.BadRequest("群名过长")
	}
	announcement = strings.TrimSpace(announcement)
	if len([]rune(announcement)) > 500 {
		return apperr.BadRequest("群公告过长")
	}
	if err := s.store.UpdateGroup(gUID, name, announcement); err != nil {
		return apperr.WrapInternal("修改群资料失败", err)
	}
	if s.notify != nil {
		members, _ := s.store.ListGroupMembers(gUID)
		for _, m := range members {
			s.notify(m, "group.updated", ginMap("g_uid", gUID, "name", name, "announcement", announcement))
		}
	}
	return nil
}

// ListUserGroups 我加入的群列表。
func (s *Service) ListUserGroups(uid int64) ([]*GroupDTO, error) {
	gUids, err := s.store.ListUserGroups(uid)
	if err != nil {
		return nil, apperr.WrapInternal("获取群列表失败", err)
	}
	list := make([]*GroupDTO, 0, len(gUids))
	for _, gUID := range gUids {
		if g, err := s.store.GetGroupByGUID(gUID); err == nil {
			members, _ := s.store.ListGroupMembers(gUID)
			list = append(list, &GroupDTO{GUID: g.GUID, Name: g.Name, OwnerUID: g.OwnerUID, Announcement: g.Announcement, MemberCount: len(members), Avatar: g.Avatar})
		}
	}
	return list, nil
}

// GetGroupMembers 群成员 uid 列表（供群聊多路分发）。
func (s *Service) GetGroupMembers(gUID int64) ([]int64, error) {
	return s.store.ListGroupMembers(gUID)
}

// ---- 通知 ----

// NotificationDTO 通知。ID 为雪花 ID，以字符串输出避免精度丢失。
type NotificationDTO struct {
	ID      int64  `json:"id,string"`
	Type    string `json:"type"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Action  string `json:"action,omitempty"`
	Read    int8   `json:"read"`
	Time    int64  `json:"time"`
}

// ListNotifications 通知列表。
func (s *Service) ListNotifications(uid int64) ([]*NotificationDTO, error) {
	list, err := s.store.ListNotifications(uid)
	if err != nil {
		return nil, apperr.WrapInternal("获取通知失败", err)
	}
	out := make([]*NotificationDTO, 0, len(list))
	for _, n := range list {
		out = append(out, &NotificationDTO{
			ID: n.ID, Type: n.Type, Title: n.Title, Summary: n.Summary,
			Action: n.Action, Read: n.Read, Time: n.CreatedAt.Unix(),
		})
	}
	return out, nil
}

// MarkRead 标记已读。
func (s *Service) MarkRead(uid, id int64, all bool) error {
	if all {
		return s.store.MarkAllNotificationsRead(uid)
	}
	return s.store.MarkNotificationRead(id, uid)
}

// UnreadCount 未读通知数。
func (s *Service) UnreadCount(uid int64) (int, error) {
	return s.store.CountUnreadNotifications(uid)
}

// Clear 清空通知。
func (s *Service) Clear(uid int64) error {
	return s.store.ClearNotifications(uid)
}

// ---- 工具 ----

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

func randGUID() int64 {
	// 10 位随机数字（1000000000 ~ 9999999999）
	return int64(genRand(1000000000, 9000000000))
}

// genRand 生成 [min, min+span) 的随机数（避免引入额外依赖）。
func genRand(min, span int64) int64 {
	return min + time.Now().UnixNano()%span
}

func ginMap(kv ...interface{}) map[string]interface{} {
	m := map[string]interface{}{}
	for i := 0; i+1 < len(kv); i += 2 {
		if k, ok := kv[i].(string); ok {
			m[k] = kv[i+1]
		}
	}
	return m
}
