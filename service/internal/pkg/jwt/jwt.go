// Package jwt 提供 access / refresh token 的签发与校验（golang-jwt）。
package jwt

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"im/service/internal/config"
)

// Claims 自定义声明。
type Claims struct {
	UID     int64  `json:"uid"`
	Type    string `json:"type,omitempty"` // access / refresh
	IsAdmin bool   `json:"is_admin,omitempty"`
	jwt.RegisteredClaims
}

// Manager JWT 管理。
type Manager struct {
	secret        []byte
	issuer        string
	accessExpire  time.Duration
	refreshExpire time.Duration
	counter       int64 // 自增计数，用于生成唯一 token 标识（jti）
}

// New 创建 JWT 管理器。
func New(cfg config.JWT) *Manager {
	return &Manager{
		secret:        []byte(cfg.Secret),
		issuer:        cfg.Issuer,
		accessExpire:  time.Duration(cfg.AccessExpire) * time.Second,
		refreshExpire: time.Duration(cfg.RefreshExpire) * time.Second,
	}
}

// AccessExpire 返回 access token 有效期（秒）。
func (m *Manager) AccessExpire() time.Duration { return m.accessExpire }

// Generate 同时签发 access 与 refresh token。
func (m *Manager) Generate(uid int64) (access, refresh string, err error) {
	now := time.Now()
	access, err = m.sign(uid, "access", now, m.accessExpire)
	if err != nil {
		return "", "", err
	}
	refresh, err = m.sign(uid, "refresh", now, m.refreshExpire)
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

func (m *Manager) sign(uid int64, typ string, now time.Time, expire time.Duration) (string, error) {
	claims := Claims{
		UID:  uid,
		Type: typ,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   itoa(uid),
			ID:        m.newID(now, uid, typ), // 唯一 jti，用于 refresh token 撤销/黑名单
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expire)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

// newID 生成唯一 token 标识（jti）：时间戳 + uid + 类型，保证每个 token 独立。
func (m *Manager) newID(now time.Time, uid int64, typ string) string {
	return fmt.Sprintf("%d-%d-%s-%d", now.UnixNano(), uid, typ, atomic.AddInt64(&m.counter, 1))
}

// GenerateAdmin 签发管理员访问令牌（IsAdmin=true，使用 access 有效期）。
func (m *Manager) GenerateAdmin(adminID int64) (string, error) {
	now := time.Now()
	claims := Claims{
		UID:     adminID,
		Type:    "admin",
		IsAdmin: true,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   itoa(adminID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessExpire)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

// Parse 解析并校验 token，返回 Claims。
func (m *Manager) Parse(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
