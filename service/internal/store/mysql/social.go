// social.go：好友 / 群组 / 群成员 / 好友申请 / 通知 DAO。
package mysql

import (
	"database/sql"
	"errors"
	"strings"
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

// HasPendingFriendRequest 判断 fromUID 是否已向 toUID 发送过待处理的好友申请
// （搜索用户时前端据此置灰"发送验证申请"按钮，避免重复发送）。
func (d *DB) HasPendingFriendRequest(fromUID, toUID int64) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM friend_requests WHERE from_uid = ? AND to_uid = ? AND status = 0`,
		fromUID, toUID).Scan(&n)
	return n > 0, err
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
	InviteConfirm int8 // 邀请需确认（G7）：1=成员邀请需群主/管理员同意
	MuteAll       int8 // 全员禁言（G8）：1=仅群主/管理员可发言
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
	row := d.QueryRow("SELECT id, g_uid, name, owner_uid, announcement, member_count, avatar, conv_id, invite_confirm, mute_all, created_at FROM `groups` WHERE g_uid = ?", gUID)
	g, err := scanGroup(row)
	if err != nil {
		return nil, err
	}
	return g, nil
}

// GetGroupConvID 查询群的统一会话 ID（groups.conv_id，全体成员共享）。
// 群不存在返回 ErrNotFound；conv_id 为 NULL（迁移 0006 未执行的历史数据）时兜底用 groups 主键 id。
func (d *DB) GetGroupConvID(gUID int64) (int64, error) {
	var convID, groupID sql.NullInt64
	err := d.QueryRow("SELECT conv_id, id FROM `groups` WHERE g_uid = ?", gUID).Scan(&convID, &groupID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if convID.Valid && convID.Int64 > 0 {
		return convID.Int64, nil
	}
	return groupID.Int64, nil
}

// GetGroupsByGUIDs 批量按群号查群（群列表场景，替代逐个 GetGroupByGUID 的 N+1）。
// 不存在的群不在返回 map 中。
func (d *DB) GetGroupsByGUIDs(gUIDs []int64) map[int64]*Group {
	out := make(map[int64]*Group, len(gUIDs))
	if len(gUIDs) == 0 {
		return out
	}
	var sb strings.Builder
	sb.WriteString("SELECT id, g_uid, name, owner_uid, announcement, member_count, avatar, conv_id, invite_confirm, mute_all, created_at FROM `groups` WHERE g_uid IN (")
	args := make([]interface{}, 0, len(gUIDs))
	for i, g := range gUIDs {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("?")
		args = append(args, g)
	}
	sb.WriteString(")")
	rows, err := d.Query(sb.String(), args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return out
		}
		out[g.GUID] = g
	}
	return out
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

// UpdateGroupMemberRole 更新群成员角色（1 管理员 / 2 普通成员；群主不可通过此方法变更）。
func (d *DB) UpdateGroupMemberRole(gUID, uid int64, role int8) error {
	_, err := d.Exec(`UPDATE group_members SET role = ? WHERE g_uid = ? AND uid = ? AND role != 0`, role, gUID, uid)
	return err
}

// TransferGroupOwner 转让群主：更新群 owner_uid + 新群主 role=0 + 原群主 role=2。
// 三步非事务（单库低频操作，失败时服务端返回错误，客户端可重试）。
func (d *DB) TransferGroupOwner(gUID, oldOwnerUID, newOwnerUID int64) error {
	if _, err := d.Exec("UPDATE `groups` SET owner_uid = ? WHERE g_uid = ?", newOwnerUID, gUID); err != nil {
		return err
	}
	if _, err := d.Exec(`UPDATE group_members SET role = 0 WHERE g_uid = ? AND uid = ?`, gUID, newOwnerUID); err != nil {
		return err
	}
	_, err := d.Exec(`UPDATE group_members SET role = 2 WHERE g_uid = ? AND uid = ? AND role = 0`, gUID, oldOwnerUID)
	return err
}

// UpdateGroupMemberNickname 设置群内昵称（空字符串清除，回落用户昵称）。
func (d *DB) UpdateGroupMemberNickname(gUID, uid int64, nickname string) error {
	var v interface{}
	if nickname != "" {
		v = nickname
	}
	_, err := d.Exec(`UPDATE group_members SET nickname = ? WHERE g_uid = ? AND uid = ?`, v, gUID, uid)
	return err
}

// ListGroupMemberNicknames 批量查询群内昵称（仅返回已设置的，uid → nickname）。
func (d *DB) ListGroupMemberNicknames(gUID int64) (map[int64]string, error) {
	rows, err := d.Query(`SELECT uid, nickname FROM group_members WHERE g_uid = ? AND nickname IS NOT NULL AND nickname != ''`, gUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]string)
	for rows.Next() {
		var uid int64
		var nick string
		if err := rows.Scan(&uid, &nick); err != nil {
			return nil, err
		}
		out[uid] = nick
	}
	return out, rows.Err()
}

// ListGroupMemberRoles 批量查询群成员角色（uid → role），供群资料页展示群主/管理员标签。
func (d *DB) ListGroupMemberRoles(gUID int64) (map[int64]int8, error) {
	rows, err := d.Query(`SELECT uid, role FROM group_members WHERE g_uid = ?`, gUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]int8)
	for rows.Next() {
		var uid int64
		var role sql.NullInt64
		if err := rows.Scan(&uid, &role); err != nil {
			return nil, err
		}
		r := int8(2)
		if role.Valid {
			r = int8(role.Int64)
		}
		out[uid] = r
	}
	return out, rows.Err()
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

// ListGroupMembers 列出群成员 uid（按入群时间升序，客户端据此保持同角色内入群先后顺序）。
func (d *DB) ListGroupMembers(gUID int64) ([]int64, error) {
	rows, err := d.Query(`SELECT uid FROM group_members WHERE g_uid = ? ORDER BY join_time, uid`, gUID)
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

// GroupMemberCounts 批量统计多个群的成员数（群列表场景，
// 替代逐群 ListGroupMembers 的 N+1）。无成员的群不在返回 map 中。
func (d *DB) GroupMemberCounts(gUIDs []int64) map[int64]int {
	out := make(map[int64]int, len(gUIDs))
	if len(gUIDs) == 0 {
		return out
	}
	var sb strings.Builder
	sb.WriteString("SELECT g_uid, COUNT(1) FROM group_members WHERE g_uid IN (")
	args := make([]interface{}, 0, len(gUIDs))
	for i, g := range gUIDs {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("?")
		args = append(args, g)
	}
	sb.WriteString(") GROUP BY g_uid")
	rows, err := d.Query(sb.String(), args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var gUID int64
		var n int
		if err := rows.Scan(&gUID, &n); err != nil {
			return out
		}
		out[gUID] = n
	}
	return out
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

// UpdateGroupSettings 更新群设置开关（G7 入群确认 / G8 全员禁言）：
// inviteConfirm / muteAll 为 nil 表示不更新对应字段，取值仅允许 0/1。
func (d *DB) UpdateGroupSettings(gUID int64, inviteConfirm, muteAll *int8) error {
	if inviteConfirm == nil && muteAll == nil {
		return nil
	}
	sets := make([]string, 0, 2)
	args := make([]interface{}, 0, 3)
	if inviteConfirm != nil {
		sets = append(sets, "invite_confirm = ?")
		args = append(args, *inviteConfirm)
	}
	if muteAll != nil {
		sets = append(sets, "mute_all = ?")
		args = append(args, *muteAll)
	}
	args = append(args, gUID)
	_, err := d.Exec("UPDATE `groups` SET "+strings.Join(sets, ", ")+" WHERE g_uid = ?", args...)
	return err
}

// GetGroupMuteState 查询群全员禁言开关（G8 发送守卫用）。
func (d *DB) GetGroupMuteState(gUID int64) (int8, error) {
	var mute sql.NullInt64
	err := d.QueryRow("SELECT mute_all FROM `groups` WHERE g_uid = ?", gUID).Scan(&mute)
	if err != nil {
		return 0, err
	}
	if !mute.Valid {
		return 0, nil
	}
	return int8(mute.Int64), nil
}

// UpdateMemberMutedUntil 设置/解除成员禁言（G8）：until 为 unix 毫秒，0/负数=解除。
func (d *DB) UpdateMemberMutedUntil(gUID, uid int64, until int64) error {
	var v interface{}
	if until > 0 {
		v = until
	}
	_, err := d.Exec(`UPDATE group_members SET muted_until = ? WHERE g_uid = ? AND uid = ?`, v, gUID, uid)
	return err
}

// ListGroupMemberMutes 批量查询群成员禁言截止时间（uid → unix 毫秒，0=未禁言）。
func (d *DB) ListGroupMemberMutes(gUID int64) (map[int64]int64, error) {
	rows, err := d.Query(`SELECT uid, COALESCE(muted_until, 0) FROM group_members WHERE g_uid = ?`, gUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]int64)
	for rows.Next() {
		var uid, until int64
		if err := rows.Scan(&uid, &until); err != nil {
			return nil, err
		}
		out[uid] = until
	}
	return out, rows.Err()
}

// UpdateGroupMemberSaved 更新成员"保存到通讯录"开关（G10，仅操作自己的行）。
func (d *DB) UpdateGroupMemberSaved(gUID, uid int64, saved int8) error {
	_, err := d.Exec(`UPDATE group_members SET saved = ? WHERE g_uid = ? AND uid = ?`, saved, gUID, uid)
	return err
}

// ListGroupMemberSaved 批量查询某成员在多个群的"保存到通讯录"开关（g_uid → saved，群列表填充用）。
func (d *DB) ListGroupMemberSaved(uid int64, gUIDs []int64) (map[int64]int8, error) {
	out := make(map[int64]int8, len(gUIDs))
	if len(gUIDs) == 0 {
		return out, nil
	}
	var sb strings.Builder
	sb.WriteString("SELECT g_uid, saved FROM group_members WHERE uid = ? AND g_uid IN (")
	args := make([]interface{}, 0, len(gUIDs)+1)
	args = append(args, uid)
	for i, g := range gUIDs {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("?")
		args = append(args, g)
	}
	sb.WriteString(")")
	rows, err := d.Query(sb.String(), args...)
	if err != nil {
		return out, nil
	}
	defer rows.Close()
	for rows.Next() {
		var gUID int64
		var saved sql.NullInt64
		if err := rows.Scan(&gUID, &saved); err != nil {
			return out, nil
		}
		v := int8(1)
		if saved.Valid {
			v = int8(saved.Int64)
		}
		out[gUID] = v
	}
	return out, rows.Err()
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
	var invite sql.NullInt64
	var mute sql.NullInt64
	err := row.Scan(&g.ID, &g.GUID, &g.Name, &g.OwnerUID, &ann, &g.MemberCount, &av, &conv, &invite, &mute, &g.CreatedAt)
	if err != nil {
		return nil, err
	}
	g.Announcement = ann.String
	g.Avatar = av.String
	g.ConvID = conv.Int64
	if invite.Valid {
		g.InviteConfirm = int8(invite.Int64)
	}
	if mute.Valid {
		g.MuteAll = int8(mute.Int64)
	}
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
