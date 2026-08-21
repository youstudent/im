// conversation.go：会话表 DAO。每个用户一个会话视图。
package mysql

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Conversation 会话记录。
type Conversation struct {
	ID          int64     // 会话 ID（雪花）
	Type        int8      // 1 单聊 / 2 群聊
	OwnerUID    int64     // 归属用户 uid
	TargetID    int64     // 对方 uid 或群 g_uid
	LastMsgID   int64     // 最后消息 id
	LastMsgText string    // 最后消息摘要
	LastMsgTime *time.Time // 最后消息时间（排序）
	LastSyncedSeq int64   // 已同步最大 seq
	UnreadCount   int64   // 未读消息数（发消息累加，已读清零，撤回递减）
	Muted       int8      // 免打扰
	Pinned      int8      // 置顶
	LastSenderUID  int64  // 最后消息发送者 uid（0 表示系统/无，群聊列表前缀用）
	LastSenderName string // 最后消息发送者昵称快照
	CreatedAt   time.Time
}

const convCols = `id, type, owner_uid, target_id, COALESCE(last_msg_id,0) AS last_msg_id, COALESCE(last_msg_text,'') AS last_msg_text, last_msg_time, last_synced_seq, unread_count, muted, pinned, COALESCE(last_sender_uid,0) AS last_sender_uid, COALESCE(last_sender_name,'') AS last_sender_name, created_at`

// GetOrCreateConversation 获取或创建会话视图。
// newID 为新建会话时使用的内部主键（雪花 ID，由调用方生成）。
func (d *DB) GetOrCreateConversation(ownerUID, targetID int64, typ int8, newID int64) (*Conversation, error) {
	c, err := d.GetConversation(ownerUID, targetID)
	if err == nil {
		return c, nil
	}
	if err != ErrNotFound {
		return nil, err
	}
	now := time.Now()
	_, err = d.Exec(`INSERT INTO conversations (id, type, owner_uid, target_id, created_at)
		VALUES (?, ?, ?, ?, ?)`, newID, typ, ownerUID, targetID, now)
	if err != nil {
		// 并发下可能已存在，重查
		return d.GetConversation(ownerUID, targetID)
	}
	return &Conversation{ID: newID, Type: typ, OwnerUID: ownerUID, TargetID: targetID, CreatedAt: now}, nil
}

// EnsureConversationID 确保 owner 侧会话存在且使用指定 convID（单聊双方共享同一 conv_id 的关键）。
// 单聊中，接收方会话视图必须与发送方使用同一 conv_id，否则历史消息/已读游标无法互通。
// 若该 owner 侧会话已存在但 conv_id 不同（历史 bug 数据），迁移其消息与已读记录到新 conv_id 后更新。
func (d *DB) EnsureConversationID(ownerUID, targetID int64, typ int8, convID int64) (*Conversation, error) {
	c, err := d.GetConversation(ownerUID, targetID)
	if err == nil {
		if c.ID == convID {
			return c, nil
		}
		if err := d.migrateConversation(c.ID, convID); err != nil {
			return nil, err
		}
		return d.GetConversation(ownerUID, targetID)
	}
	if err != ErrNotFound {
		return nil, err
	}
	now := time.Now()
	_, err = d.Exec(`INSERT INTO conversations (id, type, owner_uid, target_id, created_at)
		VALUES (?, ?, ?, ?, ?)`, convID, typ, ownerUID, targetID, now)
	if err != nil {
		return d.GetConversation(ownerUID, targetID)
	}
	return &Conversation{ID: convID, Type: typ, OwnerUID: ownerUID, TargetID: targetID, CreatedAt: now}, nil
}

// migrateConversation 把旧会话 ID 下的已读游标与消息迁移到新会话 ID。
// 处理单聊双方此前使用不同 conv_id 的历史脏数据，使对端会话统一到同一 conv_id。
func (d *DB) migrateConversation(oldID, newID int64) error {
	if oldID == newID {
		return nil
	}
	oldShard := TableName("messages", Shard(oldID, ShardCount))
	newShard := TableName("messages", Shard(newID, ShardCount))
	// 已读游标迁移
	_, _ = d.Exec(`UPDATE message_reads SET conv_id = ? WHERE conv_id = ?`, newID, oldID)
	// 消息迁移：同表直接 UPDATE；跨表先删目标表同会话记录再插入，避免唯一键冲突
	if oldShard == newShard {
		_, err := d.Exec(`UPDATE `+oldShard+` SET conv_id = ? WHERE conv_id = ?`, newID, oldID)
		return err
	}
	_, _ = d.Exec(`DELETE FROM `+newShard+` WHERE conv_id = ?`, newID)
	if _, err := d.Exec(`INSERT INTO `+newShard+` (id, conv_id, seq, sender_uid, type, content, extra, status, created_at)
		SELECT id, ?, seq, sender_uid, type, content, extra, status, created_at FROM `+oldShard+` WHERE conv_id = ?`, newID, oldID); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM `+oldShard+` WHERE conv_id = ?`, oldID)
	return err
}

// GetConversation 按 owner + target 查询会话。
func (d *DB) GetConversation(ownerUID, targetID int64) (*Conversation, error) {
	row := d.QueryRow(`SELECT `+convCols+` FROM conversations WHERE owner_uid = ? AND target_id = ?`, ownerUID, targetID)
	c, err := scanConv(row)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetConversationByID 按会话 ID 查询会话（用于已读回执等只持有 conv_id 的场景）。
func (d *DB) GetConversationByID(convID int64) (*Conversation, error) {
	row := d.QueryRow(`SELECT `+convCols+` FROM conversations WHERE id = ?`, convID)
	c, err := scanConv(row)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// IsConversationMember 判断用户是否拥有该会话的视图（会话参与者）。
// 审计 P0：历史拉取前的归属校验，防越权读取任意会话。
func (d *DB) IsConversationMember(uid, convID int64) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM conversations WHERE owner_uid = ? AND id = ?`, uid, convID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListConversations 列出某用户全部会话，按最后消息时间倒序。
func (d *DB) ListConversations(ownerUID int64) ([]*Conversation, error) {
	rows, err := d.Query(`SELECT `+convCols+` FROM conversations WHERE owner_uid = ? ORDER BY last_msg_time DESC, id DESC`, ownerUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Conversation
	for rows.Next() {
		c, err := scanConv(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// ListConversationsChangedSince 列出某用户在 since 之后有变化的会话（客户端差量刷新，减压服务端）。
// 无消息的空会话（last_msg_time 为 NULL）一并返回，保证新建会话不被遗漏；命中 idx_owner_last 索引。
func (d *DB) ListConversationsChangedSince(ownerUID int64, since time.Time) ([]*Conversation, error) {
	rows, err := d.Query(`SELECT `+convCols+` FROM conversations
		WHERE owner_uid = ? AND (last_msg_time IS NULL OR last_msg_time > ?)
		ORDER BY last_msg_time DESC, id DESC`, ownerUID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Conversation
	for rows.Next() {
		c, err := scanConv(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// UpdateConversationLastMsg 更新会话最后消息信息；senderUID/senderName 记录发送者
//（单聊列表不拼前缀但保持字段准确；系统消息/撤回传 0/空）。
func (d *DB) UpdateConversationLastMsg(ownerUID, targetID int64, lastMsgID int64, text string, t time.Time, senderUID int64, senderName string) error {
	_, err := d.Exec(`UPDATE conversations SET last_msg_id = ?, last_msg_text = ?, last_msg_time = ?, last_sender_uid = ?, last_sender_name = ? WHERE owner_uid = ? AND target_id = ?`,
		lastMsgID, text, t, senderUID, senderName, ownerUID, targetID)
	return err
}

// DeleteGroupConversationView 删除指定用户的群会话视图（退群清理）。
func (d *DB) DeleteGroupConversationView(ownerUID, gUID int64) error {
	_, err := d.Exec(`DELETE FROM conversations WHERE owner_uid = ? AND target_id = ? AND type = 2`, ownerUID, gUID)
	return err
}

// DeleteConversationByID 删除用户自己的会话视图行（用户主动删除会话；仅删视图不删消息，
// 再次收发消息时 GetOrCreateConversation 自动重建）。
func (d *DB) DeleteConversationByID(ownerUID, convID int64) error {
	_, err := d.Exec(`DELETE FROM conversations WHERE owner_uid = ? AND id = ?`, ownerUID, convID)
	return err
}

// DeleteAllGroupConversationViews 删除某群的全体成员会话视图（管理端解散群清理，
// 避免成员会话列表残留幽灵群会话）。
func (d *DB) DeleteAllGroupConversationViews(gUID int64) error {
	_, err := d.Exec(`DELETE FROM conversations WHERE target_id = ? AND type = 2`, gUID)
	return err
}

// UpdateConversationSettings 更新会话置顶/免打扰（仅归属者本人视图；nil 字段不更新）。
func (d *DB) UpdateConversationSettings(ownerUID, convID int64, pinned, muted *int8) error {
	var sets []string
	var args []interface{}
	if pinned != nil {
		sets = append(sets, "pinned = ?")
		args = append(args, *pinned)
	}
	if muted != nil {
		sets = append(sets, "muted = ?")
		args = append(args, *muted)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, convID, ownerUID)
	_, err := d.Exec(`UPDATE conversations SET `+strings.Join(sets, ", ")+` WHERE id = ? AND owner_uid = ?`, args...)
	return err
}

// UpdateConversationSyncedSeq 更新已同步游标。
func (d *DB) UpdateConversationSyncedSeq(ownerUID, targetID, seq int64) error {
	_, err := d.Exec(`UPDATE conversations SET last_synced_seq = ? WHERE owner_uid = ? AND target_id = ? AND last_synced_seq < ?`,
		seq, ownerUID, targetID, seq)
	return err
}

// ---- 群聊批量操作（审计 P1：替代逐成员查询+更新，消除写放大） ----

// EnsureGroupConversationView 确保发送者的群会话视图存在且使用群统一 conv_id（groups.conv_id）。
// 用户删除群会话后再次发言时，视图行必须以统一 conv_id 重建而非生成新 conv_id：
// 否则消息会落入分叉会话（群历史割裂），且其他成员收到的推送帧 conv_id 无法映射到
// 已有会话项，会在本地重复建立第二个群会话。
// 已存在错误 conv_id 的视图行（旧版 bug 分叉脏数据）时，修复行 ID 并迁移分叉消息。
func (d *DB) EnsureGroupConversationView(ownerUID, gUID, convID int64) (*Conversation, error) {
	c, err := d.GetConversation(ownerUID, gUID)
	if err == nil {
		if c.ID == convID {
			return c, nil
		}
		if err := d.repairDivergedGroupView(ownerUID, c.ID, convID); err != nil {
			return nil, err
		}
		return d.GetConversation(ownerUID, gUID)
	}
	if err != ErrNotFound {
		return nil, err
	}
	now := time.Now()
	_, err = d.Exec(`INSERT INTO conversations (id, type, owner_uid, target_id, created_at)
		VALUES (?, 2, ?, ?, ?)`, convID, ownerUID, gUID, now)
	if err != nil {
		// 并发下可能已被重建，重查
		return d.GetConversation(ownerUID, gUID)
	}
	return &Conversation{ID: convID, Type: 2, OwnerUID: ownerUID, TargetID: gUID, CreatedAt: now}, nil
}

// repairDivergedGroupView 修复分叉的群会话视图（owner 的视图行 id 偏离群统一 conv_id）：
// 1) 分叉期间误存的消息按 seq 升序读出，以统一会话当前最大 seq 为基数重排后迁入（避免 uk_conv_seq 冲突）；
// 2) 已读游标按同一偏移平移（同 uid 在两侧都有记录时保留统一会话侧）；
// 3) 视图行 id 改回统一 conv_id（conversations 主键为 (owner_uid, target_id)，id 列非唯一可更新）。
func (d *DB) repairDivergedGroupView(ownerUID, oldID, newID int64) error {
	oldShard := msgTable(oldID)
	newShard := msgTable(newID)
	rows, err := d.Query(`SELECT `+msgCols+` FROM `+oldShard+` WHERE conv_id = ? ORDER BY seq`, oldID)
	if err != nil {
		return err
	}
	var msgs []*Message
	for rows.Next() {
		m, err := scanMsg(rows)
		if err != nil {
			rows.Close()
			return err
		}
		msgs = append(msgs, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var base int64
	if err := d.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM `+newShard+` WHERE conv_id = ?`, newID).Scan(&base); err != nil {
		return err
	}
	for i, m := range msgs {
		var extraArg interface{}
		if m.Extra != "" {
			extraArg = m.Extra
		}
		if _, err := d.Exec(`INSERT INTO `+newShard+` (id, conv_id, seq, sender_uid, type, content, extra, status, created_at)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			m.ID, newID, base+int64(i)+1, m.SenderUID, m.Type, m.Content, extraArg, m.Status, m.CreatedAt); err != nil {
			return err
		}
	}
	if len(msgs) > 0 {
		if _, err := d.Exec(`DELETE FROM `+oldShard+` WHERE conv_id = ?`, oldID); err != nil {
			return err
		}
	}
	// 已读游标平移；同 uid 冲突时（两边都有记录）保留统一会话侧
	_, _ = d.Exec(`DELETE FROM message_reads WHERE conv_id = ? AND uid IN (SELECT uid FROM (SELECT uid FROM message_reads WHERE conv_id = ?) t)`, oldID, newID)
	_, _ = d.Exec(`UPDATE message_reads SET conv_id = ?, last_read_seq = last_read_seq + ? WHERE conv_id = ?`, newID, base, oldID)
	_, err = d.Exec(`UPDATE conversations SET id = ? WHERE id = ? AND owner_uid = ?`, newID, oldID, ownerUID)
	return err
}

// EnsureGroupConversationViews 批量确保群成员会话视图存在（单条 INSERT IGNORE）。
// 所有成员共享同一 convID；主键为 (owner_uid, target_id)，重复成员自动忽略。
func (d *DB) EnsureGroupConversationViews(convID, gUID int64, memberUIDs []int64) error {
	if len(memberUIDs) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("INSERT IGNORE INTO conversations (id, type, owner_uid, target_id) VALUES ")
	args := make([]interface{}, 0, len(memberUIDs)*3)
	for i, uid := range memberUIDs {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("(?, 2, ?, ?)")
		args = append(args, convID, uid, gUID)
	}
	_, err := d.Exec(sb.String(), args...)
	return err
}

// UpdateGroupConversationsLastMsg 批量更新群全部成员视图的最后消息（单条 SQL）。
// senderUID/senderName 记录最后消息发送者（系统消息传 0/空），客户端列表据此拼 "发送者: 内容" 前缀。
func (d *DB) UpdateGroupConversationsLastMsg(gUID, lastMsgID int64, text string, t time.Time, senderUID int64, senderName string) error {
	_, err := d.Exec(`UPDATE conversations SET last_msg_id = ?, last_msg_text = ?, last_msg_time = ?, last_sender_uid = ?, last_sender_name = ?
		WHERE type = 2 AND target_id = ?`, lastMsgID, text, t, senderUID, senderName, gUID)
	return err
}

// UpdateGroupConversationsSyncedSeq 批量更新群成员视图的已同步游标（排除发送者，避免其误显未读）。
func (d *DB) UpdateGroupConversationsSyncedSeq(gUID, seq, excludeUID int64) error {
	_, err := d.Exec(`UPDATE conversations SET last_synced_seq = ?
		WHERE type = 2 AND target_id = ? AND owner_uid != ? AND last_synced_seq < ?`,
		seq, gUID, excludeUID, seq)
	return err
}

// UpdateGroupConversationsLastMsgExcept 批量更新群成员视图的最后消息，排除指定用户（单条 SQL）。
// 撤回场景：发送方视图与其余成员视图需展示不同文案，故拆两条 SQL（发送方用 UpdateConversationLastMsg）。
// senderUID/senderName 同 UpdateGroupConversationsLastMsg（撤回文案无发送者语义，传 0/空避免列表加前缀）。
func (d *DB) UpdateGroupConversationsLastMsgExcept(gUID, excludeUID, lastMsgID int64, text string, t time.Time, senderUID int64, senderName string) error {
	_, err := d.Exec(`UPDATE conversations SET last_msg_id = ?, last_msg_text = ?, last_msg_time = ?, last_sender_uid = ?, last_sender_name = ?
		WHERE type = 2 AND target_id = ? AND owner_uid != ?`, lastMsgID, text, t, senderUID, senderName, gUID, excludeUID)
	return err
}

// ---- 未读计数维护（P2：unread_count 列替代 seq 差值，消除撤回场景虚高） ----

// BumpConversationUnread 调整单个会话视图的未读数（delta 可为负），GREATEST 保底不为负。
func (d *DB) BumpConversationUnread(ownerUID, targetID, delta int64) error {
	_, err := d.Exec(`UPDATE conversations SET unread_count = GREATEST(unread_count + ?, 0)
		WHERE owner_uid = ? AND target_id = ?`, delta, ownerUID, targetID)
	return err
}

// BumpGroupConversationsUnread 批量调整群成员视图的未读数（排除指定用户，delta 可为负）。
func (d *DB) BumpGroupConversationsUnread(gUID, excludeUID, delta int64) error {
	_, err := d.Exec(`UPDATE conversations SET unread_count = GREATEST(unread_count + ?, 0)
		WHERE type = 2 AND target_id = ? AND owner_uid != ?`, delta, gUID, excludeUID)
	return err
}

func scanConv(row interface{ Scan(...interface{}) error }) (*Conversation, error) {
	var c Conversation
	err := row.Scan(&c.ID, &c.Type, &c.OwnerUID, &c.TargetID, &c.LastMsgID, &c.LastMsgText,
		&c.LastMsgTime, &c.LastSyncedSeq, &c.UnreadCount, &c.Muted, &c.Pinned,
		&c.LastSenderUID, &c.LastSenderName, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}
