// message_read.go：已读状态表 DAO。
package mysql

import (
	"database/sql"
	"strings"
)

// GetLastReadSeq 查询某用户在某会话的最后已读 seq。
func (d *DB) GetLastReadSeq(uid, convID int64) (int64, error) {
	var seq int64
	err := d.QueryRow(`SELECT last_read_seq FROM message_reads WHERE uid = ? AND conv_id = ?`, uid, convID).Scan(&seq)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return seq, nil
}

// GetPeerReadSeqs 批量查询多个 (uid, conv_id) 对的已读游标（会话列表对端已读恢复，
// 替代逐会话 GetLastReadSeq 的 N+1）。pairs 元素为 [uid, convID]；
// 返回 conv_id -> last_read_seq，无记录的会话不在 map 中。
func (d *DB) GetPeerReadSeqs(pairs [][2]int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(pairs))
	if len(pairs) == 0 {
		return out, nil
	}
	var sb strings.Builder
	sb.WriteString("SELECT conv_id, last_read_seq FROM message_reads WHERE (uid, conv_id) IN (")
	args := make([]interface{}, 0, len(pairs)*2)
	for i, p := range pairs {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?,?)")
		args = append(args, p[0], p[1])
	}
	sb.WriteByte(')')
	rows, err := d.Query(sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var convID, seq int64
		if err := rows.Scan(&convID, &seq); err != nil {
			return nil, err
		}
		out[convID] = seq
	}
	return out, rows.Err()
}

// UpsertReadSeq 更新已读 seq（仅前向推进），并将该会话视图的未读计数清零。
// P2：未读数改由 unread_count 列维护，已读即清零，不再拉取时用 seq 差值计算。
func (d *DB) UpsertReadSeq(uid, convID, seq int64) error {
	_, err := d.Exec(`INSERT INTO message_reads (uid, conv_id, last_read_seq)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE last_read_seq = IF(last_read_seq < ?, ?, last_read_seq)`,
		uid, convID, seq, seq, seq)
	if err != nil {
		return err
	}
	_, err = d.Exec(`UPDATE conversations SET unread_count = 0 WHERE owner_uid = ? AND id = ?`, uid, convID)
	return err
}

// CountRead 统计某会话中已读游标 >= seq 的成员数（G14 群已读人数展示）。
func (d *DB) CountRead(convID, seq int64) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM message_reads WHERE conv_id = ? AND last_read_seq >= ?`, convID, seq).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}
