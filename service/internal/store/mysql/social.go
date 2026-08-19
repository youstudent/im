// social.go：好友 / 群组 / 群成员 / 好友申请 / 通知 DAO。
package mysql

import (
	"database/sql"
	"time"
)

// SearchUsers 按账号/昵称模糊搜索用户（用于加好友）。
func (d *DB) SearchUsers(keyword string, limit int) ([]*User, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	like := "%" + keyword + "%"
	rows, err := d.Query(`SELECT `+userCols+` FROM users WHERE account LIKE ? OR nickname LIKE ? ORDER BY uid LIMIT ?`,
		like, like, limit)
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

// ---- 好友 ----

// Friend 好友关系。
type Friend struct {
	UID      int64
	FriendUID int64
	Remark   string
	CreatedAt time.Time
}

// AddFriend 双向写入好友关系。
func (d *DB) AddFriend(uid, friendUID int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT IGNORE INTO friends (uid, friend_uid) VALUES (?, ?), (?, ?)`,
		uid, friendUID, friendUID, uid); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// DeleteFriend 双向删除好友关系。
func (d *DB) DeleteFriend(uid, friendUID int64) error {
	_, err := d.Exec(`DELETE FROM friends WHERE (uid = ? AND friend_uid = ?) OR (uid = ? AND friend_uid = ?)`,
		uid, friendUID, friendUID, uid)
	return err
}

// ListFriends 列出某用户全部好友 uid。
func (d *DB) ListFriends(uid int64) ([]*Friend, error) {
	rows, err := d.Query(`SELECT uid, friend_uid, COALESCE(remark, '') AS remark, created_at FROM friends WHERE uid = ?`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Friend
	for rows.Next() {
		var f Friend
		if err := rows.Scan(&f.UID, &f.FriendUID, &f.Remark, &f.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, &f)
	}
	return list, rows.Err()
}

// AreFriends 判断是否互为好友。
func (d *DB) AreFriends(uid, friendUID int64) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM friends WHERE uid = ? AND friend_uid = ?`, uid, friendUID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// UpdateFriendRemark 更新我对好友的备注（仅改自己一侧的关系行，不影响对方）。
func (d *DB) UpdateFriendRemark(uid, friendUID int64, remark string) error {
	_, err := d.Exec(`UPDATE friends SET remark = ? WHERE uid = ? AND friend_uid = ?`, remark, uid, friendUID)
	return err
}

// ---- 好友申请 ----

// FriendRequest 好友申请。
type FriendRequest struct {
	ID        int64
	FromUID   int64
	ToUID     int64
	Message   string
	Status    int8 // 0 待处理 / 1 已接受 / 2 已拒绝
	CreatedAt time.Time
}

// CreateFriendRequest 创建好友申请（防重复：待处理中不重复创建）。
func (d *DB) CreateFriendRequest(r *FriendRequest) (int64, error) {
	var existing int64
	err := d.QueryRow(`SELECT id FROM friend_requests WHERE from_uid = ? AND to_uid = ? AND status = 0`,
		r.FromUID, r.ToUID).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	_, err = d.Exec(`INSERT INTO friend_requests (id, from_uid, to_uid, message, status) VALUES (?, ?, ?, ?, 0)`,
		r.ID, r.FromUID, r.ToUID, r.Message)
	return r.ID, err
}

// GetFriendRequest 按 ID 查询申请。
func (d *DB) GetFriendRequest(id int64) (*FriendRequest, error) {
	row := d.QueryRow(`SELECT id, from_uid, to_uid, COALESCE(message, '') AS message, status, created_at FROM friend_requests WHERE id = ?`, id)
	var r FriendRequest
	if err := row.Scan(&r.ID, &r.FromUID, &r.ToUID, &r.Message, &r.Status, &r.CreatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListFriendRequests 列出某用户收到的待处理申请。
func (d *DB) ListFriendRequests(toUID int64) ([]*FriendRequest, error) {
	rows, err := d.Query(`SELECT id, from_uid, to_uid, COALESCE(message, '') AS message, status, created_at FROM friend_requests WHERE to_uid = ? AND status = 0 ORDER BY created_at DESC`, toUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*FriendRequest
	for rows.Next() {
		var r FriendRequest
		if err := rows.Scan(&r.ID, &r.FromUID, &r.ToUID, &r.Message, &r.Status, &r.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, &r)
	}
	return list, rows.Err()
}

// UpdateFriendRequestStatus 更新申请状态。
func (d *DB) UpdateFriendRequestStatus(id int64, status int8) error {
	_, err := d.Exec(`UPDATE friend_requests SET status = ? WHERE id = ?`, status, id)
	return err
}

// ---- 群组 ----

// Group 群。
type Group struct {
	ID          int64
	GUID        int64
	Name        string
	OwnerUID    int64
	Announcement string
	MemberCount int
	Avatar      string
	ConvID      int64 // 群聊统一会话 ID（所有成员共享）
	CreatedAt   time.Time
}

// CreateGroup 创建群。
func (d *DB) CreateGroup(g *Group) error {
	_, err := d.Exec("INSERT INTO `groups` (id, g_uid, name, owner_uid, announcement, member_count, avatar, conv_id)\n\t\tVALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		g.ID, g.GUID, g.Name, g.OwnerUID, g.Announcement, g.MemberCount, g.Avatar, g.ConvID)
	return err
}

// GetGroupByGUID 按业务群号查群。
func (d *DB) GetGroupByGUID(gUID int64) (*Group, error) {
	row := d.QueryRow("SELECT id, g_uid, name, owner_uid, announcement, member_count, avatar, conv_id, created_at FROM `groups` WHERE g_uid = ?", gUID)
	g, err := scanGroup(row)
	if err != nil {
		return nil, err
	}
	return g, nil
}

// AddGroupMember 加入群成员。
func (d *DB) AddGroupMember(gUID, uid int64, role int8) error {
	_, err := d.Exec(`INSERT IGNORE INTO group_members (g_uid, uid, role) VALUES (?, ?, ?)`, gUID, uid, role)
	if err != nil {
		return err
	}
	_, err = d.Exec("UPDATE `groups` SET member_count = member_count + 1 WHERE g_uid = ?", gUID)
	return err
}

// RemoveGroupMember 移除群成员。
func (d *DB) RemoveGroupMember(gUID, uid int64) error {
	res, err := d.Exec(`DELETE FROM group_members WHERE g_uid = ? AND uid = ?`, gUID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		_, _ = d.Exec("UPDATE `groups` SET member_count = GREATEST(member_count - 1, 0) WHERE g_uid = ?", gUID)
	}
	return nil
}

// IsGroupMember 判断是否群成员。
func (d *DB) IsGroupMember(gUID, uid int64) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM group_members WHERE g_uid = ? AND uid = ?`, gUID, uid).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetGroupMemberRole 查询成员角色（0 群主 / 1 管理员 / 2 成员）；非成员返回 -1。
func (d *DB) GetGroupMemberRole(gUID, uid int64) (int8, error) {
	var role sql.NullInt64
	err := d.QueryRow(`SELECT role FROM group_members WHERE g_uid = ? AND uid = ?`, gUID, uid).Scan(&role)
	if err == sql.ErrNoRows {
		return -1, nil
	}
	if err != nil {
		return -1, err
	}
	if !role.Valid {
		return 2, nil
	}
	return int8(role.Int64), nil
}

// ListGroupMembers 列出群成员 uid。
func (d *DB) ListGroupMembers(gUID int64) ([]int64, error) {
	rows, err := d.Query(`SELECT uid FROM group_members WHERE g_uid = ?`, gUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		list = append(list, uid)
	}
	return list, rows.Err()
}

// ListUserGroups 列出某用户加入的群 g_uid。
func (d *DB) ListUserGroups(uid int64) ([]int64, error) {
	rows, err := d.Query(`SELECT g_uid FROM group_members WHERE uid = ?`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []int64
	for rows.Next() {
		var g int64
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		list = append(list, g)
	}
	return list, rows.Err()
}

// UpdateGroup 更新群名与群公告。
func (d *DB) UpdateGroup(gUID int64, name, announcement string) error {
	_, err := d.Exec("UPDATE `groups` SET name = ?, announcement = ? WHERE g_uid = ?", name, announcement, gUID)
	return err
}

// GUIDExists 检查群号是否占用。
func (d *DB) GUIDExists(gUID int64) (bool, error) {
	var n int
	err := d.QueryRow("SELECT COUNT(1) FROM `groups` WHERE g_uid = ?", gUID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func scanGroup(row interface{ Scan(...interface{}) error }) (*Group, error) {
	var g Group
	var ann sql.NullString
	var av sql.NullString
	var conv sql.NullInt64
	err := row.Scan(&g.ID, &g.GUID, &g.Name, &g.OwnerUID, &ann, &g.MemberCount, &av, &conv, &g.CreatedAt)
	if err != nil {
		return nil, err
	}
	g.Announcement = ann.String
	g.Avatar = av.String
	g.ConvID = conv.Int64
	return &g, nil
}

// ---- 通知 ----

// Notification 通知。
type Notification struct {
	ID        int64
	UID       int64
	Type      string
	Title     string
	Summary   string
	Action    string
	Read      int8
	CreatedAt time.Time
}

// CreateNotification 创建通知。
func (d *DB) CreateNotification(n *Notification) error {
	action := n.Action
	if action == "" {
		action = "null"
	}
	_, err := d.Exec("INSERT INTO notifications (id, uid, type, title, summary, action, `read`)\n\t\tVALUES (?, ?, ?, ?, ?, ?, ?)",
		n.ID, n.UID, n.Type, n.Title, n.Summary, action, n.Read)
	return err
}

// ListNotifications 列出某用户通知，按时间倒序。
func (d *DB) ListNotifications(uid int64) ([]*Notification, error) {
	rows, err := d.Query("SELECT id, uid, type, COALESCE(title,'') AS title, COALESCE(summary,'') AS summary, COALESCE(action,'') AS action, `read`, created_at FROM notifications WHERE uid = ? ORDER BY created_at DESC LIMIT 200", uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, n)
	}
	return list, rows.Err()
}

// MarkNotificationRead 标记单条已读。
func (d *DB) MarkNotificationRead(id, uid int64) error {
	_, err := d.Exec("UPDATE notifications SET `read` = 1 WHERE id = ? AND uid = ?", id, uid)
	return err
}

// MarkAllNotificationsRead 全部已读。
func (d *DB) MarkAllNotificationsRead(uid int64) error {
	_, err := d.Exec("UPDATE notifications SET `read` = 1 WHERE uid = ? AND `read` = 0", uid)
	return err
}

// CountUnreadNotifications 未读通知数。
func (d *DB) CountUnreadNotifications(uid int64) (int, error) {
	var n int
	err := d.QueryRow("SELECT COUNT(1) FROM notifications WHERE uid = ? AND `read` = 0", uid).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ClearNotifications 清空通知。
func (d *DB) ClearNotifications(uid int64) error {
	_, err := d.Exec(`DELETE FROM notifications WHERE uid = ?`, uid)
	return err
}

func scanNotification(row interface{ Scan(...interface{}) error }) (*Notification, error) {
	var n Notification
	var action interface{}
	err := row.Scan(&n.ID, &n.UID, &n.Type, &n.Title, &n.Summary, &action, &n.Read, &n.CreatedAt)
	if err != nil {
		return nil, err
	}
	if s, ok := action.([]byte); ok {
		n.Action = string(s)
	}
	return &n, nil
}
