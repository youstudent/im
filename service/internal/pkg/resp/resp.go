// Package resp 提供统一的 HTTP 响应结构。
package resp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	apperr "im/service/internal/pkg/err"
)

// Body 统一响应结构。code==0 表示成功，data 承载业务数据；失败时 message 描述原因。
type Body struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// OK 返回成功响应。
func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{Code: int(apperr.CodeOK), Message: "ok", Data: data})
}

// OKNoData 返回成功但无业务数据的响应。
func OKNoData(c *gin.Context) {
	c.JSON(http.StatusOK, Body{Code: int(apperr.CodeOK), Message: "ok"})
}

// Fail 返回失败响应，根据错误码映射 HTTP 状态码。
func Fail(c *gin.Context, e error) {
	appErr, ok := e.(*apperr.Error)
	if !ok {
		appErr = apperr.Internal(e.Error())
	}
	status := mapHTTPStatus(appErr.Code)
	c.AbortWithStatusJSON(status, Body{
		Code:    int(appErr.Code),
		Message: appErr.Message,
	})
}

func mapHTTPStatus(code apperr.Code) int {
	switch code {
	case apperr.CodeOK:
		return http.StatusOK
	case apperr.CodeBadRequest:
		return http.StatusBadRequest
	case apperr.CodeUnauthorized:
		return http.StatusUnauthorized
	case apperr.CodeForbidden:
		return http.StatusForbidden
	case apperr.CodeNotFound:
		return http.StatusNotFound
	case apperr.CodeConflict:
		return http.StatusConflict
	case apperr.CodeInternal:
		return http.StatusInternalServerError
	case apperr.CodeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
