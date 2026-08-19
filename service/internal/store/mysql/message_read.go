// message_read.go：已读状态表 DAO。
package mysql

import "database/sql"

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
