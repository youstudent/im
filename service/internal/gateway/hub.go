// hub.go：网关连接注册中心 + Redis 在线状态 + 跨节点消息路由。
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"im/service/internal/pkg/log"
)

// Conn 代表一个已鉴权的长连接。
type Conn interface {
	UID() int64
	Send(msgType int, data []byte) error
	Close() error
}

// Hub 管理所有在线连接，并在 Redis 记录 uid→节点 映射、发布跨节点消息。
type Hub struct {
	mu   sync.RWMutex
	conn map[int64]map[Conn]struct{} // uid → 多连接（多端）

	rdb   *redis.Client
	node  string // 本节点标识（用于跨节点路由定位）
	route chan *RouteMsg // 本节点收到的跨节点投递消息
}

// RouteMsg 跨节点路由消息。
type RouteMsg struct {
	TargetUID int64
	Frame     *Frame
}

// NewHub 创建注册中心，并订阅本节点频道接收跨节点消息。
func NewHub(rdb *redis.Client, node string) *Hub {
	h := &Hub{
		conn:  make(map[int64]map[Conn]struct{}),
		rdb:   rdb,
		node:  node,
		route: make(chan *RouteMsg, 256),
	}
	go h.subscribe()
	go h.dispatch()
	return h
}

// Add 注册连接并写 Redis 在线状态。
func (h *Hub) Add(c Conn) {
	h.mu.Lock()
	if h.conn[c.UID()] == nil {
		h.conn[c.UID()] = make(map[Conn]struct{})
	}
	h.conn[c.UID()][c] = struct{}{}
	h.mu.Unlock()

	// 记录 uid → 节点 映射，TTL 与在线心跳对齐
	_ = h.rdb.Set(context.Background(), presenceKey(c.UID()), h.node, 90*time.Second)
	log.L().Info("connection established", "uid", c.UID(), "node", h.node)
}

// Remove 注销连接，无连接时清除在线状态。
func (h *Hub) Remove(c Conn) {
	h.mu.Lock()
	if set := h.conn[c.UID()]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.conn, c.UID())
			_ = h.rdb.Del(context.Background(), presenceKey(c.UID()))
			log.L().Info("connection closed, offline", "uid", c.UID())
		}
	}
	h.mu.Unlock()
}

// IsOnline 查询某用户是否在线（presence 键存在即在线）。
func (h *Hub) IsOnline(uid int64) bool {
	n, err := h.rdb.Exists(context.Background(), presenceKey(uid)).Result()
	return err == nil && n > 0
}

// TouchPresence 续期在线状态键：心跳到达时调用，防止 TTL 到期后
// 连接仍存活却被误判离线（多节点路由错判、在线统计失真）。
func (h *Hub) TouchPresence(uid int64) {
	_ = h.rdb.Expire(context.Background(), presenceKey(uid), 90*time.Second)
}

// DisconnectAll 断开本节点全部连接（服务优雅退出时调用），
// 各连接 readLoop 退出后触发 Remove 清理在线状态。
func (h *Hub) DisconnectAll() {
	h.mu.RLock()
	conns := make([]Conn, 0)
	for _, set := range h.conn {
		for c := range set {
			conns = append(conns, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range conns {
		_ = c.Close()
	}
	if len(conns) > 0 {
		log.L().Info("disconnect all connections on shutdown", "count", len(conns))
	}
}

// Disconnect 强制断开指定用户的所有 WS 连接（用于退出登录等）。
// 关闭连接后，各连接的 readLoop 会退出并触发 Remove（清理在线状态、打印断开日志）。
func (h *Hub) Disconnect(uid int64) {
	h.mu.RLock()
	set := h.conn[uid]
	h.mu.RUnlock()
	for c := range set {
		_ = c.Close()
	}
	log.L().Info("force disconnect by logout", "uid", uid)
}

// Kick 强制下线指定用户：先推送 kick 帧（携带原因），再关闭其所有 WS 连接。
// 客户端收到 kick 帧后停止重连并回到登录页；关闭连接后各连接 readLoop 退出并触发 Remove。
// reason 为可选原因（如"账号已被禁用"），为空时客户端使用默认文案。
func (h *Hub) Kick(uid int64, reason string) {
	frame := &Frame{Ver: 1, Type: FrameKick, Seq: 0, Body: map[string]interface{}{"reason": reason}}
	h.deliverLocal(uid, frame)
	h.Disconnect(uid)
	log.L().Info("force disconnect by admin", "uid", uid, "reason", reason)
}

// Push 投递消息给目标 uid：本节点在线则直接发，否则走跨节点路由。
// 目标完全离线（本节点 + 跨节点均不可达）时，将消息存入其离线队列，重连后补发。
func (h *Hub) Push(targetUID int64, frame *Frame) {
	if h.deliverLocal(targetUID, frame) {
		return
	}
	// 跨节点：向目标节点频道发布
	node := h.rdb.Get(context.Background(), presenceKey(targetUID)).Val()
	if node == "" || node == h.node {
		// 目标离线：存入离线队列（重连后 flush）
		h.enqueueOffline(targetUID, frame)
		return
	}
	data, err := json.Marshal(&RouteMsg{TargetUID: targetUID, Frame: frame})
	if err != nil {
		return
	}
	_ = h.rdb.Publish(context.Background(), nodeChannel(node), data)
}

// enqueueOffline 将消息帧存入目标用户的离线队列（Redis list），按帧类型分级（P2）：
//   - 内容帧（msg.push 等）走常规队列 offline:%d（上限 offlineQueueMax）；
//   - 控制帧（ack/read.sync）走高优先级队列 offline:hi:%d（上限 offlineHiQueueMax），
//     避免 ack/已读回执与消息帧挤占同一队列容量，重连后优先补发送达/已读状态。
//
// 边界保护（审计 P0）：队列限长（保留最新 N 条，最旧丢弃，
// 客户端重连后还能通过增量拉取 after_seq 兜底补齐）+ 7 天 TTL 防无限膨胀。
const offlineQueueMax = 1000
const offlineHiQueueMax = 200

// offlineQueueKey 按帧类型返回离线队列 key 与长度上限。
func offlineQueueKey(uid int64, typ FrameType) (string, int64) {
	if typ == FrameAck || typ == FrameReadSync {
		return fmt.Sprintf("offline:hi:%d", uid), offlineHiQueueMax
	}
	return fmt.Sprintf("offline:%d", uid), offlineQueueMax
}

func (h *Hub) enqueueOffline(uid int64, frame *Frame) {
	data, err := json.Marshal(frame)
	if err != nil {
		return
	}
	ctx := context.Background()
	key, max := offlineQueueKey(uid, frame.Type)
	_ = h.rdb.LPush(ctx, key, data)
	_ = h.rdb.LTrim(ctx, key, 0, max-1)
	_ = h.rdb.Expire(ctx, key, 7*24*time.Hour)
}

// FlushOffline 重连后补发离线消息：先高优先级队列（ack/已读控制帧），再消息队列；
// 先读后发、只删发送成功的部分（审计 P0）。
// 旧实现先 RPop 再 Send，发送失败即永久丢失；现在发送失败时消息留在队列，下次重连继续。
func (h *Hub) FlushOffline(uid int64, conn Conn) int {
	count := h.flushKey(fmt.Sprintf("offline:hi:%d", uid), conn)
	count += h.flushKey(fmt.Sprintf("offline:%d", uid), conn)
	return count
}

// flushKey 冲刷单个离线队列：LRANGE 取最旧一批逐条发送，成功多少出队多少；
// 发送失败时已成功部分出队，其余留在队列等下次。
func (h *Hub) flushKey(key string, conn Conn) int {
	ctx := context.Background()
	var count int
	for {
		// LPush 头插，尾部为最旧：LRANGE -200..-1 取最旧的一批（旧→新）
		vals, err := h.rdb.LRange(ctx, key, -200, -1).Result()
		if err != nil || len(vals) == 0 {
			break
		}
		sent := 0
		for _, v := range vals {
			if conn == nil {
				break
			}
			if err := conn.Send(1, []byte(v)); err != nil {
				break // 发送失败：已成功部分出队，其余留在队列等下次
			}
			sent++
		}
		if sent == 0 {
			break
		}
		// 移除已发送的尾部 sent 条（保留头部新消息；sent 等于全长时清空）
		_ = h.rdb.LTrim(ctx, key, 0, int64(-sent-1))
		count += sent
		if sent < len(vals) {
			break // 发送被中断或队列已空
		}
	}
	return count
}

// deliverLocal 本节点范围内投递（目标所有连接）。
func (h *Hub) deliverLocal(targetUID int64, frame *Frame) bool {
	h.mu.RLock()
	set := h.conn[targetUID]
	h.mu.RUnlock()
	if len(set) == 0 {
		return false
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return true
	}
	for c := range set {
		if err := c.Send(1, data); err != nil {
			_ = c.Close()
		}
	}
	return true
}

// PushLive 在线实时投递：本节点直发 / 跨节点路由；目标离线返回 false（不入离线队列）。
// 用于时效性强的控制信令（如通话信令）：过期的呼叫帧重连后补发已无意义，反而骚扰用户。
func (h *Hub) PushLive(targetUID int64, frame *Frame) bool {
	if h.deliverLocal(targetUID, frame) {
		return true
	}
	node := h.rdb.Get(context.Background(), presenceKey(targetUID)).Val()
	if node == "" || node == h.node {
		return false
	}
	data, err := json.Marshal(&RouteMsg{TargetUID: targetUID, Frame: frame})
	if err != nil {
		return false
	}
	_ = h.rdb.Publish(context.Background(), nodeChannel(node), data)
	return true
}

// isBusy 查询用户是否通话中（忙线键存在即忙，由 call.go 维护）。
func (h *Hub) isBusy(uid int64) bool {
	n, err := h.rdb.Exists(context.Background(), callBusyKey(uid)).Result()
	return err == nil && n > 0
}

// Broadcast 向多个目标广播（群聊/好友在线状态）。
func (h *Hub) Broadcast(uids []int64, frame *Frame) {
	for _, uid := range uids {
		h.Push(uid, frame)
	}
}

// PublishLocal 把本节点消息发布到其他节点的频道（供其它节点投递给它们本地的连接）。
// 本实现单机模式直接投递；多实例时由 Push 处理跨节点。
func (h *Hub) PublishLocal(uid int64, frame *Frame) {
	h.deliverLocal(uid, frame)
}

// subscribe 订阅本节点频道，接收其它节点投递过来的消息。
func (h *Hub) subscribe() {
	ctx := context.Background()
	sub := h.rdb.Subscribe(ctx, nodeChannel(h.node))
	ch := sub.Channel()
	for msg := range ch {
		var rm RouteMsg
		if err := json.Unmarshal([]byte(msg.Payload), &rm); err != nil {
			continue
		}
		select {
		case h.route <- &rm:
		default: // 队列满丢弃，避免阻塞
		}
	}
}

// dispatch 处理跨节点路由消息并投递给本节点连接。
func (h *Hub) dispatch() {
	for rm := range h.route {
		if rm.Frame != nil {
			h.deliverLocal(rm.TargetUID, rm.Frame)
		}
	}
}

func presenceKey(uid int64) string { return "presence:" + itoa(uid) }
func nodeChannel(node string) string { return "im:route:" + node }

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
