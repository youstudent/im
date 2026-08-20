// adapter.go：把 message 服务适配为网关的 MessageHandler。
package gateway

import (
	"encoding/json"

	"im/service/internal/message"
)

// GetGroupMembers 获取群成员 uid 列表（由上层注入 social 服务能力）。
type GetGroupMembers func(gUID int64) ([]int64, error)

// Adapter 桥接网关与消息服务。
type Adapter struct {
	svc   *message.Service
	member GetGroupMembers
}

// NewAdapter 创建适配器。member 用于群聊多路分发，可为 nil（则群聊仅回显发送方）。
func NewAdapter(svc *message.Service, member GetGroupMembers) *Adapter {
	return &Adapter{svc: svc, member: member}
}

// SetGroupMembers 注入群成员查询（在 social 服务就绪后调用）。
func (a *Adapter) SetGroupMembers(fn GetGroupMembers) {
	a.member = fn
}

// HandleMsg 处理发送消息帧。
// 返回 (pushFrame, echoFrame, recipients, err)：
//   - pushFrame：需要推送给接收方的帧；重复消息（幂等重发）为 nil，避免接收方重复收到。
//   - echoFrame：回显给发送方自身的帧（用于替换乐观渲染/确认）。
//   - recipients：接收方 uid 列表。
func (a *Adapter) HandleMsg(uid int64, body json.RawMessage) (*Frame, *Frame, []int64, error) {
	var req message.SendReq
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, nil, nil, err
	}
	dto, isNew, err := a.svc.Send(uid, &req)
	if err != nil {
		return nil, nil, nil, err
	}
	// 接收方：单聊对端 target；群聊为群成员列表
	var recipients []int64
	if req.ConvType == 2 && a.member != nil {
		recipients, _ = a.member(req.TargetID)
	} else {
		recipients = []int64{req.TargetID}
	}
	// 幂等重发（同一 msgId 已存在）：不向接收方推送、也不回显给发送方（发送方首次回显时已替换乐观消息），
	// 完全静默，避免两端重复显示。
	if !isNew {
		return nil, nil, recipients, nil
	}
	echoFrame := NewFrame(FrameMsgPush, 0, dto)
	return echoFrame, echoFrame, recipients, nil
}

// HandleRead 处理已读回执：记录已读 seq，并返回需要广播给消息发送方的 read.sync 帧与对端 uid 列表。
// 仅单聊广播给对端；群聊返回空列表（不实现群聊已读）。
func (a *Adapter) HandleRead(uid int64, body json.RawMessage) (*Frame, []int64, error) {
	var req struct {
		ConvID int64 `json:"conv_id,string"`
		Seq    int64 `json:"seq"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, nil, err
	}
	if err := a.svc.MarkRead(uid, req.ConvID, req.Seq); err != nil {
		return nil, nil, err
	}
	// 对端 uid（消息发送方），仅单聊
	var toUIDs []int64
	if peer := a.svc.GetConversationPeer(req.ConvID, uid); peer > 0 {
		toUIDs = []int64{peer}
	}
	return NewFrame(FrameReadSync, 0, req), toUIDs, nil
}

// HandleAck 送达回执：接收方确认收到消息后，把 ack 转发给消息发送方。
// 返回需要转发给发送方的 ack 帧 + 发送方 uid 列表。
func (a *Adapter) HandleAck(uid int64, body json.RawMessage) (*Frame, []int64, error) {
	var req struct {
		ConvID int64 `json:"conv_id"`
		MsgID  int64 `json:"msg_id"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.MsgID == 0 {
		return nil, nil, nil
	}
	// 查消息发送方，把 ack 转发给它
	msg, err := a.svc.GetMessage(req.ConvID, req.MsgID)
	if err != nil || msg == nil {
		return nil, nil, nil
	}
	ackFrame := NewFrame(FrameAck, 0, map[string]interface{}{
		"conv_id": req.ConvID,
		"msg_id":  req.MsgID,
		"sender":  uid, // ack 来自接收方
	})
	return ackFrame, []int64{msg.SenderUID}, nil
}

// HandleTyping 正在输入（S7）：解析 conv_id，仅单聊返回对端 uid（群聊不广播输入状态）。
func (a *Adapter) HandleTyping(uid int64, body json.RawMessage) ([]int64, error) {
	var req struct {
		ConvID int64 `json:"conv_id,string"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.ConvID <= 0 {
		return nil, nil
	}
	if peer := a.svc.GetConversationPeer(req.ConvID, uid); peer > 0 {
		return []int64{peer}, nil
	}
	return nil, nil // 群聊或未知会话：不转发
}
