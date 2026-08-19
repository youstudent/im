// Package admin 实现管理后台：管理员登录鉴权、用户/群组管理、数据看板。
package admin

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	apperr "im/service/internal/pkg/err"
	"im/service/internal/pkg/jwt"
	"im/service/internal/pkg/log"
	"im/service/internal/pkg/pwd"
	"im/service/internal/store/mysql"
)

// Store 管理后台依赖的存储接口。
type Store interface {
	GetAdminByUsername(username string) (*mysql.AdminUser, error)
	CreateAdmin(a *mysql.AdminUser) error
	ListAdmins() ([]*mysql.AdminUser, error)
	CountUsers() (int64, error)
	CountGroups() (int64, error)
	CountMessages() (int64, error)
	CountOnlinePresence() (int64, error)
	UserTrendByDay(days int) ([]int64, error)
	MessageTrendByDay(days int) ([]int64, error)
	// 用户管理
	ListUsers(offset, limit int, keyword string, status int64) ([]*mysql.User, error)
	CountUsersTotal(keyword string, status int64) (int64, error)
	// 群组管理
	ListAllGroups(offset, limit int, keyword string) ([]*mysql.Group, error)
	CountGroupsTotal(keyword string) (int64, error)
	GetGroupByGUID(gUID int64) (*mysql.Group, error)
	// 群聊天记录
	ListMessagesBefore(convID, beforeSeq int64, limit int) ([]*mysql.Message, error)
	GetUserByUID(uid int64) (*mysql.User, error)
	GetUserNames(uids []int64) map[int64]string // P1 优化：发送者昵称批量查
	// 业务操作
	DisableUser(uid int64) error
	EnableUser(uid int64) error
	DeleteGroupByGUID(gUID int64) error
	ListGroupMembers(gUID int64) ([]int64, error)
	DeleteAllGroupConversationViews(gUID int64) error
	// 版本发布
	GetAdminByID(id int64) (*mysql.AdminUser, error)
	UpdateAdminPassword(id int64, passwordHash string) error
	CreateAppVersion(v *mysql.AppVersion) error
	ListAppVersions(offset, limit int) ([]*mysql.AppVersion, error)
	CountAppVersions() (int64, error)
	GetLatestAppVersion() (*mysql.AppVersion, error)
}

// Service 管理后台服务。
type Service struct {
	store Store
	jwt   *jwt.Manager
	genID func() int64
	kick  func(uid int64, reason string) // 可选：禁用时踢该用户下线（由网关注入）
	loginCache LoginCache // 可选：登录限流缓存（Redis），nil 时不限流
	downloadHosts map[string]bool // 可选：版本下载地址域名白名单，空时仅校验 https 协议
	dismissNotify DismissNotifier // 可选：解散群时通知成员清理会话（由网关注入）
}

// DismissNotifier 解散群通知：向成员推送 group.left 事件（复用退群事件语义，
// 客户端据此清理会话列表与本地消息，无需客户端改动）。
type DismissNotifier func(uid int64, gUID, convID int64)

// SetDismissNotifier 运行时注入解散群通知能力（由装配层用网关实现）。
func (s *Service) SetDismissNotifier(fn DismissNotifier) { s.dismissNotify = fn }

// SetAllowedDownloadHosts 注入版本下载地址域名白名单（审计 L2，由装配层用 OSS 域名注入）。
// 白名单为空时仅强制 https，不阻断未配置 OSS 的部署。
func (s *Service) SetAllowedDownloadHosts(hosts []string) {
	m := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			m[h] = true
		}
	}
	s.downloadHosts = m
}

// LoginCache 管理端登录限流依赖的缓存接口（复用 Redis IncrWithTTL）。
type LoginCache interface {
	IncrWithTTL(key string, ttl time.Duration) (int64, error)
	Del(key string) error
}

// 管理端登录限流（审计 H2）：防爆破管理员密码（管理员可禁用用户/解散群/发布新版本，
// 是供应链入口）；窗口内尝试次数超限直接拒绝，登录成功清零；缓存不可用时放行不阻断。
const (
	adminLoginLimitMax    = 5
	adminLoginLimitWindow = 15 * time.Minute
)

func adminLoginLimitKey(username string) string { return "admin:login:limit:" + username }

// SetLoginCache 运行时注入登录限流缓存（由装配层用 Redis 实现）。
func (s *Service) SetLoginCache(c LoginCache) {
	s.loginCache = c
}

// New 创建管理后台服务。
func New(store Store, jwtMgr *jwt.Manager, genID func() int64) *Service {
	return &Service{store: store, jwt: jwtMgr, genID: genID}
}

// SetKickFunc 运行时注入"踢用户下线"的能力（由网关实现）。
func (s *Service) SetKickFunc(fn func(uid int64, reason string)) {
	s.kick = fn
}

// LoginReq 管理员登录请求。
type LoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResult 登录成功返回。
type LoginResult struct {
	AccessToken string      `json:"access_token"`
	Admin       *AdminInfo  `json:"admin"`
	// 首次登录必须修改密码（种子默认账号）：前端据此强制弹出改密框
	MustChangePwd bool `json:"must_change_pwd"`
}

// AdminInfo 管理员信息。
type AdminInfo struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Role     int8   `json:"role"`
}

// Login 管理员登录，返回 admin JWT。
func (s *Service) Login(req *LoginReq) (*LoginResult, error) {
	if req.Username == "" || req.Password == "" {
		return nil, apperr.BadRequest("用户名或密码不能为空")
	}
	// 登录限流（审计 H2）：在查库前拦截，防密码爆破与账号枚举；限流不可用时放行
	if s.loginCache != nil {
		if n, err := s.loginCache.IncrWithTTL(adminLoginLimitKey(req.Username), adminLoginLimitWindow); err == nil && n > adminLoginLimitMax {
			return nil, apperr.TooManyRequests("登录尝试过于频繁，请 15 分钟后再试")
		}
	}
	a, err := s.store.GetAdminByUsername(req.Username)
	if err != nil {
		return nil, apperr.Unauthorized("用户名或密码错误")
	}
	if a.Status != 1 {
		return nil, apperr.Forbidden("账号已被禁用")
	}
	if !pwd.Verify(a.PasswordHash, req.Password) {
		return nil, apperr.Unauthorized("用户名或密码错误")
	}
	// 登录成功：清零失败计数
	if s.loginCache != nil {
		_ = s.loginCache.Del(adminLoginLimitKey(req.Username))
	}
	// 审计日志（审计 L7）：管理员登录留痕
	log.L().Info("admin login", "admin_id", a.ID, "username", a.Username)
	token, err := s.jwt.GenerateAdmin(a.ID)
	if err != nil {
		return nil, apperr.WrapInternal("签发令牌失败", err)
	}
	return &LoginResult{
		AccessToken:   token,
		Admin:         &AdminInfo{ID: a.ID, Username: a.Username, Nickname: a.Nickname, Role: a.Role},
		MustChangePwd: a.MustChangePwd == 1,
	}, nil
}

// ChangePwdReq 修改密码请求。
type ChangePwdReq struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// ChangePassword 管理员修改自己的密码：校验旧密码、新密码强度（至少 8 位且含字母和数字，
// 与用户注册强度一致）、新旧不同；成功后清零强制改密标记。
func (s *Service) ChangePassword(adminID int64, req *ChangePwdReq) error {
	if adminID <= 0 {
		return apperr.Unauthorized("未登录")
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		return apperr.BadRequest("旧密码与新密码不能为空")
	}
	if len(req.NewPassword) < 8 || !hasLetterAndDigit(req.NewPassword) {
		return apperr.BadRequest("新密码至少 8 位，且同时包含字母和数字")
	}
	if req.OldPassword == req.NewPassword {
		return apperr.BadRequest("新密码不能与旧密码相同")
	}
	a, err := s.store.GetAdminByID(adminID)
	if err != nil || a == nil {
		return apperr.Unauthorized("账号不存在")
	}
	if !pwd.Verify(a.PasswordHash, req.OldPassword) {
		return apperr.BadRequest("旧密码错误")
	}
	hash, err := pwd.Hash(req.NewPassword)
	if err != nil {
		return apperr.WrapInternal("密码加密失败", err)
	}
	if err := s.store.UpdateAdminPassword(adminID, hash); err != nil {
		return apperr.WrapInternal("修改密码失败", err)
	}
	log.L().Info("admin password changed", "admin_id", adminID)
	return nil
}

// hasLetterAndDigit 密码强度校验：同时包含字母与数字。
func hasLetterAndDigit(s string) bool {
	hasLetter, hasDigit := false, false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}
	return hasLetter && hasDigit
}

// ---- 看板 ----

// Dashboard 数据看板。
type Dashboard struct {
	Users    int64  `json:"users"`
	Groups   int64  `json:"groups"`
	Messages int64  `json:"messages"`
	Online   int64  `json:"online"`
	// 近 7 天趋势（下标 0 为最早一天），供柱状图等图表使用
	UserTrend    []int64 `json:"user_trend"`
	MessageTrend []int64 `json:"message_trend"`
}

// GetDashboard 统计概览。
func (s *Service) GetDashboard() (*Dashboard, error) {
	users, _ := s.store.CountUsers()
	groups, _ := s.store.CountGroups()
	messages, _ := s.store.CountMessages()
	online, _ := s.store.CountOnlinePresence()
	userTrend, _ := s.store.UserTrendByDay(7)
	messageTrend, _ := s.store.MessageTrendByDay(7)
	return &Dashboard{
		Users: users, Groups: groups, Messages: messages, Online: online,
		UserTrend: userTrend, MessageTrend: messageTrend,
	}, nil
}

// ---- 用户管理 ----

// UserDTO 用户管理视图。
// Disabled：账号状态（0 正常 / 1 禁用）；Status：在线状态（0离线/1在线/2忙碌/3隐身）。
type UserDTO struct {
	UID       int64  `json:"uid"`
	Account   string `json:"account"`
	Nickname  string `json:"nickname"`
	Status    int8   `json:"status"`
	Disabled  int8   `json:"disabled"`
	CreatedAt int64  `json:"created_at"`
}

// TotalUsers 用户总数（keyword 非空时按关键词匹配计数；status > 0 时按状态过滤）。
func (s *Service) TotalUsers(keyword string, status int64) int64 {
	n, _ := s.store.CountUsersTotal(keyword, status)
	return n
}

// TotalGroups 群总数（keyword 非空时按关键词匹配计数）。
func (s *Service) TotalGroups(keyword string) int64 {
	n, _ := s.store.CountGroupsTotal(keyword)
	return n
}

// ListUsers 分页列出用户（keyword 匹配昵称/账号；status > 0 时按状态过滤）。
func (s *Service) ListUsers(offset, limit int, keyword string, status int64) ([]*UserDTO, error) {
	list, err := s.store.ListUsers(offset, limit, keyword, status)
	if err != nil {
		return nil, apperr.WrapInternal("获取用户列表失败", err)
	}
	out := make([]*UserDTO, 0, len(list))
	for _, u := range list {
		out = append(out, &UserDTO{
			UID: u.UID, Account: u.Account, Nickname: u.Nickname, Status: u.Status,
			Disabled: u.Disabled, CreatedAt: u.CreatedAt.Unix(),
		})
	}
	return out, nil
}

// DisableUser 禁用用户账号（审计 L7：operatorID 为操作管理员，写入审计日志）；若用户在线，立刻将其踢下线。
func (s *Service) DisableUser(operatorID, uid int64) error {
	if err := s.store.DisableUser(uid); err != nil {
		return err
	}
	log.L().Info("admin op: disable user", "admin_id", operatorID, "target_uid", uid)
	if s.kick != nil {
		s.kick(uid, "账号已被管理员禁用")
	}
	return nil
}

// EnableUser 启用用户账号（审计 L7：操作留痕）。
func (s *Service) EnableUser(operatorID, uid int64) error {
	if err := s.store.EnableUser(uid); err != nil {
		return err
	}
	log.L().Info("admin op: enable user", "admin_id", operatorID, "target_uid", uid)
	return nil
}

// ---- 群组管理 ----

// GroupDTO 群组管理视图。
type GroupDTO struct {
	GUID        int64  `json:"g_uid"`
	Name        string `json:"name"`
	OwnerUID    int64  `json:"owner_uid"`
	MemberCount int    `json:"member_count"`
	CreatedAt   int64  `json:"created_at"`
}

// ListGroups 分页列出群（keyword 匹配群名/群号）。
func (s *Service) ListGroups(offset, limit int, keyword string) ([]*GroupDTO, error) {
	list, err := s.store.ListAllGroups(offset, limit, keyword)
	if err != nil {
		return nil, apperr.WrapInternal("获取群列表失败", err)
	}
	out := make([]*GroupDTO, 0, len(list))
	for _, g := range list {
		out = append(out, &GroupDTO{
			GUID: g.GUID, Name: g.Name, OwnerUID: g.OwnerUID, MemberCount: g.MemberCount,
			CreatedAt: g.CreatedAt.Unix(),
		})
	}
	return out, nil
}

// DeleteGroup 解散群（审计 L7：操作留痕）。
// 完整清理（审计 P0）：删除前先取群信息与成员列表；删除后清理全体成员会话视图，
// 并逐个通知成员实时清理客户端会话，避免残留幽灵群会话。
func (s *Service) DeleteGroup(operatorID, gUID int64) error {
	// 删除前取会话 ID 与成员列表（删后无法再查）
	g, _ := s.store.GetGroupByGUID(gUID)
	members, _ := s.store.ListGroupMembers(gUID)
	if err := s.store.DeleteGroupByGUID(gUID); err != nil {
		return err
	}
	_ = s.store.DeleteAllGroupConversationViews(gUID)
	if g != nil && g.ConvID > 0 && s.dismissNotify != nil {
		for _, uid := range members {
			s.dismissNotify(uid, gUID, g.ConvID)
		}
	}
	log.L().Info("admin op: delete group", "admin_id", operatorID, "target_g_uid", gUID, "members", len(members))
	return nil
}

// ---- 群聊天记录 ----

// GroupMessageDTO 群聊天记录单条消息。
type GroupMessageDTO struct {
	MsgID      int64  `json:"msg_id"`
	Seq        int64  `json:"seq"`
	SenderUID  int64  `json:"sender_uid"`
	SenderName string `json:"sender_name"` // 发送者昵称（找不到时用账号/uid）
	Type       int8   `json:"type"`        // 1 文本 / 2 图片 / 3 文件 / 4 语音 / 5 视频 / 6 系统
	Content    string `json:"content"`     // 文本内容或媒体描述
	Extra      string `json:"extra"`       // 媒体元数据（URL 等）
	Status     int8   `json:"status"`      // 0 正常 / 1 已撤回
	CreatedAt  int64  `json:"created_at"`  // Unix 秒
}

// GetGroupMessages 查询群聊天记录：取最近 limit 条（按 seq 倒序翻页，beforeSeq=0 取最新）。
func (s *Service) GetGroupMessages(gUID, beforeSeq int64, limit int) ([]*GroupMessageDTO, error) {
	g, err := s.store.GetGroupByGUID(gUID)
	if err != nil {
		return nil, apperr.BadRequest("群不存在")
	}
	if g.ConvID == 0 {
		return []*GroupMessageDTO{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	list, err := s.store.ListMessagesBefore(g.ConvID, beforeSeq, limit)
	if err != nil {
		return nil, apperr.WrapInternal("查询群聊天记录失败", err)
	}
	// P1 优化：发送者昵称一次批量查，替代逐条 GetUserByUID 的 N+1
	uidSet := make(map[int64]struct{})
	for _, m := range list {
		if m.SenderUID > 0 {
			uidSet[m.SenderUID] = struct{}{}
		}
	}
	uids := make([]int64, 0, len(uidSet))
	for u := range uidSet {
		uids = append(uids, u)
	}
	names := s.store.GetUserNames(uids)
	out := make([]*GroupMessageDTO, 0, len(list))
	for _, m := range list {
		dto := &GroupMessageDTO{
			MsgID: m.ID, Seq: m.Seq, SenderUID: m.SenderUID,
			Type: m.Type, Content: previewText(m.Type, m.Content), Extra: m.Extra,
			Status: m.Status, CreatedAt: m.CreatedAt.Unix(),
		}
		// 发送者昵称（查不到时用 uid）
		if name := names[m.SenderUID]; name != "" {
			dto.SenderName = name
		} else {
			dto.SenderName = fmt.Sprintf("%d", m.SenderUID)
		}
		out = append(out, dto)
	}
	return out, nil
}

// previewText 生成消息摘要：文本直接返回，媒体消息给占位文案（不暴露原始 URL 语义）。
// FILE 消息中音频后缀（录音产物）展示为 [语音]，与消息服务 convPreview 一致。
func previewText(t int8, content string) string {
	switch t {
	case 1, 6: // 文本 / 系统消息
		return content
	case 2:
		return "[图片]"
	case 3:
		if isAudioContent(content) {
			return "[语音]"
		}
		return "[文件]"
	case 4:
		return "[语音]"
	case 5:
		return "[视频]"
	default:
		return content
	}
}

// adminAudioExts 可作为 FILE 类型发送的音频扩展名集合（与消息服务 audioExts 一致）。
var adminAudioExts = map[string]bool{
	".webm": true, ".m4a": true, ".aac": true, ".mp3": true,
	".wav": true, ".ogg": true, ".flac": true,
}

// isAudioContent 判断 FILE 消息 content（资源 URL）是否音频文件（按路径后缀识别）。
func isAudioContent(content string) bool {
	p := content
	if u, err := url.Parse(content); err == nil && u.Path != "" {
		p = u.Path
	} else {
		p, _, _ = strings.Cut(p, "?")
	}
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	if i := strings.LastIndex(p, "."); i >= 0 {
		return adminAudioExts[strings.ToLower(p[i:])]
	}
	return false
}

// ---- 客户端版本发布（检查更新） ----

// VersionDTO 版本信息对外视图。
type VersionDTO struct {
	Version      string `json:"version"`
	DownloadURL  string `json:"download_url"`
	Sha256       string `json:"sha256,omitempty"` // 安装包摘要，客户端自动更新下载后校验
	ReleaseNotes string `json:"release_notes"`
	Publisher    string `json:"publisher"`
	CreatedAt    int64  `json:"created_at"`
}

// PublishVersionReq 发布版本请求。
type PublishVersionReq struct {
	Version      string `json:"version"`
	DownloadURL  string `json:"download_url"`
	Sha256       string `json:"sha256"` // 必填：安装包 SHA-256，防客户端自动更新被投毒
	ReleaseNotes string `json:"release_notes"`
}

var versionRe = regexp.MustCompile(`^\d+\.\d+(\.\d+)?$`)
var sha256Re = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// PublishVersion 发布新版本：校验格式、版本号唯一；publisher 记录操作管理员用户名。
func (s *Service) PublishVersion(adminID int64, req *PublishVersionReq) error {
	version := strings.TrimSpace(req.Version)
	if !versionRe.MatchString(version) {
		return apperr.BadRequest("版本号格式不正确，应形如 1.1.0")
	}
	downloadURL := strings.TrimSpace(req.DownloadURL)
	if downloadURL == "" {
		return apperr.BadRequest("下载地址不能为空")
	}
	// 下载地址校验（审计 L2）：强制 https；白名单已注入时域名必须可信，
	// 防管理端被攻破/误操作后发布指向恶意域的安装包（客户端自动更新直接执行）
	if u, err := url.Parse(downloadURL); err != nil || u.Scheme != "https" || u.Host == "" {
		return apperr.BadRequest("下载地址必须为合法的 https 链接")
	} else if len(s.downloadHosts) > 0 && !s.downloadHosts[strings.ToLower(u.Hostname())] {
		return apperr.BadRequest("下载地址域名不在可信白名单内")
	}
	// 供应链安全（审计 P1）：强制要求安装包摘要，客户端自动更新下载后校验，防篡改/投毒
	sha := strings.ToLower(strings.TrimSpace(req.Sha256))
	if !sha256Re.MatchString(sha) {
		return apperr.BadRequest("必须提供安装包的 SHA-256（64 位十六进制；上传安装包时会自动计算）")
	}
	publisher := ""
	if a, err := s.store.GetAdminByID(adminID); err == nil && a != nil {
		publisher = a.Username
	}
	v := &mysql.AppVersion{
		ID:           s.genID(),
		Version:      version,
		DownloadURL:  downloadURL,
		Sha256:       sha,
		ReleaseNotes: strings.TrimSpace(req.ReleaseNotes),
		Publisher:    publisher,
	}
	if err := s.store.CreateAppVersion(v); err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			return apperr.Conflict("该版本号已发布")
		}
		return apperr.WrapInternal("发布版本失败", err)
	}
	// 审计日志（审计 L7）：版本发布属供应链操作，必须留痕
	log.L().Info("admin op: publish version", "admin_id", adminID, "version", version, "url", downloadURL)
	return nil
}

// ListVersions 版本列表（倒序，分页）。
func (s *Service) ListVersions(offset, limit int) ([]*VersionDTO, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	list, err := s.store.ListAppVersions(offset, limit)
	if err != nil {
		return nil, 0, apperr.WrapInternal("获取版本列表失败", err)
	}
	total, _ := s.store.CountAppVersions()
	out := make([]*VersionDTO, 0, len(list))
	for _, v := range list {
		out = append(out, toVersionDTO(v))
	}
	return out, total, nil
}

// LatestVersion 最新版本；未发布过任何版本时返回 nil。
func (s *Service) LatestVersion() (*VersionDTO, error) {
	v, err := s.store.GetLatestAppVersion()
	if err != nil {
		return nil, apperr.WrapInternal("查询最新版本失败", err)
	}
	if v == nil {
		return nil, nil
	}
	return toVersionDTO(v), nil
}

func toVersionDTO(v *mysql.AppVersion) *VersionDTO {
	return &VersionDTO{
		Version:      v.Version,
		DownloadURL:  v.DownloadURL,
		Sha256:       v.Sha256,
		ReleaseNotes: v.ReleaseNotes,
		Publisher:    v.Publisher,
		CreatedAt:    v.CreatedAt.Unix(),
	}
}
