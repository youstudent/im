// Package social 实现社交领域：好友、群组、通知。
package social

import (
	"encoding/json"
	"math/rand"
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
	HasPendingFriendRequest(fromUID, toUID int64) (bool, error)
	// 群
	CreateGroup(g *mysql.Group) error
	GetGroupByGUID(gUID int64) (*mysql.Group, error)
	AddGroupMember(gUID, uid int64, role int8) error
	RemoveGroupMember(gUID, uid int64) error
	IsGroupMember(gUID, uid int64) (bool, error)
	GetGroupMemberRole(gUID, uid int64) (int8, error)
	ListGroupMembers(gUID int64) ([]int64, error)
	// 群成员角色管理（设/撤管理员）与批量角色查询（群资料页标签展示）
	UpdateGroupMemberRole(gUID, uid int64, role int8) error
	ListGroupMemberRoles(gUID int64) (map[int64]int8, error)
	// 转让群主与群内昵称（第二期）
	TransferGroupOwner(gUID, oldOwnerUID, newOwnerUID int64) error
	UpdateGroupMemberNickname(gUID, uid int64, nickname string) error
	ListGroupMemberNicknames(gUID int64) (map[int64]string, error)
	// 第三期（P2）：群设置（入群确认/全员禁言）、成员禁言、保存到通讯录
	UpdateGroupSettings(gUID int64, inviteConfirm, muteAll *int8) error
	GetGroupMuteState(gUID int64) (int8, error)
	UpdateMemberMutedUntil(gUID, uid int64, until int64) error
	ListGroupMemberMutes(gUID int64) (map[int64]int64, error)
	UpdateGroupMemberSaved(gUID, uid int64, saved int8) error
	ListGroupMemberSaved(uid int64, gUIDs []int64) (map[int64]int8, error)
	// P1 性能优化：好友/群列表批量查，消除逐条 N+1
	GetUsersByUIDs(uids []int64) map[int64]*mysql.User
	GetGroupsByGUIDs(gUIDs []int64) map[int64]*mysql.Group
	GroupMemberCounts(gUIDs []int64) map[int64]int
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

// UserSearchDTO 搜索结果（附带与当前用户的关系状态，供前端按钮置灰展示）。
type UserSearchDTO struct {
	*FriendDTO
	IsFriend    bool `json:"is_friend"`    // 是否已是好友
	RequestSent bool `json:"request_sent"` // 我是否已发送待处理的好友申请
}

// SearchUser 按手机号/邮箱/昵称搜索用户（用于加好友），返回用户 uid 信息，
// 并附带与调用者（callerUID）的关系状态：已是好友 / 已发送待处理申请。
func (s *Service) SearchUser(callerUID int64, account string) (*UserSearchDTO, error) {
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
	dto := &UserSearchDTO{
		FriendDTO: &FriendDTO{UID: u.UID, Nickname: u.Nickname, Avatar: u.Avatar, Remark: u.Account},
	}
	// 关系状态查询失败不阻断搜索（发送申请接口仍会兜底拦截）
	if callerUID > 0 && callerUID != u.UID {
		if ok, aerr := s.store.AreFriends(callerUID, u.UID); aerr == nil {
			dto.IsFriend = ok
		}
		if !dto.IsFriend {
			if sent, perr := s.store.HasPendingFriendRequest(callerUID, u.UID); perr == nil {
				dto.RequestSent = sent
			}
		}
	}
	return dto, nil
}

// ListFriends 好友列表（P1 优化：昵称/头像一次批量查，消除逐好友 GetUserByUID 的 N+1）。
func (s *Service) ListFriends(uid int64) ([]*FriendDTO, error) {
	friends, err := s.store.ListFriends(uid)
	if err != nil {
		return nil, apperr.WrapInternal("获取好友列表失败", err)
	}
	uids := make([]int64, 0, len(friends))
	for _, f := range friends {
		uids = append(uids, f.FriendUID)
	}
	users := s.store.GetUsersByUIDs(uids)
	list := make([]*FriendDTO, 0, len(friends))
	for _, f := range friends {
		u := users[f.FriendUID]
		if u == nil {
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

// friendReqMsgMaxRunes 好友申请验证消息长度上限（库列 VARCHAR(255)，业务层收紧到 100 字，
// 与验证消息场景对齐，防超大字符串冲刷申请/通知表）。
const friendReqMsgMaxRunes = 100

// SendFriendRequest 发送好友申请（不重复申请，写通知给接收方）。
// 拦截：不能加自己、对方已是好友、目标用户不存在、验证消息超长。
func (s *Service) SendFriendRequest(fromUID, toUID int64, message string) error {
	if fromUID == toUID {
		return apperr.BadRequest("不能添加自己为好友")
	}
	if len([]rune(strings.TrimSpace(message))) > friendReqMsgMaxRunes {
		return apperr.BadRequest("验证消息过长（最多 100 字）")
	}
	// 目标用户必须存在，防止对任意 uid 创建申请/通知
	if _, err := s.store.GetUserByUID(toUID); err != nil {
		return apperr.BadRequest("目标用户不存在")
	}
	// 已是好友直接拦截，不再创建申请
	if ok, err := s.store.AreFriends(fromUID, toUID); err == nil && ok {
		return apperr.BadRequest("对方已是你的好友，无需重复添加")
	}
	// 防重复申请（审计 P0）：已有待处理申请时拦截，防止反复刷通知骚扰对方
	if pendings, err := s.store.ListFriendRequests(toUID); err == nil {
		for _, r := range pendings {
			if r.FromUID == fromUID {
				return apperr.BadRequest("好友申请已发送，请等待对方处理")
			}
		}
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
		// req_id/nickname 供客户端实时弹框展示与直接同意/拒绝（无需再查申请列表）
		s.notify(toUID, "friend.request", ginMap("req_id", itoa(id), "from_uid", fromUID, "nickname", from.Nickname, "message", message))
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

// AreFriends 判断两用户是否为好友（通话信令转发前鉴权用，防止对任意用户发起呼叫骚扰）。
func (s *Service) AreFriends(uid, friendUID int64) bool {
	ok, err := s.store.AreFriends(uid, friendUID)
	return err == nil && ok
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
	MemberRoles map[int64]int8 `json:"member_roles,omitempty"` // 成员角色映射（uid → 0 群主/1 管理员/2 成员，仅 GetGroup 填充）
	MemberNicknames map[int64]string `json:"member_nicknames,omitempty"` // 群内昵称映射（uid → 昵称，仅已设置的，仅 GetGroup 填充）
	MemberMutes map[int64]int64 `json:"member_mutes,omitempty"` // 成员禁言截止映射（uid → unix 毫秒，0=未禁言，仅 GetGroup 填充）
	MyNickname string  `json:"my_nickname,omitempty"` // 请求者的群内昵称（未设置为空，仅 GetGroup 填充）
	MyRole    int8     `json:"my_role"` // 请求者在群内的角色：0 群主 / 1 管理员 / 2 成员（仅 GetGroup 填充）
	InviteConfirm int8  `json:"invite_confirm"` // 邀请需确认（G7）
	MuteAll       int8  `json:"mute_all"`       // 全员禁言（G8）
	MyMutedUntil  int64 `json:"my_muted_until,omitempty"` // 我的禁言截止（unix 毫秒，0=未禁言，仅 GetGroup 填充）
	Saved         int8  `json:"saved"` // 我的"保存到通讯录"开关（G10，默认 1）
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
	// 邀请成员（审计 P0：逐个校验目标用户存在、去重、受群人数上限约束，防对任意 uid 强拉入群骚扰）
	memberUIDs = filterInvitees(memberUIDs, ownerUID, nil, s.store)
	if len(memberUIDs) >= maxGroupMembers {
		return nil, apperr.BadRequest("邀请人数超过群上限")
	}
	for _, m := range memberUIDs {
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
// G7 入群确认：群开启 invite_confirm 时邀请不直接入群，改为向群主/管理员发待确认通知
// （notifications type=group_invite_confirm + WS group.invite_pending），同意后由 DecideInvite 入群。
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
	// 审计 P0：过滤无效邀请目标（不存在/已在群/重复），并校验群人数上限
	existing, _ := s.store.ListGroupMembers(gUID)
	memberUIDs = filterInvitees(memberUIDs, operatorUID, existing, s.store)
	if len(memberUIDs) == 0 {
		return nil
	}
	if len(existing)+len(memberUIDs) > maxGroupMembers {
		return apperr.BadRequest("群成员已达上限，无法继续邀请")
	}
	// G7 入群确认：不直接入群，向群主/管理员发送待确认通知（不落系统消息、不建会话视图）
	if g.InviteConfirm == 1 {
		if err := s.notifyInvitePending(g, operatorUID, memberUIDs); err != nil {
			return err
		}
		return apperr.Conflict("该群开启了入群确认，已通知群主/管理员处理")
	}
	convID := g.ConvID
	s.addInvitedMembers(operatorUID, g, convID, memberUIDs)
	return nil
}

// notifyInvitePending 入群确认（G7）：向群主/管理员写通知并推 WS 事件，
// 等待其同意/拒绝（action 携带群号/邀请人/被邀请人，供通知中心直接操作）。
func (s *Service) notifyInvitePending(g *mysql.Group, inviterUID int64, inviteeUIDs []int64) error {
	if len(inviteeUIDs) == 0 {
		return nil
	}
	// 获取群主/管理员列表（去重：群主可能同时出现在管理员集合）
	adminSet := map[int64]struct{}{}
	roles, err := s.store.ListGroupMemberRoles(g.GUID)
	if err != nil {
		return apperr.WrapInternal("查询群角色失败", err)
	}
	for uid, role := range roles {
		if role <= 1 {
			adminSet[uid] = struct{}{}
		}
	}
	if len(adminSet) == 0 {
		return nil
	}
	inviterName := s.Info(inviterUID)
	if inviterName == "" {
		inviterName = "用户" + itoa(inviterUID)
	}
	inviteeNames := make([]string, 0, len(inviteeUIDs))
	for _, u := range inviteeUIDs {
		n := s.Info(u)
		if n == "" {
			n = "用户" + itoa(u)
		}
		inviteeNames = append(inviteeNames, n)
	}
	action, jerr := json.Marshal(map[string]interface{}{
		"g_uid":        itoa(g.GUID),
		"group_name":   g.Name,
		"inviter_uid":  inviterUID,
		"inviter_name": inviterName,
		"invitee_uids": inviteeUIDs,
	})
	if jerr != nil {
		action = nil
	}
	actionStr := ""
	if action != nil {
		actionStr = string(action)
	}
	summary := inviterName + " 邀请 " + strings.Join(inviteeNames, "、") + " 加入群「" + g.Name + "」，请确认"
	for uid := range adminSet {
		_ = s.store.CreateNotification(&mysql.Notification{
			ID: s.genID(), UID: uid, Type: "group_invite_confirm",
			Title: "入群申请待确认", Summary: summary, Action: actionStr,
		})
		if s.notify != nil {
			s.notify(uid, "group.invite_pending", ginMap("g_uid", g.GUID, "group_name", g.Name,
				"inviter_uid", inviterUID, "inviter_name", inviterName, "invitee_uids", inviteeUIDs))
		}
	}
	return nil
}

// addInvitedMembers 执行邀请入群落地：加成员行/会话视图/系统消息/通知被邀请者（建群与入群确认同意共用）。
func (s *Service) addInvitedMembers(operatorUID int64, g *mysql.Group, convID int64, memberUIDs []int64) {
	var invited []int64
	for _, m := range memberUIDs {
		_ = s.store.AddGroupMember(g.GUID, m, 2)
		invited = append(invited, m)
		_ = s.store.CreateNotification(&mysql.Notification{
			ID: s.genID(), UID: m, Type: "invite", Title: "群邀请", Summary: "你被邀请加入群「" + g.Name + "」",
		})
		if s.notify != nil {
			s.notify(m, "group.invite", ginMap("g_uid", g.GUID, "group_name", g.Name))
		}
	}
	// 邀请系统消息：推送给全体群成员（含老成员），客户端按身份渲染
	if len(invited) > 0 && s.groupSysMsg != nil {
		content, extra := s.inviteSysMsg(operatorUID, invited, g.Name)
		members, _ := s.store.ListGroupMembers(g.GUID)
		s.groupSysMsg(operatorUID, g.GUID, convID, content, extra, members)
	}
}

// DecideInvite 处理入群确认（G7）：仅群主/管理员可操作。
// accept=true 走现有邀请落地链路（成员入群 + 系统消息 + 会话视图）；
// accept=false 通知邀请者"入群申请被拒绝"；已入群者幂等跳过。
func (s *Service) DecideInvite(deciderUID, gUID, inviteeUID int64, accept bool) error {
	role, err := s.store.GetGroupMemberRole(gUID, deciderUID)
	if err != nil {
		return apperr.WrapInternal("查询群角色失败", err)
	}
	if role < 0 || role > 1 {
		return apperr.Forbidden("仅群主或管理员可处理入群申请")
	}
	g, err := s.store.GetGroupByGUID(gUID)
	if err != nil {
		return apperr.NotFound("群不存在")
	}
	// 已在群：幂等返回（客户端重复点击同意场景）
	if in, ierr := s.store.IsGroupMember(gUID, inviteeUID); ierr == nil && in {
		return nil
	}
	inviteeName := s.Info(inviteeUID)
	if inviteeName == "" {
		inviteeName = "用户" + itoa(inviteeUID)
	}
	if !accept {
		// 拒绝：通知邀请者（type=system，无需定向到具体邀请人，摘要已含申请人）
		_ = s.store.CreateNotification(&mysql.Notification{
			ID: s.genID(), UID: deciderUID, Type: "system",
			Title: "入群申请已拒绝", Summary: "你已拒绝 " + inviteeName + " 加入群「" + g.Name + "」",
		})
		return nil
	}
	// 同意：目标必须是真实用户（防对任意 uid 拉入群），且群未达上限
	if _, err := s.store.GetUserByUID(inviteeUID); err != nil {
		return apperr.BadRequest("目标用户不存在")
	}
	existing, _ := s.store.ListGroupMembers(gUID)
	if len(existing)+1 > maxGroupMembers {
		return apperr.BadRequest("群成员已达上限")
	}
	s.addInvitedMembers(deciderUID, g, g.ConvID, []int64{inviteeUID})
	// 通知被拒绝/同意方（邀请者视角）：通知中心可见入群成功
	_ = s.store.CreateNotification(&mysql.Notification{
		ID: s.genID(), UID: deciderUID, Type: "system",
		Title: "入群申请已通过", Summary: inviteeName + " 已加入群「" + g.Name + "」",
	})
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

// maxGroupMembers 单群成员上限（含群主）。
const maxGroupMembers = 500

// filterInvitees 过滤邀请名单（审计 P0）：去掉操作者自己、已在群成员、不存在的用户。
// existing 为当前群成员列表（建群时传 nil）；人数上限由调用方校验。
func filterInvitees(uids []int64, operatorUID int64, existing []int64, store Store) []int64 {
	seen := make(map[int64]struct{}, len(existing)+len(uids))
	seen[operatorUID] = struct{}{}
	for _, u := range existing {
		seen[u] = struct{}{}
	}
	out := make([]int64, 0, len(uids))
	for _, m := range uids {
		if m <= 0 {
			continue
		}
		if _, dup := seen[m]; dup {
			continue
		}
		// 目标用户必须存在，防止对任意 uid 强拉入群并刷邀请通知
		if _, err := store.GetUserByUID(m); err != nil {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
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
	leaverName := s.Info(uid)
	if leaverName == "" {
		leaverName = "用户" + itoa(uid)
	}
	// "xx 退出群聊" 系统消息：仅群主可见/可收（其他成员无推送；历史加载时客户端也按身份过滤）
	if s.groupSysMsgTo != nil && g.OwnerUID != uid {
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
	// 通知群内其他成员：成员列表变化（有人退群），客户端刷新群资料/成员列表/成员数
	if s.notify != nil {
		members, _ := s.store.ListGroupMembers(gUID) // 已移除退群者
		for _, m := range members {
			s.notify(m, "group.member_left", ginMap("g_uid", gUID, "uid", itoa(uid), "name", leaverName))
		}
	}
	return nil
}

// RemoveMember 移除群成员（微信规则）：
//   - 群主可移除除自己外的任何成员；管理员仅可移除普通成员（不可动群主与其他管理员）；
//   - 不能移除自己（退群走 LeaveGroup）；
//   - 移除后：删成员行/会话视图 → 剩余成员共享历史落“移出”系统消息 → 被移除者推送 group.kicked 事件清理会话。
func (s *Service) RemoveMember(operatorUID, gUID, targetUID int64) error {
	if operatorUID == targetUID {
		return apperr.BadRequest("不能移除自己，退出群聊请使用退群功能")
	}
	opRole, err := s.store.GetGroupMemberRole(gUID, operatorUID)
	if err != nil {
		return apperr.WrapInternal("查询群角色失败", err)
	}
	if opRole < 0 || opRole > 1 {
		return apperr.Forbidden("仅群主或管理员可移除群成员")
	}
	targetRole, err := s.store.GetGroupMemberRole(gUID, targetUID)
	if err != nil {
		return apperr.WrapInternal("查询群角色失败", err)
	}
	if targetRole < 0 {
		return apperr.NotFound("该用户不是群成员")
	}
	// 管理员不能移除群主或其他管理员（群主可移除任何人）
	if opRole == 1 && targetRole <= 1 {
		return apperr.Forbidden("管理员仅可移除普通成员")
	}
	g, err := s.store.GetGroupByGUID(gUID)
	if err != nil {
		return apperr.NotFound("群不存在")
	}
	if err := s.store.RemoveGroupMember(gUID, targetUID); err != nil {
		return apperr.WrapInternal("移除群成员失败", err)
	}
	// 清理被移除者的群会话视图（会话列表不再展示该群）
	_ = s.store.DeleteGroupConversationView(targetUID, gUID)
	opName := s.Info(operatorUID)
	if opName == "" {
		opName = "用户" + itoa(operatorUID)
	}
	targetName := s.Info(targetUID)
	if targetName == "" {
		targetName = "用户" + itoa(targetUID)
	}
	// “xx 将 yy 移出了群聊”：落共享历史并推送给剩余全体成员（移除后 ListGroupMembers 已不含被移除者）
	if s.groupSysMsgTo != nil {
		b, jerr := json.Marshal(map[string]interface{}{
			"kind":         "group_kick",
			"operator_uid":  operatorUID,
			"operator_name": opName,
			"target_uid":    targetUID,
			"target_name":   targetName,
		})
		extra := ""
		if jerr == nil {
			extra = string(b)
		}
		members, _ := s.store.ListGroupMembers(gUID)
		if len(members) > 0 {
			s.groupSysMsgTo(gUID, g.ConvID, opName+" 将 "+targetName+" 移出了群聊", extra, members)
		}
	}
	// 通知被移除者：通知中心 + WS 事件（客户端据此清理会话与消息）
	_ = s.store.CreateNotification(&mysql.Notification{
		ID: s.genID(), UID: targetUID, Type: "kick", Title: "被移出群聊",
		Summary: "你被 " + opName + " 移出了群「" + g.Name + "」",
	})
	if s.notify != nil {
		s.notify(targetUID, "group.kicked", ginMap("g_uid", gUID, "conv_id", itoa(g.ConvID), "operator_name", opName))
	}
	return nil
}

// SetMemberRole 设为/取消管理员：仅群主可操作；目标必须是除群主外的群成员，role 仅允许 1/2。
// 成功后向全体群成员推送 group.role_changed 事件，客户端据此刷新成员角色标签。
func (s *Service) SetMemberRole(operatorUID, gUID, targetUID int64, role int8) error {
	if role != 1 && role != 2 {
		return apperr.BadRequest("角色值无效")
	}
	if operatorUID == targetUID {
		return apperr.BadRequest("不能修改自己的角色")
	}
	opRole, err := s.store.GetGroupMemberRole(gUID, operatorUID)
	if err != nil {
		return apperr.WrapInternal("查询群角色失败", err)
	}
	if opRole != 0 {
		return apperr.Forbidden("仅群主可设置管理员")
	}
	targetRole, err := s.store.GetGroupMemberRole(gUID, targetUID)
	if err != nil {
		return apperr.WrapInternal("查询群角色失败", err)
	}
	if targetRole < 0 {
		return apperr.NotFound("该用户不是群成员")
	}
	if targetRole == 0 {
		return apperr.BadRequest("不能修改群主角色")
	}
	if targetRole == role {
		return nil // 幂等：角色未变化
	}
	if err := s.store.UpdateGroupMemberRole(gUID, targetUID, role); err != nil {
		return apperr.WrapInternal("修改群成员角色失败", err)
	}
	if s.notify != nil {
		members, _ := s.store.ListGroupMembers(gUID)
		for _, m := range members {
			s.notify(m, "group.role_changed", ginMap("g_uid", gUID, "uid", itoa(targetUID), "role", role))
		}
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
	roles, _ := s.store.ListGroupMemberRoles(gUID)
	nicks, _ := s.store.ListGroupMemberNicknames(gUID)
	mutes, _ := s.store.ListGroupMemberMutes(gUID)
	saved := int8(1)
	if savedMap, err := s.store.ListGroupMemberSaved(uid, []int64{gUID}); err == nil {
		if v, ok := savedMap[gUID]; ok {
			saved = v
		}
	}
	return &GroupDTO{
		GUID: g.GUID, Name: g.Name, OwnerUID: g.OwnerUID,
		Announcement: g.Announcement, MemberCount: len(members), Avatar: g.Avatar, Members: members,
		MemberRoles: roles, MemberNicknames: nicks, MemberMutes: mutes, MyNickname: nicks[uid], MyRole: myRole,
		InviteConfirm: g.InviteConfirm, MuteAll: g.MuteAll, MyMutedUntil: mutes[uid], Saved: saved,
	}, nil
}

// TransferOwnership 转让群主（微信规则）：仅现任群主可操作，目标必须是除自己外的群成员；
// 转让后原群主自动变普通成员；全体群成员推送 group.owner_changed 事件 + 共享历史落系统消息。
func (s *Service) TransferOwnership(ownerUID, gUID, newOwnerUID int64) error {
	if ownerUID == newOwnerUID {
		return apperr.BadRequest("不能转让给自己")
	}
	opRole, err := s.store.GetGroupMemberRole(gUID, ownerUID)
	if err != nil {
		return apperr.WrapInternal("查询群角色失败", err)
	}
	if opRole != 0 {
		return apperr.Forbidden("仅群主可转让群主")
	}
	g, err := s.store.GetGroupByGUID(gUID)
	if err != nil {
		return apperr.NotFound("群不存在")
	}
	if g.OwnerUID != ownerUID {
		return apperr.Forbidden("仅现任群主可转让群主")
	}
	newRole, err := s.store.GetGroupMemberRole(gUID, newOwnerUID)
	if err != nil {
		return apperr.WrapInternal("查询群角色失败", err)
	}
	if newRole < 0 {
		return apperr.NotFound("目标不是群成员")
	}
	if err := s.store.TransferGroupOwner(gUID, ownerUID, newOwnerUID); err != nil {
		return apperr.WrapInternal("转让群主失败", err)
	}
	newName := s.Info(newOwnerUID)
	if newName == "" {
		newName = "用户" + itoa(newOwnerUID)
	}
	// “xx 成为新群主”：落共享历史并推送全体成员
	if s.groupSysMsg != nil {
		b, jerr := json.Marshal(map[string]interface{}{
			"kind":      "group_transfer",
			"owner_uid":  newOwnerUID,
			"owner_name": newName,
		})
		extra := ""
		if jerr == nil {
			extra = string(b)
		}
		members, _ := s.store.ListGroupMembers(gUID)
		s.groupSysMsg(newOwnerUID, gUID, g.ConvID, newName+" 成为新群主", extra, members)
	}
	// 事件推送：客户端刷新 myRole/ownerUid（原群主权限即时收回）
	if s.notify != nil {
		members, _ := s.store.ListGroupMembers(gUID)
		for _, m := range members {
			s.notify(m, "group.owner_changed", ginMap("g_uid", gUID, "owner_uid", itoa(newOwnerUID)))
		}
	}
	return nil
}

// SetMyNickname 设置我的群内昵称（任何成员均可；空字符串清除回落用户昵称，上限 32 字符）。
// 成功后向全体群成员推送 group.nickname_changed，客户端刷新发送者展示名。
func (s *Service) SetMyNickname(uid, gUID int64, nickname string) error {
	if ok, err := s.store.IsGroupMember(gUID, uid); err != nil || !ok {
		return apperr.Forbidden("你不是群成员")
	}
	nickname = strings.TrimSpace(nickname)
	if len([]rune(nickname)) > 32 {
		return apperr.BadRequest("群昵称过长（上限 32 字）")
	}
	if err := s.store.UpdateGroupMemberNickname(gUID, uid, nickname); err != nil {
		return apperr.WrapInternal("设置群昵称失败", err)
	}
	if s.notify != nil {
		members, _ := s.store.ListGroupMembers(gUID)
		for _, m := range members {
			s.notify(m, "group.nickname_changed", ginMap("g_uid", gUID, "uid", itoa(uid), "nickname", nickname))
		}
	}
	return nil
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
	// 变更前的公告（用于判定是否需要强提醒推送：仅公告实际变化时推 group.announcement）
	oldAnnouncement := ""
	if old, err := s.store.GetGroupByGUID(gUID); err == nil && old != nil {
		oldAnnouncement = old.Announcement
	}
	if err := s.store.UpdateGroup(gUID, name, announcement); err != nil {
		return apperr.WrapInternal("修改群资料失败", err)
	}
	if s.notify != nil {
		members, _ := s.store.ListGroupMembers(gUID)
		for _, m := range members {
			s.notify(m, "group.updated", ginMap("g_uid", gUID, "name", name, "announcement", announcement))
		}
		// 公告强提醒（G11）：公告实际变化时推专用事件，客户端打开该群会话时顶部横幅展示；
		// 操作者自身不推（自己刚编辑完无需再提醒）
		if announcement != oldAnnouncement {
			for _, m := range members {
				if m == uid {
					continue
				}
				s.notify(m, "group.announcement", ginMap("g_uid", gUID, "announcement", announcement, "operator_uid", uid))
			}
		}
	}
	return nil
}

// UpdateGroupSettings 更新群设置开关（G7 入群确认 / G8 全员禁言）：仅群主或管理员可操作。
// 全员禁言开启（0→1）时落系统消息"群主开启了全员禁言"并推送全体成员（发送守卫据此拦截普通成员）。
func (s *Service) UpdateGroupSettings(uid, gUID int64, inviteConfirm, muteAll *int8) error {
	role, err := s.store.GetGroupMemberRole(gUID, uid)
	if err != nil {
		return apperr.WrapInternal("查询群角色失败", err)
	}
	if role < 0 || role > 1 {
		return apperr.Forbidden("仅群主或管理员可修改群设置")
	}
	if inviteConfirm == nil && muteAll == nil {
		return apperr.BadRequest("没有要更新的设置项")
	}
	for _, v := range []*int8{inviteConfirm, muteAll} {
		if v != nil && *v != 0 && *v != 1 {
			return apperr.BadRequest("设置值无效")
		}
	}
	g, err := s.store.GetGroupByGUID(gUID)
	if err != nil {
		return apperr.NotFound("群不存在")
	}
	if err := s.store.UpdateGroupSettings(gUID, inviteConfirm, muteAll); err != nil {
		return apperr.WrapInternal("更新群设置失败", err)
	}
	// 全员禁言从 0→1：落系统消息并推送全体成员（禁言生效感知）；1→0 同样提示恢复
	if muteAll != nil && g.MuteAll != *muteAll {
		opName := s.Info(uid)
		if opName == "" {
			opName = "用户" + itoa(uid)
		}
		content := "群主开启了全员禁言"
		if *muteAll == 0 {
			content = "群主已解除全员禁言"
		}
		b, jerr := json.Marshal(map[string]interface{}{"kind": "group_mute_all", "operator_uid": uid, "operator_name": opName, "mute_all": *muteAll})
		extra := ""
		if jerr == nil {
			extra = string(b)
		}
		if s.groupSysMsg != nil {
			members, _ := s.store.ListGroupMembers(gUID)
			s.groupSysMsg(uid, gUID, g.ConvID, content, extra, members)
		}
	}
	// 群资料变更：推 group.updated（客户端刷新资料面板开关状态）
	if s.notify != nil {
		members, _ := s.store.ListGroupMembers(gUID)
		for _, m := range members {
			s.notify(m, "group.updated", ginMap("g_uid", gUID, "invite_confirm", inviteConfirm, "mute_all", muteAll))
		}
	}
	return nil
}

// SetMemberMutedUntil 设置/解除成员禁言（G8）：仅群主或管理员可操作，不能禁言群主。
// until 为 unix 毫秒；0 表示解除。成功推 group.muted 事件供客户端刷新成员标签。
func (s *Service) SetMemberMutedUntil(operatorUID, gUID, targetUID, until int64) error {
	if targetUID <= 0 {
		return apperr.BadRequest("目标成员无效")
	}
	opRole, err := s.store.GetGroupMemberRole(gUID, operatorUID)
	if err != nil {
		return apperr.WrapInternal("查询群角色失败", err)
	}
	if opRole < 0 || opRole > 1 {
		return apperr.Forbidden("仅群主或管理员可禁言成员")
	}
	targetRole, err := s.store.GetGroupMemberRole(gUID, targetUID)
	if err != nil {
		return apperr.WrapInternal("查询群角色失败", err)
	}
	if targetRole < 0 {
		return apperr.NotFound("该用户不是群成员")
	}
	// 管理员不能禁言群主或其他管理员（对齐移除成员规则）
	if opRole == 1 && targetRole <= 1 {
		return apperr.Forbidden("管理员仅可禁言普通成员")
	}
	if err := s.store.UpdateMemberMutedUntil(gUID, targetUID, until); err != nil {
		return apperr.WrapInternal("设置禁言失败", err)
	}
	if s.notify != nil {
		members, _ := s.store.ListGroupMembers(gUID)
		for _, m := range members {
			s.notify(m, "group.muted", ginMap("g_uid", gUID, "uid", itoa(targetUID), "until", until))
		}
	}
	return nil
}

// GroupMuteCheck 群禁言发送守卫（G8，由消息服务注入调用）：
//   - 全员禁言：仅群主/管理员可发言，其余成员禁言；
//   - 个人禁言：muted_until > now 时禁言（个人禁言不豁免管理员，微信规则）。
// 返回 (muted, reason)。
func (s *Service) GroupMuteCheck(gUID, uid int64) (bool, string) {
	role, err := s.store.GetGroupMemberRole(gUID, uid)
	if err != nil || role < 0 {
		return true, "你不是群成员，无法发送消息"
	}
	muteAll, err := s.store.GetGroupMuteState(gUID)
	if err == nil && muteAll == 1 && role > 1 {
		return true, "群主开启了全员禁言，仅群主/管理员可发言"
	}
	mutes, err := s.store.ListGroupMemberMutes(gUID)
	if err != nil {
		return false, ""
	}
	if until := mutes[uid]; until > time.Now().UnixMilli() {
		return true, "你已被禁言，暂时无法发言"
	}
	return false, ""
}

// UpdateGroupSaved 更新我"保存到通讯录"开关（G10）：任何群成员可操作自己的行。
func (s *Service) UpdateGroupSaved(uid, gUID int64, saved int8) error {
	if saved != 0 && saved != 1 {
		return apperr.BadRequest("设置值无效")
	}
	if ok, err := s.store.IsGroupMember(gUID, uid); err != nil || !ok {
		return apperr.Forbidden("你不是群成员")
	}
	if err := s.store.UpdateGroupMemberSaved(gUID, uid, saved); err != nil {
		return apperr.WrapInternal("更新群设置失败", err)
	}
	return nil
}

// ListUserGroups 我加入的群列表（P1 优化：群信息与成员数各一次批量查，
// 消除逐群 GetGroupByGUID + ListGroupMembers 的 2N 次查询）。
func (s *Service) ListUserGroups(uid int64) ([]*GroupDTO, error) {
	gUids, err := s.store.ListUserGroups(uid)
	if err != nil {
		return nil, apperr.WrapInternal("获取群列表失败", err)
	}
	groups := s.store.GetGroupsByGUIDs(gUids)
	counts := s.store.GroupMemberCounts(gUids)
	savedMap, _ := s.store.ListGroupMemberSaved(uid, gUids)
	list := make([]*GroupDTO, 0, len(gUids))
	for _, gUID := range gUids {
		g := groups[gUID]
		if g == nil {
			continue
		}
		saved := int8(1)
		if v, ok := savedMap[gUID]; ok {
			saved = v
		}
		list = append(list, &GroupDTO{GUID: g.GUID, Name: g.Name, OwnerUID: g.OwnerUID, Announcement: g.Announcement, MemberCount: counts[gUID], Avatar: g.Avatar, InviteConfirm: g.InviteConfirm, MuteAll: g.MuteAll, Saved: saved})
	}
	return list, nil
}

// GetGroupMembers 群成员 uid 列表（供群聊多路分发）。
func (s *Service) GetGroupMembers(gUID int64) ([]int64, error) {
	return s.store.ListGroupMembers(gUID)
}

// GetMemberRole 群成员角色查询（G2 @所有人鉴权 / G14 已读人数权限）：0 群主 / 1 管理员 / 2 成员 / -1 非成员。
func (s *Service) GetMemberRole(gUID, uid int64) (int8, error) {
	return s.store.GetGroupMemberRole(gUID, uid)
}

// NotifyMentioned 为被 @ 成员写通知中心条目（群消息落库后由消息服务回调）。
// 摘要格式：「群名」发送者：内容摘要；action 携带 conv_id/msg_id/g_uid（字符串防精度丢失），
// 供客户端后续点击定位原消息。写失败静默（通知为增强能力，不回滚发送）。
func (s *Service) NotifyMentioned(gUID, convID, msgID, senderUID int64, senderName string, mentionedUIDs []int64, preview string) {
	g, err := s.store.GetGroupByGUID(gUID)
	if err != nil {
		return
	}
	if senderName == "" {
		senderName = "有人"
	}
	if r := []rune(preview); len(r) > 50 {
		preview = string(r[:50]) + "…"
	}
	for _, uid := range mentionedUIDs {
		if uid <= 0 || uid == senderUID {
			continue
		}
		_ = s.store.CreateNotification(&mysql.Notification{
			ID: s.genID(), UID: uid, Type: "mention",
			Title: "有人@了你",
			Summary: "「" + g.Name + "」" + senderName + "：" + preview,
			Action:  `{"conv_id":"` + itoa(convID) + `","msg_id":"` + itoa(msgID) + `","g_uid":"` + itoa(gUID) + `"}`,
		})
	}
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

// genRand 生成 [min, min+span) 的随机数（math/rand 全局源已自动播种，不可用纳秒取模预测）。
func genRand(min, span int64) int64 {
	return min + rand.Int63n(span)
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
