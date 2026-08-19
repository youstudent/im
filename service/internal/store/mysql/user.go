// Package mysql 的数据访问层（DAO）。
// user.go 提供 users 表的读写操作，字段与 docs/IM系统架构设计.md 6.1 对齐。
package mysql

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ErrNotFound 记录不存在。
var ErrNotFound = errors.New("record not found")

// User 用户表映射，业务 UID 为 10 位随机数字（对外展示 workchatId）。
type User struct {
	ID           int64      // 内部主键（雪花 ID）
	UID          int64      // 业务 UID
	Account      string     // 登录账号（手机/邮箱）
	PasswordHash string     // 密码哈希（bcrypt）
	Email        string     // 绑定邮箱（可空）
	Nickname     string     // 昵称
	Avatar       string     // 头像 URL
	Signature    string     // 个性签名
	Status       int8       // 在线状态
	Disabled     int8       // 账号状态：0 正常 / 1 禁用
	LastSeenAt   *time.Time // 最后上线时间
	CreatedAt    time.Time  // 注册时间
}

const userCols = `id, uid, account, password_hash, email, nickname, avatar, signature, status, disabled, last_seen_at, created_at`

func scanUser(row interface{ Scan(...interface{}) error }) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.UID, &u.Account, &u.PasswordHash, &u.Email,
		&u.Nickname, &u.Avatar, &u.Signature, &u.Status, &u.Disabled, &u.LastSeenAt, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

// CreateUser 插入新用户，返回完整记录。
func (d *DB) CreateUser(u *User) error {
	_, err := d.Exec(`INSERT INTO users (id, uid, account, password_hash, email, nickname, avatar, signature, status, disabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.UID, u.Account, u.PasswordHash, u.Email, u.Nickname, u.Avatar, u.Signature, u.Status, u.Disabled)
	return err
}

// GetUserByID 按内部主键查询用户。
func (d *DB) GetUserByID(id int64) (*User, error) {
	row := d.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id)
	return scanUser(row)
}

// GetUserByUID 按业务 UID 查询用户。
func (d *DB) GetUserByUID(uid int64) (*User, error) {
	row := d.QueryRow(`SELECT `+userCols+` FROM users WHERE uid = ?`, uid)
	return scanUser(row)
}

// GetUserByAccount 按登录账号（手机/邮箱）查询用户，含密码哈希。
func (d *DB) GetUserByAccount(account string) (*User, error) {
	row := d.QueryRow(`SELECT `+userCols+` FROM users WHERE account = ?`, account)
	return scanUser(row)
}

// TouchLastSeen 更新最后上线时间与在线状态。
func (d *DB) TouchLastSeen(uid int64, status int8) error {
	_, err := d.Exec(`UPDATE users SET last_seen_at = ?, status = ? WHERE uid = ?`,
		time.Now(), status, uid)
	return err
}

// UIDExists 检查业务 UID 是否已占用（唯一索引兜底，冲突时重试）。
func (d *DB) UIDExists(uid int64) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM users WHERE uid = ?`, uid).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetUserNames 批量获取昵称（审计 P1：历史列表一次查出，消除逐条 GetUserName 的 N+1）。
func (d *DB) GetUserNames(uids []int64) map[int64]string {
	out := make(map[int64]string)
	if len(uids) == 0 {
		return out
	}
	var sb strings.Builder
	sb.WriteString("SELECT uid, nickname FROM users WHERE uid IN (")
	args := make([]interface{}, 0, len(uids))
	for i, uid := range uids {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("?")
		args = append(args, uid)
	}
	sb.WriteString(")")
	rows, err := d.Query(sb.String(), args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var uid int64
		var name string
		if rows.Scan(&uid, &name) == nil {
			out[uid] = name
		}
	}
	return out
}
