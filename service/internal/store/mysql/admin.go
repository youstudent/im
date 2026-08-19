// admin.go：admin_users 表 DAO + 后台统计查询。
package mysql

import (
	"fmt"
	"time"
)

// AdminUser 管理员。
type AdminUser struct {
	ID           int64
	Username     string
	PasswordHash string
	Nickname     string
	Role         int8
	Status       int8
	CreatedAt    time.Time
}

// GetAdminByUsername 按用户名查管理员。
func (d *DB) GetAdminByUsername(username string) (*AdminUser, error) {
	row := d.QueryRow(`SELECT id, username, password_hash, nickname, role, status, created_at FROM admin_users WHERE username = ?`, username)
	var a AdminUser
	if err := row.Scan(&a.ID, &a.Username, &a.PasswordHash, &a.Nickname, &a.Role, &a.Status, &a.CreatedAt); err != nil {
		return nil, err
	}
	return &a, nil
}

// GetAdminByID 按 ID 查管理员（版本发布记录发布者用）。
func (d *DB) GetAdminByID(id int64) (*AdminUser, error) {
	row := d.QueryRow(`SELECT id, username, password_hash, nickname, role, status, created_at FROM admin_users WHERE id = ?`, id)
	var a AdminUser
	if err := row.Scan(&a.ID, &a.Username, &a.PasswordHash, &a.Nickname, &a.Role, &a.Status, &a.CreatedAt); err != nil {
		return nil, err
	}
	return &a, nil
}

// CreateAdmin 创建管理员。
func (d *DB) CreateAdmin(a *AdminUser) error {
	_, err := d.Exec(`INSERT INTO admin_users (id, username, password_hash, nickname, role, status)
		VALUES (?, ?, ?, ?, ?, ?)`,
		a.ID, a.Username, a.PasswordHash, a.Nickname, a.Role, a.Status)
	return err
}

// CountAdmins 管理员数量。
func (d *DB) CountAdmins() (int64, error) {
	var n int64
	err := d.QueryRow(`SELECT COUNT(1) FROM admin_users`).Scan(&n)
	return n, err
}

// ListAdmins 管理员列表。
func (d *DB) ListAdmins() ([]*AdminUser, error) {
	rows, err := d.Query(`SELECT id, username, password_hash, nickname, role, status, created_at FROM admin_users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*AdminUser
	for rows.Next() {
		var a AdminUser
		if err := rows.Scan(&a.ID, &a.Username, &a.PasswordHash, &a.Nickname, &a.Role, &a.Status, &a.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, &a)
	}
	return list, rows.Err()
}

// CountUsers 用户总数。
func (d *DB) CountUsers() (int64, error) {
	var n int64
	err := d.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&n)
	return n, err
}

// CountGroups 群总数。
func (d *DB) CountGroups() (int64, error) {
	var n int64
	err := d.QueryRow("SELECT COUNT(1) FROM `groups`").Scan(&n)
	return n, err
}

// CountMessages 消息总数。
func (d *DB) CountMessages() (int64, error) {
	var total int64
	for i := 0; i < ShardCount; i++ {
		var n int64
		table := TableName("messages", i)
		if err := d.QueryRow("SELECT COUNT(1) FROM `" + table + "`").Scan(&n); err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

// CountOnlinePresence 在线用户数（由 Redis 统计，这里返回 MySQL 侧最近活跃占位）。
func (d *DB) CountOnlinePresence() (int64, error) {
	var n int64
	err := d.QueryRow(`SELECT COUNT(1) FROM users WHERE status = 1`).Scan(&n)
	return n, err
}

// UserTrendByDay 近 days 天每天新增用户数（下标 0 对应最早一天）。
func (d *DB) UserTrendByDay(days int) ([]int64, error) {
	return d.countByDay("users", days)
}

// MessageTrendByDay 近 days 天每天消息数（汇总 4 张消息分表，下标 0 对应最早一天）。
func (d *DB) MessageTrendByDay(days int) ([]int64, error) {
	var res []int64
	for i := 0; i < ShardCount; i++ {
		day, err := d.countByDay(TableName("messages", i), days)
		if err != nil {
			return nil, err
		}
		if res == nil {
			res = day
		} else {
			for j := range day {
				res[j] += day[j]
			}
		}
	}
	return res, nil
}

// countByDay 按天统计某表 created_at 在最近 days 天内的记录数。
func (d *DB) countByDay(table string, days int) ([]int64, error) {
	res := make([]int64, days)
	rows, err := d.Query(
		fmt.Sprintf("SELECT DATE(created_at) AS d, COUNT(1) FROM `%s` WHERE created_at >= ? GROUP BY d", table),
		time.Now().AddDate(0, 0, -(days - 1)).Format("2006-01-02 00:00:00"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// 建立 日期 -> 偏移量 的映射
	dayIdx := make(map[string]int)
	now := time.Now()
	for k := days - 1; k >= 0; k-- {
		dayIdx[now.AddDate(0, 0, -k).Format("2006-01-02")] = days - 1 - k
	}
	for rows.Next() {
		// 注意：DSN 带 parseTime=True 时，DATE(...) 会被驱动解析为 time.Time，
		// 若直接 Scan 到 string 会得到 RFC3339（如 "2026-08-17T00:00:00+08:00"），
		// 与 dayIdx 的 "2006-01-02" key 失配导致趋势全 0。这里按 time.Time 扫描再格式化。
		var date time.Time
		var n int64
		if err := rows.Scan(&date, &n); err != nil {
			return nil, err
		}
		if idx, ok := dayIdx[date.Format("2006-01-02")]; ok {
			res[idx] = n
		}
	}
	return res, rows.Err()
}
