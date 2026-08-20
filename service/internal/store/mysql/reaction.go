// reaction.go：消息表情回应 DAO（S6）。
// 独立表存储：消息不可变 + extra 2KB 上限，故不把 reactions 塞进消息行。
package mysql

import (
	"database/sql"
	"strings"
	"time"
)

// Reaction 表情回应。
type Reaction struct {
	ID        int64
	ConvID    int64
	MsgID     int64
	UID       int64
	Emoji     string
	CreatedAt time.Time
}

// AddReaction 添加表情回应（同一成员对同一消息同一 emoji 幂等：唯一键冲突时忽略）。
func (d *DB) AddReaction(id, convID, msgID, uid int64, emoji string) error {
	_, err := d.Exec(`INSERT IGNORE INTO message_reactions (id, conv_id, msg_id, uid, emoji)
		VALUES (?, ?, ?, ?, ?)`, id, convID, msgID, uid, emoji)
	return err
}

// RemoveReaction 移除表情回应（仅本人可移除自己的反应，调用方已鉴权）。
func (d *DB) RemoveReaction(convID, msgID, uid int64, emoji string) error {
	_, err := d.Exec(`DELETE FROM message_reactions WHERE conv_id = ? AND msg_id = ? AND uid = ? AND emoji = ?`,
		convID, msgID, uid, emoji)
	return err
}

// ListReactions 批量查询会话中多条消息的表情回应：返回 msg_id -> []Reaction（时间升序）。
// 用于历史/增量拉取时一次聚合，避免逐条 N+1。
func (d *DB) ListReactions(convID int64, msgIDs []int64) (map[int64][]Reaction, error) {
	out := make(map[int64][]Reaction)
	if len(msgIDs) == 0 {
		return out, nil
	}
	var sb strings.Builder
	sb.WriteString("SELECT id, conv_id, msg_id, uid, emoji, created_at FROM message_reactions WHERE conv_id = ? AND msg_id IN (")
	args := make([]interface{}, 0, len(msgIDs)+1)
	args = append(args, convID)
	for i, m := range msgIDs {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("?")
		args = append(args, m)
	}
	sb.WriteString(") ORDER BY created_at ASC, id ASC")
	rows, err := d.Query(sb.String(), args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Reaction
		if err := rows.Scan(&r.ID, &r.ConvID, &r.MsgID, &r.UID, &r.Emoji, &r.CreatedAt); err != nil {
			return out, err
		}
		out[r.MsgID] = append(out[r.MsgID], r)
	}
	return out, rows.Err()
}

// ListReactionEmojis 查询单条消息的全部表情回应（返回 []Reaction，供单条查询接口）。
func (d *DB) ListReactionEmojis(convID, msgID int64) ([]Reaction, error) {
	rows, err := d.Query(`SELECT id, conv_id, msg_id, uid, emoji, created_at FROM message_reactions
		WHERE conv_id = ? AND msg_id = ? ORDER BY created_at ASC, id ASC`, convID, msgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Reaction
	for rows.Next() {
		var r Reaction
		if err := rows.Scan(&r.ID, &r.ConvID, &r.MsgID, &r.UID, &r.Emoji, &r.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

var _ = sql.ErrNoRows // 保持 database/sql 引入（后续扩展预留）
