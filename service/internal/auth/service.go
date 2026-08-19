// Package auth 实现认证领域：注册、登录、JWT 刷新、退出、二维码登录。
// 数据访问走 mysql DAO，二维码状态与刷新令牌黑名单走 Redis。
package auth

import (
	"errors"
	"math/rand"
	"time"

	apperr "im/service/internal/pkg/err"
	"im/service/internal/pkg/jwt"
	"im/service/internal/pkg/log"
	"im/service/internal/pkg/pwd"
	"im/service/internal/store/mysql"
)

// Store 认证模块依赖的存储接口（便于测试 mock）。
type Store interface {
	CreateUser(u *mysql.User) error
	GetUserByAccount(account string) (*mysql.User, error)
	GetUserByUID(uid int64) (*mysql.User, error)
	TouchLastSeen(uid int64, status int8) error
	UIDExists(uid int64) (bool, error)
}

// Cache 认证模块依赖的缓存接口（二维码状态、刷新令牌黑名单、登录/注册限流）。
type Cache interface {
	Set(key string, value interface{}, expiration time.Duration) error
	Get(key string) (string, error)
	Del(key string) error
	IncrWithTTL(key string, ttl time.Duration) (int64, error)
}

// 限流参数（审计 P1：防密码爆破与批量注册）：
//   - 登录：同一账号窗口内最多尝试 loginLimitMax 次，超限拒绝至窗口结束；登录成功清零。
//   - 注册：同一 IP 每天最多 registerLimitMax 次。
const (
	loginLimitMax       = 10
	loginLimitWindow    = 15 * time.Minute
	registerLimitMax    = 20
	registerLimitWindow = 24 * time.Hour
)

func loginLimitKey(account string) string { return "auth:login:limit:" + account }
func registerLimitKey(ip string) string   { return "auth:reg:limit:" + ip }

// Service 认证服务。
type Service struct {
	store Store
	cache Cache
	jwt   *jwt.Manager
	idGen func() int64 // 内部主键生成器（雪花 ID）
	disconnectWS func(uid int64) // 可选：退出登录时断开该用户的 WS 连接
	pendingReqCheck func(uid int64) bool // 可选：登录时返回是否有待处理好友申请（红点状态）
}

// SetDisconnectWS 运行时注入"断开用户 WS 连接"的能力（由网关实现）。
func (s *Service) SetDisconnectWS(fn func(uid int64)) {
	s.disconnectWS = fn
}

// SetPendingReqCheck 运行时注入"是否有待处理好友申请"的判断（由上层注入 social 服务实现）。
func (s *Service) SetPendingReqCheck(fn func(uid int64) bool) {
	s.pendingReqCheck = fn
}

// New 创建认证服务。idGen 为雪花 ID 生成器，用于内部主键。
func New(store Store, cache Cache, jwtMgr *jwt.Manager, idGen func() int64) *Service {
	return &Service{store: store, cache: cache, jwt: jwtMgr, idGen: idGen}
}

// RegisterReq 注册请求。
type RegisterReq struct {
	Nickname string `json:"nickname"`
	Account  string `json:"account"`
	Password string `json:"password"`
}

// Register 注册新用户，成功后直接返回 token（注册即登录）。ip 用于注册频控。
func (s *Service) Register(req *RegisterReq, ip string) (*LoginResult, error) {
	// 注册频控（审计 P1）：同一 IP 每日上限，防批量注册垃圾号；限流不可用时放行不阻断主流程
	if ip != "" {
		if n, err := s.cache.IncrWithTTL(registerLimitKey(ip), registerLimitWindow); err == nil && n > registerLimitMax {
			return nil, apperr.TooManyRequests("今日注册次数已达上限，请明天再试")
		}
	}
	account := trimSpace(req.Account)
	nickname := trimSpace(req.Nickname)
	if account == "" || nickname == "" || req.Password == "" {
		return nil, apperr.BadRequest("昵称/账号/密码不能为空")
	}
	if len([]rune(nickname)) < 2 || len([]rune(nickname)) > 20 {
		return nil, apperr.BadRequest("昵称长度需在 2-20 个字符之间")
	}
	if len(req.Password) < 8 || !hasLetterAndDigit(req.Password) {
		return nil, apperr.BadRequest("密码至少 8 位，且同时包含字母和数字")
	}

	// 账号唯一性预检
	if _, err := s.store.GetUserByAccount(account); err == nil {
		return nil, apperr.Conflict("该账号已注册")
	} else if !errors.Is(err, mysql.ErrNotFound) {
		return nil, apperr.WrapInternal("查询账号失败", err)
	}

	hash, err := pwd.Hash(req.Password)
	if err != nil {
		return nil, apperr.WrapInternal("密码加密失败", err)
	}

	// 生成唯一业务 UID（10 位随机数字，首位 1~9）
	uid, err := s.genUniqueUID()
	if err != nil {
		return nil, err
	}

	user := &mysql.User{
		ID:           s.idGen(),
		UID:          uid,
		Account:      account,
		PasswordHash: hash,
		Email:        accountIfEmail(account),
		Nickname:     nickname,
		Status:       1,
	}
	if err := s.store.CreateUser(user); err != nil {
		return nil, apperr.WrapInternal("创建用户失败", err)
	}
	log.L().Info("user registered", "uid", uid)

	return s.issueTokens(uid)
}

// LoginReq 登录请求。
type LoginReq struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

// LoginResult 登录/刷新成功返回的令牌与用户信息。
type LoginResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // access token 有效期（秒）
	User         *UserInfo `json:"user"`
	// 是否有待处理的好友申请（导航栏红点状态，避免前端额外请求 /friends/requests）
	HasPendingFriendRequest bool `json:"has_pending_friend_request"`
}

// UserInfo 对外展示的用户信息。
type UserInfo struct {
	UID       int64  `json:"uid"`
	Account   string `json:"account"`
	Nickname  string `json:"nickname"`
	Avatar    string `json:"avatar,omitempty"`
	Signature string `json:"signature,omitempty"`
}

// Login 账号密码登录。ip 预留（当前限流按账号维度）。
func (s *Service) Login(req *LoginReq, ip string) (*LoginResult, error) {
	account := trimSpace(req.Account)
	if account == "" || req.Password == "" {
		return nil, apperr.BadRequest("账号或密码不能为空")
	}
	// 登录限流（审计 P1）：窗口内尝试次数超限直接拒绝，防密码爆破；限流不可用时放行
	if n, err := s.cache.IncrWithTTL(loginLimitKey(account), loginLimitWindow); err == nil && n > loginLimitMax {
		return nil, apperr.TooManyRequests("登录尝试过于频繁，请 15 分钟后再试")
	}
	u, err := s.store.GetUserByAccount(account)
	if err != nil {
		if errors.Is(err, mysql.ErrNotFound) {
			return nil, apperr.Unauthorized("账号或密码错误")
		}
		return nil, apperr.WrapInternal("查询用户失败", err)
	}
	if !pwd.Verify(u.PasswordHash, req.Password) {
		return nil, apperr.Unauthorized("账号或密码错误")
	}
	// 账号被禁用：禁止登录
	if u.Disabled == 1 {
		return nil, apperr.Forbidden("账号已被禁用，无法登录")
	}

	if err := s.store.TouchLastSeen(u.UID, 1); err != nil {
		log.L().Warn("touch last_seen failed", "uid", u.UID, "error", err)
	}
	// 登录成功：清零失败计数
	_ = s.cache.Del(loginLimitKey(account))
	return s.issueTokens(u.UID)
}

// issueTokens 签发 access + refresh，并组装登录结果。
func (s *Service) issueTokens(uid int64) (*LoginResult, error) {
	access, refresh, err := s.jwt.Generate(uid)
	if err != nil {
		return nil, apperr.WrapInternal("签发令牌失败", err)
	}
	u, err := s.store.GetUserByUID(uid)
	if err != nil {
		return nil, apperr.WrapInternal("查询用户信息失败", err)
	}
	// 账号被禁用：拒绝签发令牌（覆盖登录/刷新/二维码登录）
	if u.Disabled == 1 {
		return nil, apperr.Forbidden("账号已被禁用，无法登录")
	}
	// 是否有待处理好友申请（导航栏红点）；未注入判断时为 false
	hasPending := false
	if s.pendingReqCheck != nil {
		hasPending = s.pendingReqCheck(uid)
	}
	return &LoginResult{
		AccessToken:             access,
		RefreshToken:            refresh,
		ExpiresIn:               int64(s.jwt.AccessExpire().Seconds()),
		HasPendingFriendRequest: hasPending,
		User: &UserInfo{
			UID:       u.UID,
			Account:   u.Account,
			Nickname:  u.Nickname,
			Avatar:    u.Avatar,
			Signature: u.Signature,
		},
	}, nil
}

// RefreshReq 刷新请求。
type RefreshReq struct {
	RefreshToken string `json:"refresh_token"`
}

// Refresh 用 refresh token 换取新的 access + refresh（令牌轮换）。
func (s *Service) Refresh(req *RefreshReq) (*LoginResult, error) {
	token := trimSpace(req.RefreshToken)
	if token == "" {
		return nil, apperr.BadRequest("缺少 refresh token")
	}
	claims, err := s.jwt.Parse(token)
	if err != nil {
		return nil, apperr.Unauthorized("refresh token 无效或已过期")
	}
	if claims.Type != "refresh" {
		return nil, apperr.Unauthorized("令牌类型错误")
	}
	// 黑名单检查：已撤销的 refresh 直接拒绝
	if black, _ := s.cache.Get(refreshBlackKey(claims.ID)); black == "1" {
		return nil, apperr.Unauthorized("refresh token 已失效")
	}
	// 令牌轮换（审计 P1）：旧 refresh 立即拉黑至原有效期结束，
	// 防旧令牌泄露后在 30 天窗口内反复重放
	if ttl := time.Until(claims.ExpiresAt.Time); ttl > 0 {
		_ = s.cache.Set(refreshBlackKey(claims.ID), "1", ttl)
	}
	return s.issueTokens(claims.UID)
}

// LogoutReq 退出请求。
type LogoutReq struct {
	RefreshToken string `json:"refresh_token"`
}

// Logout 退出登录：撤销 refresh token（写入黑名单直至原有效期结束），并断开该用户的 WS 连接。
func (s *Service) Logout(req *LogoutReq) error {
	token := trimSpace(req.RefreshToken)
	if token == "" {
		return nil
	}
	claims, err := s.jwt.Parse(token)
	if err != nil {
		return nil
	}
	if claims.Type != "refresh" {
		return nil
	}
	ttl := time.Until(claims.ExpiresAt.Time)
	if ttl <= 0 {
		return nil
	}
	_ = s.cache.Set(refreshBlackKey(claims.ID), "1", ttl)
	// 用户退出登录：断开其所有 WS 连接
	if s.disconnectWS != nil {
		s.disconnectWS(claims.UID)
	}
	return nil
}

// genUniqueUID 生成 10 位随机数字 UID（首位 1~9），冲突时重试。
func (s *Service) genUniqueUID() (int64, error) {
	for i := 0; i < 10; i++ {
		// 1000000000 ~ 9999999999
		uid := int64(rand.Intn(9000000000) + 1000000000)
		exists, err := s.store.UIDExists(uid)
		if err != nil {
			return 0, apperr.WrapInternal("检查 UID 冲突失败", err)
		}
		if !exists {
			return uid, nil
		}
	}
	return 0, apperr.Internal("生成 UID 失败，请重试")
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && s[start] == ' ' || (start < end && s[start] == '\t') || (start < end && s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

// accountIfEmail 当账号为邮箱时返回其值（用于绑定找回密码邮箱），否则返回空。
func accountIfEmail(account string) string {
	if isEmail(account) {
		return account
	}
	return ""
}

func isEmail(s string) bool {
	at := -1
	for i, r := range s {
		if r == '@' {
			at = i
		}
	}
	if at <= 0 || at == len(s)-1 {
		return false
	}
	dot := false
	for _, r := range s[at:] {
		if r == '.' {
			dot = true
		}
	}
	return dot
}

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

func refreshBlackKey(jti string) string {
	return "auth:refresh:blacklist:" + jti
}
