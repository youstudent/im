package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	apperr "im/service/internal/pkg/err"
	"im/service/internal/pkg/jwt"
	"im/service/internal/pkg/resp"
)

// ContextKey 用于在 gin.Context 中存取当前登录用户信息。
type ContextKey string

const (
	// CtxUIDKey 当前登录用户的业务 UID。
	CtxUIDKey ContextKey = "auth.uid"
)

// JWT 基于 access token 的鉴权中间件。
// 从 Authorization: Bearer <token> 读取并校验，校验通过后注入 uid 到上下文。
func JWT(jwtMgr *jwt.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		token := ""
		if strings.HasPrefix(header, "Bearer ") {
			token = strings.TrimPrefix(header, "Bearer ")
		}
		if token == "" {
			resp.Fail(c, apperr.Unauthorized("未登录或登录已过期"))
			return
		}
		claims, err := jwtMgr.Parse(token)
		if err != nil {
			resp.Fail(c, apperr.Unauthorized("登录已过期，请重新登录"))
			return
		}
		if claims.Type != "access" {
			resp.Fail(c, apperr.Unauthorized("令牌类型错误"))
			return
		}
		c.Set(string(CtxUIDKey), claims.UID)
		c.Next()
	}
}

// UIDFromContext 从上下文读取当前登录用户 uid。
func UIDFromContext(c *gin.Context) (int64, bool) {
	v, ok := c.Get(string(CtxUIDKey))
	if !ok {
		return 0, false
	}
	uid, ok := v.(int64)
	return uid, ok
}
