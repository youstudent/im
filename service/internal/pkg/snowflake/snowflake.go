// Package snowflake 实现雪花 ID 生成器，用于全局唯一消息/会话等内部主键。
// 位分配：1 bit 符号位 | 41 bit 毫秒时间戳 | 10 bit 机器节点 | 12 bit 序列号
package snowflake

import (
	"sync"
	"time"
)

const (
	epoch        int64 = 1704067200000 // 2024-01-01 00:00:00 UTC
	nodeBits     uint  = 10
	sequenceBits uint  = 12
	sequenceMask int64 = -1 ^ (-1 << sequenceBits)

	nodeMax int64 = -1 ^ (-1 << nodeBits)

	nodeShift   uint = sequenceBits
	timeShift   uint = sequenceBits + nodeBits
)

// Snowflake 线程安全的雪花 ID 生成器。
type Snowflake struct {
	mu       sync.Mutex
	nodeID   int64
	lastTime int64
	sequence int64
}

// New 创建生成器，nodeID 取值 0 ~ 1023。
func New(nodeID int64) (*Snowflake, error) {
	if nodeID < 0 || nodeID > nodeMax {
		return nil, &ErrNodeID{}
	}
	return &Snowflake{nodeID: nodeID, lastTime: -1}, nil
}

// ErrNodeID 节点 ID 越界错误。
type ErrNodeID struct{}

func (e *ErrNodeID) Error() string { return "snowflake: node id out of range" }

// NextID 生成下一个雪花 ID。
func (s *Snowflake) NextID() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli() - epoch
	if now < s.lastTime {
		now = s.lastTime // 时钟回拨时退化为单调递增（保证不重复）
	}
	if now == s.lastTime {
		s.sequence = (s.sequence + 1) & sequenceMask
		if s.sequence == 0 {
			// 当前毫秒序列号用尽，等待到下一毫秒
			for now <= s.lastTime {
				now = time.Now().UnixMilli() - epoch
			}
		}
	} else {
		s.sequence = 0
	}
	s.lastTime = now

	return (now << timeShift) | (s.nodeID << nodeShift) | s.sequence
}

// NodeID 返回当前节点 ID。
func (s *Snowflake) NodeID() int64 { return s.nodeID }
