package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	apperr "im/service/internal/pkg/err"
	"im/service/internal/pkg/jwt"
	"im/service/internal/pkg/resp"
)

// CtxAdminIDKey 管理后台当前登录管理员 ID。
const CtxAdminIDKey ContextKey = "admin.id"

// AdminAuth 管理后台鉴权中间件：校验 IsAdmin 令牌。
func AdminAuth(jwtMgr *jwt.Manager) gin.HandlerFunc {
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
		if !claims.IsAdmin {
			resp.Fail(c, apperr.Forbidden("无管理权限"))
			return
		}
		c.Set(string(CtxAdminIDKey), claims.UID)
		c.Next()
	}
}
