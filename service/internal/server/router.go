// Package server 负责 HTTP 路由装配与中间件挂载。
package server

import (
	"github.com/gin-gonic/gin"

	"im/service/internal/admin"
	"im/service/internal/auth"
	"im/service/internal/file"
	"im/service/internal/gateway"
	"im/service/internal/message"
	"im/service/internal/social"
	apperr "im/service/internal/pkg/err"
	"im/service/internal/pkg/jwt"
	"im/service/internal/pkg/log"
	"im/service/internal/pkg/resp"
	"im/service/internal/server/middleware"
	"im/service/internal/store/mysql"
	"im/service/internal/store/redis"
)

// Dependencies 路由依赖的公共资源，便于各业务模块注册。
type Dependencies struct {
	MySQL *mysql.DB
	Redis *redis.Client
	JWT   *jwt.Manager
	// Auth 认证模块服务与处理器（阶段二）。
	AuthSvc  *auth.Service
	AuthHdlr *auth.Handler
	// Message 消息模块（阶段三）。
	MessageSvc  *message.Service
	MessageHdlr *message.Handler
	WSGateway   *gateway.Server
	// Social 社交模块（阶段四）。
	SocialSvc  *social.Service
	SocialHdlr *social.Handler
	// Admin 管理后台（阶段五）。
	AdminHdlr *admin.Handler
	// File 文件预签名（阶段五）。
	FileHdlr *file.Handler
}

// NewRouter 创建并装配 Gin 引擎。
func NewRouter(deps *Dependencies, mode string) *gin.Engine {
	gin.SetMode(mode)

	r := gin.New()
	r.Use(middleware.Logger(), middleware.Recovery(), middleware.CORS())

	// 健康检查
	health := &healthHandler{deps: deps}
	r.GET("/healthz", health.Check)

	// 业务路由
	api := r.Group("/api/v1")
	{
		// 认证（无需鉴权；qrcode/confirm 例外——必须已登录，确认者 uid 从令牌取，防冒充确认接管账号）
		authGroup := api.Group("/auth")
		{
			authGroup.POST("/register", deps.AuthHdlr.Register)
			authGroup.POST("/login", deps.AuthHdlr.Login)
			authGroup.POST("/refresh", deps.AuthHdlr.Refresh)
			authGroup.POST("/logout", deps.AuthHdlr.Logout)
			authGroup.POST("/qrcode/create", deps.AuthHdlr.CreateQR)
			authGroup.POST("/qrcode/poll", deps.AuthHdlr.PollQR)
			authGroup.POST("/qrcode/confirm", middleware.JWT(deps.JWT), deps.AuthHdlr.ConfirmQR)
		}

		// 会话 / 消息（需鉴权）
		convGroup := api.Group("/conversations", middleware.JWT(deps.JWT))
		{
			convGroup.GET("", deps.MessageHdlr.ListConversations)
			convGroup.POST("", deps.MessageHdlr.Send)
			convGroup.GET("/search", deps.MessageHdlr.Search)
			convGroup.GET("/:id/messages", deps.MessageHdlr.GetHistory)
			convGroup.POST("/:id/recall", deps.MessageHdlr.RecallMessage)
		}
		// 文件预签名（需鉴权）
		fileGroup := api.Group("/files", middleware.JWT(deps.JWT))
		{
			fileGroup.POST("/presign", deps.FileHdlr.Presign)
		}

		// 社交：好友 / 群组 / 通知（需鉴权）
		friendGroup := api.Group("/friends", middleware.JWT(deps.JWT))
		{
			friendGroup.GET("", deps.SocialHdlr.ListFriends)
			friendGroup.GET("/requests", deps.SocialHdlr.ListFriendRequests)
			friendGroup.POST("/requests", deps.SocialHdlr.SendFriendRequest)
			friendGroup.POST("/requests/:id/handle", deps.SocialHdlr.HandleFriendRequest)
			friendGroup.PUT("/:uid/remark", deps.SocialHdlr.SetFriendRemark)
			friendGroup.DELETE("/:uid", deps.SocialHdlr.DeleteFriend)
		}
		// 用户搜索（按手机号/邮箱/昵称，用于加好友）
		userGroup := api.Group("/users", middleware.JWT(deps.JWT))
		{
			userGroup.GET("/search", deps.SocialHdlr.SearchUser)
		}
		groupGroup := api.Group("/groups", middleware.JWT(deps.JWT))
		{
			groupGroup.POST("", deps.SocialHdlr.CreateGroup)
			groupGroup.GET("", deps.SocialHdlr.ListUserGroups)
			groupGroup.GET("/:gid", deps.SocialHdlr.GetGroup)
			groupGroup.PUT("/:gid", deps.SocialHdlr.UpdateGroup)
			groupGroup.POST("/:gid/members", deps.SocialHdlr.InviteToGroup)
			groupGroup.DELETE("/:gid/members/me", deps.SocialHdlr.LeaveGroup)
		}
		notifyGroup := api.Group("/notifications", middleware.JWT(deps.JWT))
		{
			notifyGroup.GET("", deps.SocialHdlr.ListNotifications)
			notifyGroup.POST("/read", deps.SocialHdlr.MarkRead)
			notifyGroup.GET("/unread", deps.SocialHdlr.UnreadCount)
			notifyGroup.DELETE("", deps.SocialHdlr.Clear)
		}

		// 客户端检查更新（公开接口，仅返回非敏感版本信息）
		api.GET("/version/latest", deps.AdminHdlr.LatestVersion)
	}

	// WebSocket 长连接网关
	r.GET("/ws", deps.WSGateway.HandleWS)

	// 管理后台（阶段五）：登录无需鉴权，其余需 admin JWT
	adminApi := r.Group("/api/admin")
	{
		adminApi.POST("/login", deps.AdminHdlr.Login)
		adminGroup := adminApi.Group("", middleware.AdminAuth(deps.JWT))
		{
			// 修改自己的密码（种子账号首次登录强制改密）
			adminGroup.POST("/password", deps.AdminHdlr.ChangePassword)
			adminGroup.GET("/dashboard", deps.AdminHdlr.Dashboard)
			adminGroup.GET("/users", deps.AdminHdlr.ListUsers)
			adminGroup.DELETE("/users/:uid/disable", deps.AdminHdlr.DisableUser)
			adminGroup.DELETE("/users/:uid/enable", deps.AdminHdlr.EnableUser)
			adminGroup.GET("/groups", deps.AdminHdlr.ListGroups)
			adminGroup.GET("/groups/:gid/messages", deps.AdminHdlr.GroupMessages)
			adminGroup.DELETE("/groups/:gid", deps.AdminHdlr.DeleteGroup)
			// 版本发布：发布新版本 / 版本列表
			adminGroup.POST("/version", deps.AdminHdlr.PublishVersion)
			adminGroup.GET("/versions", deps.AdminHdlr.ListVersions)
			// 管理端文件上传预签名（安装包直传 OSS）
			adminGroup.POST("/files/presign", deps.FileHdlr.PresignForAdmin)
		}
	}

	return r
}

type healthHandler struct {
	deps *Dependencies
}

// Check 健康检查：校验 MySQL 与 Redis 连通性。
// 审计 L3：错误详情仅写服务端日志，不回显给调用方（防连接串/内部地址泄露）。
func (h *healthHandler) Check(c *gin.Context) {
	if err := h.deps.MySQL.Ping(); err != nil {
		log.L().Error("healthz: mysql ping failed", "error", err)
		resp.Fail(c, apperr.Unavailable("服务暂时不可用"))
		return
	}
	if err := h.deps.Redis.Ping(c.Request.Context()).Err(); err != nil {
		log.L().Error("healthz: redis ping failed", "error", err)
		resp.Fail(c, apperr.Unavailable("服务暂时不可用"))
		return
	}
	resp.OK(c, gin.H{"status": "ok"})
}
