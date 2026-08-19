// call.go：语音通话信令转发（纯中继：不落库、不进离线队列）。
// 信令帧统一结构 { call_id, action, from, to, payload }，action 取值：
//   - invite/answer/ice/reject/busy/cancel/hangup：透传给 to；
//   - busy.set/busy.clear：仅服务端维护忙线状态，不转发。
//
// invite 特殊处理：对端忙线回 busy、对端离线回 offline（均不入离线队列）；
// 其余信令对端离线时静默丢弃（通话已无从建立）。
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// callSignal 通话信令帧统一结构。
type callSignal struct {
	CallID  string          `json:"call_id"`
	Action  string          `json:"action"`
	From    int64           `json:"from"`
	To      int64           `json:"to"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// 仅服务端处理、不转发的忙线维护动作。
const (
	callBusySet   = "busy.set"
	callBusyClear = "busy.clear"
)

const (
	// callBusyTTL 忙线状态兜底过期（客户端崩溃未清键时最多占用 1 小时）
	callBusyTTL = time.Hour
	// callIDMaxLen call_id 长度上限，防超大帧
	callIDMaxLen = 64
	// callPayloadMaxLen SDP/ICE 载荷上限（SDP 通常 1~3KB，留足余量）
	callPayloadMaxLen = 32 * 1024
)

// callBusyKey 忙线状态键（存在即通话中）。
func callBusyKey(uid int64) string { return fmt.Sprintf("im:call:busy:%d", uid) }

// handleCall 处理通话信令帧。
func (cl *client) handleCall(f *Frame) {
	var sig callSignal
	if err := json.Unmarshal(rawBody(f), &sig); err != nil {
		return
	}
	// 强制覆写发送方为已鉴权 uid：防止伪造 from 骚扰任意用户
	sig.From = cl.uid
	// 忙线维护动作：仅服务端处理，不转发
	switch sig.Action {
	case callBusySet:
		_ = cl.srv.hub.rdb.Set(context.Background(), callBusyKey(cl.uid), sig.CallID, callBusyTTL)
		return
	case callBusyClear:
		_ = cl.srv.hub.rdb.Del(context.Background(), callBusyKey(cl.uid))
		return
	}
	if sig.To <= 0 || sig.To == cl.uid || sig.CallID == "" || len(sig.CallID) > callIDMaxLen {
		return
	}
	if len(sig.Payload) > callPayloadMaxLen {
		return
	}
	// 好友校验：防止对任意用户发起呼叫骚扰（未注入校验函数时放行）
	if cl.srv.friendCheck != nil && !cl.srv.friendCheck(cl.uid, sig.To) {
		return
	}
	frame := NewFrame(FrameCallPush, 0, sig)
	if sig.Action != "invite" {
		// answer/ice/reject/cancel/hangup 等：对端离线直接丢弃
		cl.srv.hub.PushLive(sig.To, frame)
		return
	}
	// invite：对端忙线 → 回 busy；对端离线 → 回 offline（两者仅回复发起方）
	if cl.srv.hub.isBusy(sig.To) {
		cl.Send(1, mustJSON(callReply(&sig, "busy")))
		return
	}
	if !cl.srv.hub.PushLive(sig.To, frame) {
		cl.Send(1, mustJSON(callReply(&sig, "offline")))
	}
}

// callReply 构造服务端代答信令（busy/offline）：From 填被叫方，便于发起方 UI 归因。
func callReply(sig *callSignal, action string) *Frame {
	return NewFrame(FrameCallPush, 0, callSignal{
		CallID: sig.CallID,
		Action: action,
		From:   sig.To,
		To:     sig.From,
	})
}
