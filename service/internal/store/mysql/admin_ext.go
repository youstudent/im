// admin_ext.go：管理后台的用户 / 群组分页查询与操作。
package mysql

import "strings"

// ListUsers 分页列出用户（按 uid 倒序）。
// keyword 非空时按昵称/账号模糊匹配；status > 0 时按账号状态过滤（1=正常，2=已禁用）。
func (d *DB) ListUsers(offset, limit int, keyword string, status int64) ([]*User, error) {
	q := `SELECT ` + userCols + ` FROM users`
	var conds []string
	var args []any
	if keyword != "" {
		conds = append(conds, `(nickname LIKE ? OR account LIKE ?)`)
		like := "%" + EscapeLike(keyword) + "%"
		args = append(args, like, like)
	}
	if status > 0 {
		// 前端用 2 表示「已禁用」，库内 disabled=1 表示禁用
		if status == 2 {
			conds = append(conds, `disabled = 1`)
		} else {
			conds = append(conds, `disabled = 0`)
		}
	}
	if len(conds) > 0 {
		q += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	q += ` ORDER BY uid DESC LIMIT ?, ?`
	args = append(args, offset, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, u)
	}
	return list, rows.Err()
}

// CountUsersTotal 用户总数（keyword 非空时按昵称/账号模糊匹配计数；status > 0 时按账号状态过滤）。
func (d *DB) CountUsersTotal(keyword string, status int64) (int64, error) {
	q := `SELECT COUNT(1) FROM users`
	var conds []string
	var args []any
	if keyword != "" {
		conds = append(conds, `(nickname LIKE ? OR account LIKE ?)`)
		like := "%" + EscapeLike(keyword) + "%"
		args = append(args, like, like)
	}
	if status > 0 {
		if status == 2 {
			conds = append(conds, `disabled = 1`)
		} else {
			conds = append(conds, `disabled = 0`)
		}
	}
	if len(conds) > 0 {
		q += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	var n int64
	err := d.QueryRow(q, args...).Scan(&n)
	return n, err
}

// DisableUser 禁用用户账号（disabled 置 1）。
func (d *DB) DisableUser(uid int64) error {
	_, err := d.Exec(`UPDATE users SET disabled = 1 WHERE uid = ?`, uid)
	return err
}

// EnableUser 启用用户账号（disabled 置 0）。
func (d *DB) EnableUser(uid int64) error {
	_, err := d.Exec(`UPDATE users SET disabled = 0 WHERE uid = ?`, uid)
	return err
}

// ListAllGroups 分页列出所有群。keyword 非空时按群名/群号模糊匹配。
// member_count 直接实时统计 group_members 表（不读 groups.member_count 字段，
// 该字段因 AddGroupMember 的 INSERT IGNORE 重复邀请场景可能虚高，此处保证展示准确）。
func (d *DB) ListAllGroups(offset, limit int, keyword string) ([]*Group, error) {
	// 列需与 scanGroup 的 9 个扫描目标一致（含 conv_id），否则 Scan 列数不匹配报错。
	q := `SELECT g.id, g.g_uid, g.name, g.owner_uid, g.announcement,
		(SELECT COUNT(1) FROM group_members gm WHERE gm.g_uid = g.g_uid) AS member_count,
		g.avatar, g.conv_id, g.created_at
		FROM ` + "`groups`" + ` g`
	var args []any
	if keyword != "" {
		q += " WHERE g.name LIKE ? OR CAST(g.g_uid AS CHAR) LIKE ?"
		like := "%" + EscapeLike(keyword) + "%"
		args = append(args, like, like)
	}
	q += " ORDER BY g.g_uid DESC LIMIT ?, ?"
	args = append(args, offset, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Group
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, g)
	}
	return list, rows.Err()
}

// CountGroupsTotal 群总数（keyword 非空时按群名/群号模糊匹配计数）。
func (d *DB) CountGroupsTotal(keyword string) (int64, error) {
	q := "SELECT COUNT(1) FROM `groups`"
	var args []any
	if keyword != "" {
		q += " WHERE name LIKE ? OR CAST(g_uid AS CHAR) LIKE ?"
		like := "%" + EscapeLike(keyword) + "%"
		args = append(args, like, like)
	}
	var n int64
	err := d.QueryRow(q, args...).Scan(&n)
	return n, err
}

// DeleteGroupByGUID 解散群（删除群与成员）。
func (d *DB) DeleteGroupByGUID(gUID int64) error {
	if _, err := d.Exec("DELETE FROM group_members WHERE g_uid = ?", gUID); err != nil {
		return err
	}
	_, err := d.Exec("DELETE FROM `groups` WHERE g_uid = ?", gUID)
	return err
}
