// handler.go：管理后台 HTTP 处理器。
package admin

import (
	"strconv"

	"github.com/gin-gonic/gin"

	apperr "im/service/internal/pkg/err"
	"im/service/internal/pkg/resp"
	"im/service/internal/server/middleware"
)

// Handler 管理后台处理器。
type Handler struct {
	svc *Service
}

// NewHandler 创建管理后台处理器。
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Login 管理员登录。
func (h *Handler) Login(c *gin.Context) {
	var req LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	res, err := h.svc.Login(&req)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, res)
}

// Dashboard 数据看板。
func (h *Handler) Dashboard(c *gin.Context) {
	d, err := h.svc.GetDashboard()
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, d)
}

// ListUsers 用户列表（分页，支持 keyword 模糊搜索昵称/账号、status 按账号状态过滤）。
// status 约定：0/空=全部，1=正常，2=已禁用。
func (h *Handler) ListUsers(c *gin.Context) {
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	keyword := c.Query("q")
	status, _ := strconv.ParseInt(c.DefaultQuery("status", "0"), 10, 64)
	if status < 0 || status > 2 {
		status = 0
	}
	list, err := h.svc.ListUsers(offset, limit, keyword, status)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"list": list, "total": h.svc.TotalUsers(keyword, status)})
}

// DisableUser 禁用用户（若在线则踢下线）。
func (h *Handler) DisableUser(c *gin.Context) {
	uid, _ := strconv.ParseInt(c.Param("uid"), 10, 64)
	if err := h.svc.DisableUser(uid); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// EnableUser 启用用户。
func (h *Handler) EnableUser(c *gin.Context) {
	uid, _ := strconv.ParseInt(c.Param("uid"), 10, 64)
	if err := h.svc.EnableUser(uid); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// ListGroups 群列表（分页，支持 keyword 模糊搜索群名/群号）。
func (h *Handler) ListGroups(c *gin.Context) {
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	keyword := c.Query("q")
	list, err := h.svc.ListGroups(offset, limit, keyword)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"list": list, "total": h.svc.TotalGroups(keyword)})
}

// DeleteGroup 解散群。
func (h *Handler) DeleteGroup(c *gin.Context) {
	gUID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	if err := h.svc.DeleteGroup(gUID); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// GroupMessages 查看群聊天记录。
func (h *Handler) GroupMessages(c *gin.Context) {
	gUID, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	beforeSeq, _ := strconv.ParseInt(c.Query("before_seq"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := h.svc.GetGroupMessages(gUID, beforeSeq, limit)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"list": list})
}

// ---- 客户端版本发布（检查更新） ----

// PublishVersion 发布新版本（管理端）。
func (h *Handler) PublishVersion(c *gin.Context) {
	adminID, _ := c.Get(string(middleware.CtxAdminIDKey))
	id, _ := adminID.(int64)
	var req PublishVersionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if err := h.svc.PublishVersion(id, &req); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// ListVersions 版本列表（管理端，倒序分页）。
func (h *Handler) ListVersions(c *gin.Context) {
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	list, total, err := h.svc.ListVersions(offset, limit)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, gin.H{"list": list, "total": total})
}

// LatestVersion 客户端检查更新（公开接口，无需鉴权；仅返回非敏感版本信息）。
func (h *Handler) LatestVersion(c *gin.Context) {
	v, err := h.svc.LatestVersion()
	if err != nil {
		resp.Fail(c, err)
		return
	}
	if v == nil {
		resp.OK(c, gin.H{"has": false})
		return
	}
	resp.OK(c, gin.H{"has": true, "version": v})
}
