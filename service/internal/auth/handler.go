// handler.go 认证模块 HTTP 处理器：注册/登录/刷新/退出/二维码。
package auth

import (
	"github.com/gin-gonic/gin"

	apperr "im/service/internal/pkg/err"
	"im/service/internal/pkg/resp"
	"im/service/internal/server/middleware"
)

// Handler 认证 HTTP 处理器。
type Handler struct {
	svc *Service
}

// NewHandler 创建认证处理器。
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register 注册（注册即登录）。
func (h *Handler) Register(c *gin.Context) {
	var req RegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	res, err := h.svc.Register(&req, c.ClientIP())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, res)
}

// Login 账号密码登录。
func (h *Handler) Login(c *gin.Context) {
	var req LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	res, err := h.svc.Login(&req, c.ClientIP())
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, res)
}

// Refresh 刷新 token。
func (h *Handler) Refresh(c *gin.Context) {
	var req RefreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	res, err := h.svc.Refresh(&req)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, res)
}

// Logout 退出登录。
func (h *Handler) Logout(c *gin.Context) {
	var req LogoutReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if err := h.svc.Logout(&req); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}

// CreateQR 生成二维码。
func (h *Handler) CreateQR(c *gin.Context) {
	res, err := h.svc.CreateQR()
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, res)
}

// PollQR 轮询二维码状态。
func (h *Handler) PollQR(c *gin.Context) {
	var req PollQRReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	res, err := h.svc.PollQR(&req)
	if err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OK(c, res)
}

// ConfirmQR 手机扫码确认（需登录：确认者 uid 从 JWT 注入，忽略请求体，防冒充确认接管账号）。
func (h *Handler) ConfirmQR(c *gin.Context) {
	uid, ok := middleware.UIDFromContext(c)
	if !ok {
		resp.Fail(c, apperr.Unauthorized("未登录"))
		return
	}
	var req ConfirmQRReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if err := h.svc.ConfirmQR(uid, &req); err != nil {
		resp.Fail(c, err)
		return
	}
	resp.OKNoData(c)
}
