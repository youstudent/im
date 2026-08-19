// Package middleware 提供 Gin 公共中间件：请求日志、panic 恢复。
package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	"im/service/internal/pkg/log"
)

// Logger 请求日志中间件，记录方法/路径/状态码/耗时/客户端 IP。
// OPTIONS 预检请求（CORS）不打印日志，避免跨域场景刷屏。
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 拦截 OPTIONS 预检请求：不打印日志（CORS 中间件会返回 204）
		if c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}

		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery
		if query != "" {
			path = path + "?" + query
		}

		c.Next()

		latency := time.Since(start)
		log.L().Info("http",
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"latency", latency.String(),
			"ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
		)
	}
}
