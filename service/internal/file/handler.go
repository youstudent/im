// Package file 提供 OSS 预签名上传接口，支撑图片/文件/语音消息。
package file

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	apperr "im/service/internal/pkg/err"
	"im/service/internal/pkg/resp"
	"im/service/internal/server/middleware"
)

// OSSer OSS 客户端接口（便于测试注入）。
type OSSer interface {
	PresignPut(objectKey, contentType string) (string, error)
	PresignGet(objectKey string) (string, error)
	PublicURL(objectKey string) string // 固定访问 URL（Bucket 公共读场景）
	ExpireSeconds() time.Duration
}

// Handler 文件处理器。
type Handler struct {
	oss   OSSer
	genID func() int64
}

// NewHandler 创建文件处理器。
func NewHandler(ossClient OSSer, genID func() int64) *Handler {
	return &Handler{oss: ossClient, genID: genID}
}

// PresignReq 预签名请求。
type PresignReq struct {
	FileName    string `json:"file_name"`    // 原始文件名（含扩展名）
	Type        string `json:"type"`         // image / file / voice
	Size        int64  `json:"size"`         // 文件大小（字节）
	ContentType string `json:"content_type"` // 文件 MIME 类型（与上传时的 Content-Type 一致，缺省 application/octet-stream）
	Duration    int64  `json:"duration"`     // 语音时长（秒，仅 voice 类型上报；>60s 拒绝）
}

// 上传安全约束（审计 P1）：
//   - 用户端黑名单拦截可在浏览器直接执行/触发下载的危险类型（公共读 Bucket 上传 html/svg 即存储型 XSS）；
//   - image 类型仅允许图片扩展名（与桌面端白名单对齐，防止以图片名义上传任意文件）；
//   - 大小上限防滥用；管理端仅允许安装包类型。
var dangerousExt = map[string]bool{
	".html": true, ".htm": true, ".svg": true, ".xhtml": true, ".mht": true, ".mhtml": true,
	".js": true, ".mjs": true, ".jse": true, ".php": true, ".jsp": true, ".asp": true, ".aspx": true,
	".sh": true, ".bat": true, ".cmd": true, ".ps1": true, ".psm1": true, ".vbs": true, ".vbe": true,
	".wsf": true, ".wsh": true, ".sct": true, ".hta": true, ".cpl": true, ".msc": true, ".inf": true,
	".exe": true, ".msi": true, ".scr": true, ".dll": true, ".com": true, ".pif": true,
	".lnk": true, ".url": true, ".reg": true, ".chm": true, ".jar": true, ".gadget": true, ".application": true,
	// 宏文档/加载项：可在 Office 内执行宏代码
	".docm": true, ".xlsm": true, ".pptm": true, ".dotm": true, ".xlam": true,
	// 磁盘镜像/虚拟磁盘：可绕过 Windows 下载标记（Mark of the Web）
	".iso": true, ".img": true, ".vhd": true, ".vhdx": true,
	// Windows 库/设置文件：已知本地提权/远程代码执行载体
	".library-ms": true, ".searchconnector-ms": true, ".settingcontent-ms": true, ".theme": true,
}

// imageExtWhitelist 图片类型上传允许的扩展名（与桌面端"发送图片"白名单一致）。
var imageExtWhitelist = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true,
	".webp": true, ".heic": true, ".heif": true, ".tiff": true, ".ico": true,
}

// voiceExtWhitelist 语音类型上传允许的扩展名（与桌面端音频后缀识别一致）。
var voiceExtWhitelist = map[string]bool{
	".webm": true, ".m4a": true, ".aac": true, ".mp3": true,
	".wav": true, ".ogg": true, ".flac": true,
}

// 用户端分类上传上限：图片 20MB / 语音 10MB 且时长≤60s / 普通文件 200MB。
const (
	maxUserUploadSize   = 200 << 20 // 普通文件单文件上限 200MB
	maxImageUploadSize  = 20 << 20  // 图片上限 20MB
	maxVoiceUploadSize  = 10 << 20  // 语音大小上限 10MB
	maxVoiceDurationSec = 60        // 语音时长上限 60 秒
)

var adminExtWhitelist = map[string]bool{
	".exe": true, ".msi": true, ".dmg": true, ".zip": true, ".blockmap": true,
}

const maxAdminUploadSize = 2 << 30 // 管理端安装包上限 2GB

// PresignResult 预签名结果。
type PresignResult struct {
	ObjectKey  string `json:"object_key"`
	UploadURL  string `json:"upload_url"`
	DownloadURL string `json:"download_url"`
	ExpireIn   int64  `json:"expire_in"`
}

// Presign 生成预签名上传 URL（用户端：图片/文件/语音）。
func (h *Handler) Presign(c *gin.Context) {
	uid, ok := middleware.UIDFromContext(c)
	if !ok {
		resp.Fail(c, apperr.Unauthorized("未登录"))
		return
	}
	h.presignFor(c, fmt.Sprintf("%d", uid), "", false)
}

// PresignForAdmin 管理端上传预签名（安装包等），objectKey 归到 installer/ 目录并按管理员 ID 隔离。
func (h *Handler) PresignForAdmin(c *gin.Context) {
	adminID, ok := c.Get(string(middleware.CtxAdminIDKey))
	id, _ := adminID.(int64)
	// 审计 L4：身份缺失/断言失败时直接拒绝，防多管理员文件混入同一 admin0/ 目录
	if !ok || id <= 0 {
		resp.Fail(c, apperr.Unauthorized("无法获取管理员身份"))
		return
	}
	h.presignFor(c, fmt.Sprintf("admin%d", id), "installer", true)
}

// presignFor 预签名公共逻辑：dir 为对象目录前缀（空则用 req.Type），owner 用于路径隔离；
// isAdmin 决定扩展名策略（用户端黑名单拦截危险类型，管理端仅允许安装包）。
func (h *Handler) presignFor(c *gin.Context, owner, dir string, isAdmin bool) {
	var req PresignReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, apperr.BadRequest("请求参数错误: "+err.Error()))
		return
	}
	if req.FileName == "" {
		resp.Fail(c, apperr.BadRequest("文件名不能为空"))
		return
	}
	typ := req.Type
	if typ == "" {
		typ = "file"
	}
	if dir == "" {
		dir = typ
	}
	ext := strings.ToLower(extOf(req.FileName))
	// 类型/大小约束（审计 P1）：公共读 Bucket 上危险类型可被直接执行/触发下载
	if isAdmin {
		if !adminExtWhitelist[ext] {
			resp.Fail(c, apperr.BadRequest("仅支持上传安装包文件（exe/msi/dmg/zip）"))
			return
		}
		if req.Size <= 0 || req.Size > maxAdminUploadSize {
			resp.Fail(c, apperr.BadRequest("文件大小超限（上限 2GB）"))
			return
		}
	} else {
		if dangerousExt[ext] {
			resp.Fail(c, apperr.BadRequest("不支持上传该类型文件"))
			return
		}
		switch typ {
		case "image":
			// 图片类型仅允许图片扩展名（与桌面端白名单对齐）
			if !imageExtWhitelist[ext] {
				resp.Fail(c, apperr.BadRequest("仅支持上传图片文件（jpg/png/gif/webp 等）"))
				return
			}
			if req.Size <= 0 || req.Size > maxImageUploadSize {
				resp.Fail(c, apperr.BadRequest("图片大小超限（上限 20MB）"))
				return
			}
		case "voice":
			// 语音：仅允许音频扩展名，时长≤60s，大小≤10MB
			if !voiceExtWhitelist[ext] {
				resp.Fail(c, apperr.BadRequest("仅支持上传语音文件（webm/m4a/mp3/wav 等）"))
				return
			}
			if req.Duration > maxVoiceDurationSec {
				resp.Fail(c, apperr.BadRequest("语音时长超限（最长 60 秒）"))
				return
			}
			if req.Size <= 0 || req.Size > maxVoiceUploadSize {
				resp.Fail(c, apperr.BadRequest("语音大小超限（上限 10MB）"))
				return
			}
		default:
			if req.Size <= 0 || req.Size > maxUserUploadSize {
				resp.Fail(c, apperr.BadRequest("文件大小超限（上限 200MB）"))
				return
			}
		}
	}
	// objectKey: dir/owner/时间戳_随机.ext
	objectKey := fmt.Sprintf("%s/%s/%d_%d%s", dir, owner, time.Now().UnixMilli(), h.genID()%10000, ext)
	uploadURL, err := h.oss.PresignPut(objectKey, req.ContentType)
	if err != nil {
		resp.Fail(c, apperr.WrapInternal("生成上传链接失败", err))
		return
	}
	// Bucket 为公共读时使用固定 URL（永不过期），供前端长期展示/下载
	downloadURL := h.oss.PublicURL(objectKey)
	resp.OK(c, &PresignResult{
		ObjectKey:   objectKey,
		UploadURL:   uploadURL,
		DownloadURL: downloadURL,
		ExpireIn:    int64(h.oss.ExpireSeconds().Seconds()),
	})
}

func extOf(name string) string {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return ""
	}
	return name[i:]
}

// DisabledOSS OSS 未配置时的占位实现，返回明确错误。
type DisabledOSS struct{}

// PresignPut 返回未配置错误。
func (d *DisabledOSS) PresignPut(objectKey, contentType string) (string, error) {
	return "", apperr.Unavailable("OSS 未配置，文件上传不可用")
}

// PresignGet 返回未配置错误。
func (d *DisabledOSS) PresignGet(objectKey string) (string, error) {
	return "", apperr.Unavailable("OSS 未配置，文件下载不可用")
}

// PublicURL 返回空（未配置时不可用）。
func (d *DisabledOSS) PublicURL(objectKey string) string {
	return ""
}

// ExpireSeconds 返回默认有效期。
func (d *DisabledOSS) ExpireSeconds() time.Duration { return 15 * time.Minute }
