// handler.go：消息模块 HTTP 处理器（会话列表、历史、发送）。
package message

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	apperr "im/service/internal/pkg/err"
	"im/service/internal/pkg/resp"
	"im/service/internal/server/middleware"
)

// Handler 消息 HTTP 处理器。
type Handler struct {
	svc *Service
}

// NewHandler 创建消息处理器。
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Send HTTP 发送消息（低频场景可走 HTTP，高频走 WS）。
func (h *Handler) Send(c *gin.Context) {
	var req SendReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	uid, ok := middleware.UIDFromContext(c)
	if !ok {
		resp.Fail(c, apperr.Unauthorized("未登录"))
		return
	}
	dto, _, err := h.svc.Send(uid, &req)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, dto)
}

// GetHistory 拉取历史消息：
//   - after_seq>0：增量拉取 seq > after_seq 的消息（升序，客户端本地缓存补齐）
//   - 否则：倒序分页（before_seq 向前翻页 / 缺省取最新 limit 条）
func (h *Handler) GetHistory(c *gin.Context) {
	uid, ok := middleware.UIDFromContext(c)
	if !ok {
		resp.Fail(c, apperr.Unauthorized("未登录"))
		return
	}
	convID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	if afterSeq, _ := strconv.ParseInt(c.Query("after_seq"), 10, 64); afterSeq > 0 {
		list, err := h.svc.GetHistoryAfterSeq(uid, convID, afterSeq, limit)
		if err != nil {
			resp.Fail(c, err)
			return
		}
		resp.OK(c, list)
		return
	}
	beforeSeq, _ := strconv.ParseInt(c.Query("before_seq"), 10, 64)
	list, err := h.svc.GetHistory(uid, convID, beforeSeq, limit)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, list)
}

// Search 消息搜索（关键字 + 类型）。仅限当前用户自己的会话范围（审计 P1，防越权）。
func (h *Handler) Search(c *gin.Context) {
	uid, ok := middleware.UIDFromContext(c)
	if !ok {
		resp.Fail(c, apperr.Unauthorized("未登录"))
		return
	}
	keyword := c.Query("keyword")
	msgType, _ := strconv.ParseInt(c.DefaultQuery("type", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	list, err := h.svc.Search(uid, keyword, int8(msgType), limit)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, list)
}

// RecallMessage 撤回消息（2 分钟内）。
func (h *Handler) RecallMessage(c *gin.Context) {
	uid, ok := middleware.UIDFromContext(c)
	if !ok {
		resp.Fail(c, apperr.Unauthorized("未登录"))
		return
	}
	convID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req RecallReq
	if err := c.ShouldBindJSON(&req); err != nil || req.MsgID <= 0 {
		resp.Fail(c, apperr.BadRequest("参数错误"))
		return
	}
	dto, err := h.svc.RecallMessage(uid, convID, req.MsgID)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, dto)
}

// UpdateSettings 更新会话设置（置顶/免打扰，仅本人会话视图；未传的字段保持不变）。
func (h *Handler) UpdateSettings(c *gin.Context) {
	uid, ok := middleware.UIDFromContext(c)
	if !ok {
		resp.Fail(c, apperr.Unauthorized("未登录"))
		return
	}
	convID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if convID <= 0 {
		resp.Fail(c, apperr.BadRequest("会话 ID 无效"))
		return
	}
	var req struct {
		Pinned *int8 `json:"pinned"`
		Muted  *int8 `json:"muted"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if err := h.svc.UpdateConversationSettings(uid, convID, req.Pinned, req.Muted); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// DeleteConversation 删除会话（仅删本人会话视图行，保留消息；再次收发消息自动重建）。
func (h *Handler) DeleteConversation(c *gin.Context) {
	uid, ok := middleware.UIDFromContext(c)
	if !ok {
		resp.Fail(c, apperr.Unauthorized("未登录"))
		return
	}
	convID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if convID <= 0 {
		resp.Fail(c, apperr.BadRequest("会话 ID 无效"))
		return
	}
	if err := h.svc.DeleteConversation(uid, convID); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// ListConversations 会话列表。
// changed_since（unix 秒，可选）：仅返回该时间之后有变化的会话（含空会话），
// 客户端本地已有全量时用它做差量刷新，减少服务端查表量。
func (h *Handler) ListConversations(c *gin.Context) {
	uid, ok := middleware.UIDFromContext(c)
	if !ok {
		resp.Fail(c, apperr.Unauthorized("未登录"))
		return
	}
	var (
		list []*ConversationDTO
		err  error
	)
	if sinceUnix, _ := strconv.ParseInt(c.Query("changed_since"), 10, 64); sinceUnix > 0 {
		list, err = h.svc.ListConversationsChangedSince(uid, time.Unix(sinceUnix, 0))
	} else {
		list, err = h.svc.ListConversations(uid)
	}
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, list)
}
