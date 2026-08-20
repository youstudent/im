// handler.go：社交模块 HTTP 处理器（好友 / 群组 / 通知）。
package social

import (
	"strconv"

	"github.com/gin-gonic/gin"

	apperr "im/service/internal/pkg/err"
	"im/service/internal/pkg/resp"
	"im/service/internal/server/middleware"
)

// Handler 社交 HTTP 处理器。
type Handler struct {
	svc *Service
}

// NewHandler 创建社交处理器。
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ---- 好友 ----

// SearchUser 按手机号/邮箱/昵称搜索用户（用于加好友），附带与当前用户的关系状态。
func (h *Handler) SearchUser(c *gin.Context) {
	account := c.Query("account")
	u, err := h.svc.SearchUser(mustUID(c), account)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, u)
}

// ListFriends 好友列表。
func (h *Handler) ListFriends(c *gin.Context) {
	uid := mustUID(c)
	list, err := h.svc.ListFriends(uid)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, list)
}

// ListFriendRequests 我收到的待处理好友申请。
func (h *Handler) ListFriendRequests(c *gin.Context) {
	uid := mustUID(c)
	list, err := h.svc.ListFriendRequests(uid)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, list)
}

// SendFriendRequest 发送好友申请。
func (h *Handler) SendFriendRequest(c *gin.Context) {
	uid := mustUID(c)
	var req struct {
		ToUID   int64  `json:"to_uid"`
		Message string `json:"message"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if err := h.svc.SendFriendRequest(uid, req.ToUID, req.Message); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// HandleFriendRequest 处理好友申请。
func (h *Handler) HandleFriendRequest(c *gin.Context) {
	uid := mustUID(c)
	reqID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		// 雪花 ID 超过 19 位或非数字时，前端 JS 可能精度丢失，给出明确提示
		resp.Fail(c, apperr.BadRequest("申请 ID 无效，请重新发起申请"))
		return
	}
	var req struct {
		Accept bool `json:"accept"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if err := h.svc.HandleFriendRequest(uid, reqID, req.Accept); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// DeleteFriend 删除好友。
func (h *Handler) DeleteFriend(c *gin.Context) {
	uid := mustUID(c)
	friendUID, _ := strconv.ParseInt(c.Param("uid"), 10, 64)
	if err := h.svc.DeleteFriend(uid, friendUID); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// SetFriendRemark 设置好友备注（空备注清除）。
func (h *Handler) SetFriendRemark(c *gin.Context) {
	uid := mustUID(c)
	friendUID, _ := strconv.ParseInt(c.Param("uid"), 10, 64)
	var req struct {
		Remark string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if err := h.svc.SetFriendRemark(uid, friendUID, req.Remark); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// ---- 群组 ----

// CreateGroup 建群。
func (h *Handler) CreateGroup(c *gin.Context) {
	uid := mustUID(c)
	var req struct {
		Name    string  `json:"name"`
		Members []int64 `json:"members"`
		Avatar  string  `json:"avatar"` // 群头像 URL，可选
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	g, err := h.svc.CreateGroup(uid, req.Name, req.Members, req.Avatar)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, g)
}

// GetGroup 查询群信息。
func (h *Handler) GetGroup(c *gin.Context) {
	uid := mustUID(c)
	gUID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	g, err := h.svc.GetGroup(uid, gUID)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, g)
}

// UpdateGroup 修改群名/群公告（仅群主或管理员，后端鉴权）。
func (h *Handler) UpdateGroup(c *gin.Context) {
	uid := mustUID(c)
	gUID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	var req struct {
		Name         string `json:"name"`
		Announcement string `json:"announcement"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if err := h.svc.UpdateGroupInfo(uid, gUID, req.Name, req.Announcement); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// ListUserGroups 我加入的群。
func (h *Handler) ListUserGroups(c *gin.Context) {
	uid := mustUID(c)
	list, err := h.svc.ListUserGroups(uid)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, list)
}

// InviteToGroup 邀请入群。
func (h *Handler) InviteToGroup(c *gin.Context) {
	uid := mustUID(c)
	gUID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	var req struct {
		Members []int64 `json:"members"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if err := h.svc.InviteToGroup(uid, gUID, req.Members); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// LeaveGroup 退群。
func (h *Handler) LeaveGroup(c *gin.Context) {
	uid := mustUID(c)
	gUID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	if err := h.svc.LeaveGroup(uid, gUID); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// RemoveMember 移除群成员（群主/管理员；管理员仅可移除普通成员，后端鉴权）。
// 路由用 POST .../kick 而非 DELETE /members/:uid：与已有 DELETE /members/me 静态段同层，
// 通配参数会与之冲突（Gin 启动 panic），故分方法分路径。
func (h *Handler) RemoveMember(c *gin.Context) {
	uid := mustUID(c)
	gUID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	targetUID, _ := strconv.ParseInt(c.Param("uid"), 10, 64)
	if targetUID <= 0 {
		resp.Fail(c, apperr.BadRequest("成员 ID 无效"))
		return
	}
	if err := h.svc.RemoveMember(uid, gUID, targetUID); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// SetMemberRole 设为/取消管理员（仅群主，后端鉴权）：body { role: 1 管理员 | 2 普通成员 }。
func (h *Handler) SetMemberRole(c *gin.Context) {
	uid := mustUID(c)
	gUID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	targetUID, _ := strconv.ParseInt(c.Param("uid"), 10, 64)
	if targetUID <= 0 {
		resp.Fail(c, apperr.BadRequest("成员 ID 无效"))
		return
	}
	var req struct {
		Role int8 `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if err := h.svc.SetMemberRole(uid, gUID, targetUID, req.Role); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// TransferOwner 转让群主（仅现任群主，后端鉴权）：body { new_owner_uid }。
func (h *Handler) TransferOwner(c *gin.Context) {
	uid := mustUID(c)
	gUID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	var req struct {
		NewOwnerUID int64 `json:"new_owner_uid"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if req.NewOwnerUID <= 0 {
		resp.Fail(c, apperr.BadRequest("目标成员无效"))
		return
	}
	if err := h.svc.TransferOwnership(uid, gUID, req.NewOwnerUID); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// SetMyNickname 设置我的群内昵称（任何成员）：body { nickname }，空字符串清除。
func (h *Handler) SetMyNickname(c *gin.Context) {
	uid := mustUID(c)
	gUID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	var req struct {
		Nickname string `json:"nickname"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if err := h.svc.SetMyNickname(uid, gUID, req.Nickname); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// UpdateGroupSettings 更新群设置开关（G7 入群确认 / G8 全员禁言，仅群主/管理员）：body { invite_confirm?, mute_all? }。
func (h *Handler) UpdateGroupSettings(c *gin.Context) {
	uid := mustUID(c)
	gUID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	var req struct {
		InviteConfirm *int8 `json:"invite_confirm"`
		MuteAll       *int8 `json:"mute_all"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if err := h.svc.UpdateGroupSettings(uid, gUID, req.InviteConfirm, req.MuteAll); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// DecideInvite 处理入群确认（G7，仅群主/管理员）：body { invitee_uid, accept }。
func (h *Handler) DecideInvite(c *gin.Context) {
	uid := mustUID(c)
	gUID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	var req struct {
		InviteeUID int64 `json:"invitee_uid"`
		Accept     bool  `json:"accept"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if req.InviteeUID <= 0 {
		resp.Fail(c, apperr.BadRequest("目标成员无效"))
		return
	}
	if err := h.svc.DecideInvite(uid, gUID, req.InviteeUID, req.Accept); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// SetMemberMutedUntil 设置/解除成员禁言（G8，仅群主/管理员）：body { until }，unix 毫秒，0=解除。
func (h *Handler) SetMemberMutedUntil(c *gin.Context) {
	uid := mustUID(c)
	gUID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	targetUID, _ := strconv.ParseInt(c.Param("uid"), 10, 64)
	var req struct {
		Until int64 `json:"until"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if err := h.svc.SetMemberMutedUntil(uid, gUID, targetUID, req.Until); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// UpdateGroupSaved 更新我"保存到通讯录"开关（G10，任何成员）：body { saved }。
func (h *Handler) UpdateGroupSaved(c *gin.Context) {
	uid := mustUID(c)
	gUID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	var req struct {
		Saved int8 `json:"saved"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if err := h.svc.UpdateGroupSaved(uid, gUID, req.Saved); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// ---- 通知 ----

// ListNotifications 通知列表。
func (h *Handler) ListNotifications(c *gin.Context) {
	uid := mustUID(c)
	list, err := h.svc.ListNotifications(uid)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, list)
}

// MarkRead 标记已读。
func (h *Handler) MarkRead(c *gin.Context) {
	uid := mustUID(c)
	all := c.Query("all") == "1"
	id, _ := strconv.ParseInt(c.Query("id"), 10, 64)
	if err := h.svc.MarkRead(uid, id, all); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// UnreadCount 未读通知数。
func (h *Handler) UnreadCount(c *gin.Context) {
	uid := mustUID(c)
	n, err := h.svc.UnreadCount(uid)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"count": n})
}

// Clear 清空通知。
func (h *Handler) Clear(c *gin.Context) {
	uid := mustUID(c)
	if err := h.svc.Clear(uid); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

func mustUID(c *gin.Context) int64 {
	uid, ok := middleware.UIDFromContext(c)
	if !ok {
		// 正常不会走到这里（JWT 中间件已拦截）
		return 0
	}
	return uid
}
