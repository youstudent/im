// message.go：消息表 DAO（分表 messages_0 ~ messages_3，路由 conv_id % 4）。
package mysql

import (
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ShardCount 消息分表数量。
const ShardCount = 4

// Message 消息记录。
type Message struct {
	ID         int64     // 消息 ID（雪花，全局唯一）
	ConvID     int64     // 所属会话
	Seq        int64     // 会话内序号
	SenderUID  int64     // 发送者 uid
	Type       int8      // 1 text / 2 image / 3 file / 4 voice / 5 video / 6 system
	Content    string    // 文本或结构化 JSON
	Extra      string    // 扩展 JSON（可为空字符串）
	Status     int8      // 0 正常 / 1 已撤回
	CreatedAt  time.Time // 发送时间
}

// msgCols 消息表公共列。
const msgCols = `id, conv_id, seq, sender_uid, type, COALESCE(content,'') AS content, COALESCE(extra,'') AS extra, status, created_at`

// msgTable 返回 convID 对应的分表名（路由算法见 Shard，FNV 散列保证分布均匀）。
func msgTable(convID int64) string {
	return TableName("messages", Shard(convID, ShardCount))
}

// userNameCache 昵称短 TTL 缓存（P1 优化）：发送回显/撤回等单发场景频繁查同一发送者，
// 缓存 5 分钟降低查库量；昵称修改最多延迟一个 TTL 生效（当前无昵称修改接口，无失效问题）。
var (
	userNameCacheMu sync.Mutex
	userNameCache   = make(map[int64]userNameEntry)
)

type userNameEntry struct {
	name  string
	until time.Time
}

const userNameCacheTTL = 5 * time.Minute

// GetUserName 按业务 uid 查询用户昵称（用于消息展示发送者名），用户不存在返回空串。
func (d *DB) GetUserName(uid int64) string {
	if uid <= 0 {
		return ""
	}
	now := time.Now()
	userNameCacheMu.Lock()
	if e, ok := userNameCache[uid]; ok && now.Before(e.until) {
		userNameCacheMu.Unlock()
		return e.name
	}
	// 超容量时整体清空（简单高效，避免无界增长）
	if len(userNameCache) > 10000 {
		userNameCache = make(map[int64]userNameEntry)
	}
	userNameCacheMu.Unlock()
	u, err := d.GetUserByUID(uid)
	name := ""
	if err == nil {
		name = u.Nickname
	}
	userNameCacheMu.Lock()
	userNameCache[uid] = userNameEntry{name: name, until: now.Add(userNameCacheTTL)}
	userNameCacheMu.Unlock()
	return name
}

// CreateMessage 插入一条消息，并返回其 seq。
// 同一会话的 max seq 通过读取当前最大 seq +1 实现（会话内串行落库保证有序）。
func (d *DB) CreateMessage(m *Message) (int64, error) {
	table := msgTable(m.ConvID)
	// extra 为空字符串时写入 SQL NULL（字段为 JSON DEFAULT NULL），避免存成字符串 "null"；
	// 仅当有扩展 JSON（图片/文件等）时才写入实际内容。
	var extraArg interface{}
	if m.Extra == "" {
		extraArg = nil
	} else {
		extraArg = m.Extra
	}
	if m.Seq <= 0 {
		// 回退路径：单语句原子取号（INSERT ... SELECT MAX(seq)+1）。
		// InnoDB 下并发的 INSERT...SELECT 对扫描行加锁串行化，不会取到相同 seq，
		// 替代旧版 NextSeq=SELECT MAX 后另开语句 INSERT 的两步竞态（审计 P0）。
		_, err := d.Exec(`INSERT INTO `+table+` (id, conv_id, seq, sender_uid, type, content, extra, status)
			SELECT ?, ?, COALESCE(MAX(seq),0)+1, ?, ?, ?, ?, ? FROM `+table+` WHERE conv_id = ?`,
			m.ID, m.ConvID, m.SenderUID, m.Type, m.Content, extraArg, m.Status, m.ConvID)
		if err != nil {
			return 0, err
		}
		// 回读本条实际分配的 seq（MySQL 无 RETURNING）
		var seq int64
		if err := d.QueryRow(`SELECT seq FROM `+table+` WHERE id = ? AND conv_id = ?`, m.ID, m.ConvID).Scan(&seq); err != nil {
			return 0, err
		}
		m.Seq = seq
		return seq, nil
	}
	// Seq 已由调用方预分配（Redis 原子取号）：直接插入，冲突由上层重试
	_, err := d.Exec(`INSERT INTO `+table+` (id, conv_id, seq, sender_uid, type, content, extra, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.ConvID, m.Seq, m.SenderUID, m.Type, m.Content, extraArg, m.Status)
	return m.Seq, err
}

// NextSeq 计算会话下一序号 = 当前 max seq + 1。空会话返回 1。
func (d *DB) NextSeq(convID int64) (int64, error) {
	table := msgTable(convID)
	var maxSeq int64
	err := d.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM `+table+` WHERE conv_id = ?`, convID).Scan(&maxSeq)
	if err != nil {
		return 0, err
	}
	return maxSeq + 1, nil
}

// GetMessage 按消息 ID 查询（需先确定 conv_id 以便路由分表）。
func (d *DB) GetMessage(convID, msgID int64) (*Message, error) {
	table := msgTable(convID)
	row := d.QueryRow(`SELECT `+msgCols+` FROM `+table+` WHERE id = ? AND conv_id = ?`, msgID, convID)
	m, err := scanMsg(row)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// UpdateMessageStatus 更新消息状态（0 正常 / 1 已撤回）。
func (d *DB) UpdateMessageStatus(convID, msgID int64, status int8) error {
	table := msgTable(convID)
	_, err := d.Exec(`UPDATE `+table+` SET status = ? WHERE id = ? AND conv_id = ?`, status, msgID, convID)
	return err
}

// GetLastActiveMessage 查询某会话最后一条未撤回消息（status=0），用于撤回后回退会话最后消息。
// 无消息返回 nil,nil。
func (d *DB) GetLastActiveMessage(convID int64) (*Message, error) {
	table := msgTable(convID)
	row := d.QueryRow(`SELECT `+msgCols+` FROM `+table+` WHERE conv_id = ? AND status = 0 ORDER BY seq DESC LIMIT 1`, convID)
	m, err := scanMsg(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return m, nil
}

// ListMessages 拉取某会话历史：seq > afterSeq（增量）或倒序向前翻页。
// limit<=0 表示不限制。
func (d *DB) ListMessages(convID, afterSeq int64, limit int) ([]*Message, error) {
	table := msgTable(convID)
	query := `SELECT ` + msgCols + ` FROM ` + table + ` WHERE conv_id = ? AND seq > ? ORDER BY seq ASC`
	args := []interface{}{convID, afterSeq}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Message
	for rows.Next() {
		m, err := scanMsg(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

// ListMessagesBefore 向前翻页：拉取 seq < beforeSeq 的最近 limit 条消息，返回升序。
// beforeSeq<=0 时视为不限制上限（取整个会话最新 limit 条）。
func (d *DB) ListMessagesBefore(convID, beforeSeq int64, limit int) ([]*Message, error) {
	table := msgTable(convID)
	query := `SELECT ` + msgCols + ` FROM ` + table + ` WHERE conv_id = ?`
	args := []interface{}{convID}
	if beforeSeq > 0 {
		query += ` AND seq < ?`
		args = append(args, beforeSeq)
	}
	query += ` ORDER BY seq DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Message
	for rows.Next() {
		m, err := scanMsg(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 倒序查询结果翻转为升序，便于前端顺序追加
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list, nil
}

// MessageExists 检查消息是否已存在（msgId 幂等去重，这里用全局消息 ID 判断）。
func (d *DB) MessageExists(convID, msgID int64) (bool, error) {
	table := msgTable(convID)
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM `+table+` WHERE id = ? AND conv_id = ?`, msgID, convID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// SearchResult 搜索结果。
type SearchResult struct {
	Message *Message
	ConvID  int64
}

// SearchMessages 按关键字/类型在限定会话范围内跨分表搜索。
// 审计 P1：convIDs 按分表键路由分桶，每张分表只查归属自己的 conv_id（不再全量分表扫描）；
// convIDs 为空直接返回（DAO 层兜底，防全表扫/越权）；各表取足 limit 后归并，按 created_at 全局倒序。
func (d *DB) SearchMessages(keyword string, msgType int8, convIDs []int64, limit int) ([]*SearchResult, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if len(convIDs) == 0 {
		return nil, nil
	}
	// 按分表键分桶：同一分表的 conv_id 合并成一条 IN 查询
	buckets := make(map[int][]int64)
	for _, id := range convIDs {
		shard := Shard(id, ShardCount)
		buckets[shard] = append(buckets[shard], id)
	}
	like := "%" + EscapeLike(keyword) + "%" // 审计 L1：转义通配符，防构造全表慢查
	var results []*SearchResult
	for shard, ids := range buckets {
		table := TableName("messages", shard)
		query := "SELECT " + msgCols + " FROM `" + table + "` WHERE content LIKE ?"
		args := []interface{}{like}
		if msgType > 0 {
			query += " AND type = ?"
			args = append(args, msgType)
		}
		query += " AND conv_id IN ("
		for j, id := range ids {
			if j > 0 {
				query += ","
			}
			query += "?"
			args = append(args, id)
		}
		query += ") ORDER BY created_at DESC LIMIT ?"
		args = append(args, limit)
		rows, err := d.Query(query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			m, err := scanMsg(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			results = append(results, &SearchResult{Message: m, ConvID: m.ConvID})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	// 跨分表归并：按发送时间全局倒序（分表间无全局序）后截断 limit
	sort.Slice(results, func(i, j int) bool {
		return results[i].Message.CreatedAt.After(results[j].Message.CreatedAt)
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func scanMsg(row interface{ Scan(...interface{}) error }) (*Message, error) {
	var m Message
	var extra interface{}
	err := row.Scan(&m.ID, &m.ConvID, &m.Seq, &m.SenderUID, &m.Type, &m.Content, &extra, &m.Status, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	// 数据库 extra 列是字节数组，需还原为原始字符串（不能用 fmt.Sprint，那会把 []byte 转成 "[123 34 ...]"）
	switch v := extra.(type) {
	case []byte:
		m.Extra = string(v)
	case string:
		m.Extra = v
	default:
		if extra != nil {
			m.Extra = fmt.Sprint(v)
		}
	}
	return &m, nil
}
