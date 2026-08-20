// Package gateway 是 WebSocket 长连接网关（阶段三完整实现）。
// 本阶段仅定义协议常量与消息帧结构，供后续实现复用。
package gateway

// FrameType 长连接消息帧 type 枚举。
type FrameType string

const (
	FrameAuth      FrameType = "auth"       // C→S 建连鉴权
	FrameHeartbeat FrameType = "heartbeat"  // C↔S 心跳
	FrameMsg       FrameType = "msg"        // C→S 发送消息
	FrameMsgPush   FrameType = "msg.push"   // S→C 推送消息
	FrameAck       FrameType = "ack"        // C→S 送达回执
	FrameRead      FrameType = "read"       // C→S 已读回执
	FrameReadSync  FrameType = "read.sync"  // S→C 已读广播
	FrameTyping    FrameType = "typing"     // C↔S 正在输入
	FramePresence  FrameType = "presence"   // S→C 在线状态变更
	FrameNotify    FrameType = "notify"     // S→C 通知
	FrameKick      FrameType = "kick"       // S→C 强制下线（账号被禁用/管理员踢出），客户端收到后停止重连并回登录页
	FrameCall      FrameType = "call"       // C→S 语音通话信令（纯转发，不落库、不进离线队列）
	FrameCallPush  FrameType = "call.push"  // S→C 语音通话信令转发
	FrameReaction  FrameType = "reaction"   // S→C 表情回应变更（S6）：{conv_id, msg_id, uid, emoji, add}
)

// Frame 统一长连接消息帧（JSON）。
type Frame struct {
	Ver  int         `json:"ver"`
	Type FrameType   `json:"type"`
	Seq  int64       `json:"seq,omitempty"`
	Body interface{} `json:"body,omitempty"`
}

// NewFrame 构造一个帧。
func NewFrame(typ FrameType, seq int64, body interface{}) *Frame {
	return &Frame{Ver: 1, Type: typ, Seq: seq, Body: body}
}
