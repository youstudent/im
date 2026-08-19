package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// ===================== 通话信令转发 =====================

// callFrameOf 构造 call 帧（模拟客户端发送）。
func callFrameOf(body map[string]interface{}) *Frame {
	return NewFrame(FrameCall, 1, body)
}

// parseCallSignal 把 call.push 帧 body 解析为信令结构。
func parseCallSignal(t *testing.T, f *Frame) callSignal {
	t.Helper()
	if f.Type != FrameCallPush {
		t.Fatalf("期望 call.push 帧，实际 %s", f.Type)
	}
	b, _ := json.Marshal(f.Body)
	var sig callSignal
	if err := json.Unmarshal(b, &sig); err != nil {
		t.Fatalf("解析信令失败: %v", err)
	}
	return sig
}

// TestCallInviteForward 验证 invite 转发：对端在线时收到 call.push，
// 且 from 被强制覆写为已鉴权 uid（防伪造）。
func TestCallInviteForward(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, jm := testServer(newMockHandler())
	r := gin.New()
	r.GET("/ws", s.HandleWS)
	srv := httptest.NewServer(r)
	defer srv.Close()

	url := "ws" + srv.URL[4:] + "/ws"
	wsA := dialAuth(t, url, jm, 100)
	defer wsA.Close()
	wsB := dialAuth(t, url, jm, 200)
	defer wsB.Close()

	// A 伪造 from=999 发起呼叫：服务端必须覆写为真实 uid 100
	invite := callFrameOf(map[string]interface{}{
		"call_id": "c-1", "action": "invite", "from": 999, "to": 200,
		"payload": map[string]interface{}{"sdp": "offer-sdp"},
	})
	if err := sendJSON(wsA, invite); err != nil {
		t.Fatalf("send invite failed: %v", err)
	}

	f, err := readFrame(wsB, 3*time.Second)
	if err != nil {
		t.Fatalf("B 未收到 invite: %v", err)
	}
	sig := parseCallSignal(t, f)
	if sig.Action != "invite" || sig.CallID != "c-1" {
		t.Fatalf("信令内容不符: %+v", sig)
	}
	if sig.From != 100 {
		t.Fatalf("from 应被覆写为 100，实际 %d", sig.From)
	}
}

// TestCallInviteOfflineReply 验证对端离线时 invite 不转发，发起方收到 offline 代答。
func TestCallInviteOfflineReply(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, jm := testServer(newMockHandler())
	r := gin.New()
	r.GET("/ws", s.HandleWS)
	srv := httptest.NewServer(r)
	defer srv.Close()

	url := "ws" + srv.URL[4:] + "/ws"
	wsA := dialAuth(t, url, jm, 100)
	defer wsA.Close()
	// 目标 300 未连接：离线

	invite := callFrameOf(map[string]interface{}{
		"call_id": "c-2", "action": "invite", "to": 300,
	})
	if err := sendJSON(wsA, invite); err != nil {
		t.Fatalf("send invite failed: %v", err)
	}

	f, err := readFrame(wsA, 3*time.Second)
	if err != nil {
		t.Fatalf("A 未收到 offline 代答: %v", err)
	}
	sig := parseCallSignal(t, f)
	if sig.Action != "offline" || sig.CallID != "c-2" {
		t.Fatalf("期望 offline 代答，实际 %+v", sig)
	}
	if sig.From != 300 || sig.To != 100 {
		t.Fatalf("offline 代答 from/to 错误: %+v", sig)
	}
}

// TestCallSignalRelay 验证非 invite 信令（answer/ice/hangup）透传且覆写 from。
func TestCallSignalRelay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, jm := testServer(newMockHandler())
	r := gin.New()
	r.GET("/ws", s.HandleWS)
	srv := httptest.NewServer(r)
	defer srv.Close()

	url := "ws" + srv.URL[4:] + "/ws"
	wsA := dialAuth(t, url, jm, 100)
	defer wsA.Close()
	wsB := dialAuth(t, url, jm, 200)
	defer wsB.Close()

	answer := callFrameOf(map[string]interface{}{
		"call_id": "c-3", "action": "answer", "to": 200,
		"payload": map[string]interface{}{"sdp": "answer-sdp"},
	})
	if err := sendJSON(wsA, answer); err != nil {
		t.Fatalf("send answer failed: %v", err)
	}
	f, err := readFrame(wsB, 3*time.Second)
	if err != nil {
		t.Fatalf("B 未收到 answer: %v", err)
	}
	sig := parseCallSignal(t, f)
	if sig.Action != "answer" || sig.From != 100 || sig.To != 200 {
		t.Fatalf("answer 转发内容不符: %+v", sig)
	}
}

// TestCallFriendCheckBlocks 验证好友校验拦截：非好友信令静默丢弃（双方均收不到帧）。
func TestCallFriendCheckBlocks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, jm := testServer(newMockHandler())
	// 注入拒绝一切的好友校验
	s.SetFriendCheck(func(from, to int64) bool { return false })
	r := gin.New()
	r.GET("/ws", s.HandleWS)
	srv := httptest.NewServer(r)
	defer srv.Close()

	url := "ws" + srv.URL[4:] + "/ws"
	wsA := dialAuth(t, url, jm, 100)
	defer wsA.Close()
	wsB := dialAuth(t, url, jm, 200)
	defer wsB.Close()

	if err := sendJSON(wsA, callFrameOf(map[string]interface{}{
		"call_id": "c-4", "action": "invite", "to": 200,
	})); err != nil {
		t.Fatalf("send invite failed: %v", err)
	}

	// B 不应收到任何帧
	_ = wsB.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
	if _, _, err := wsB.ReadMessage(); err == nil {
		t.Fatal("非好友 invite 不应被转发")
	}
	// A 也不应收到代答（区别于 offline：校验失败是静默丢弃）
	_ = wsA.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
	if _, _, err := wsA.ReadMessage(); err == nil {
		t.Fatal("校验失败不应回复任何帧")
	}
}

// TestCallBusyActionsNotForwarded 验证 busy.set/busy.clear 仅服务端处理，不转发任何人。
func TestCallBusyActionsNotForwarded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, jm := testServer(newMockHandler())
	r := gin.New()
	r.GET("/ws", s.HandleWS)
	srv := httptest.NewServer(r)
	defer srv.Close()

	url := "ws" + srv.URL[4:] + "/ws"
	wsA := dialAuth(t, url, jm, 100)
	defer wsA.Close()
	wsB := dialAuth(t, url, jm, 200)
	defer wsB.Close()

	for _, action := range []string{"busy.set", "busy.clear"} {
		if err := sendJSON(wsA, callFrameOf(map[string]interface{}{
			"call_id": "c-5", "action": action, "to": 200,
		})); err != nil {
			t.Fatalf("send %s failed: %v", action, err)
		}
	}

	// B 与 A 都不应收到帧（测试环境无真实 Redis，忙线键写入失败被忽略即可）
	_ = wsB.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
	if _, _, err := wsB.ReadMessage(); err == nil {
		t.Fatal("busy 维护信令不应转发")
	}
	_ = wsA.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
	if _, _, err := wsA.ReadMessage(); err == nil {
		t.Fatal("busy 维护信令不应回帧")
	}
}

// TestCallInvalidFramesIgnored 验证非法信令（无 call_id / 呼叫自己 / 超长 call_id）被静默忽略。
func TestCallInvalidFramesIgnored(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, jm := testServer(newMockHandler())
	r := gin.New()
	r.GET("/ws", s.HandleWS)
	srv := httptest.NewServer(r)
	defer srv.Close()

	url := "ws" + srv.URL[4:] + "/ws"
	wsA := dialAuth(t, url, jm, 100)
	defer wsA.Close()
	wsB := dialAuth(t, url, jm, 200)
	defer wsB.Close()

	longID := make([]byte, callIDMaxLen+1)
	for i := range longID {
		longID[i] = 'x'
	}
	cases := []*Frame{
		callFrameOf(map[string]interface{}{"action": "invite", "to": 200}),                            // 无 call_id
		callFrameOf(map[string]interface{}{"call_id": "c", "action": "invite", "to": 100}),            // 呼叫自己
		callFrameOf(map[string]interface{}{"call_id": string(longID), "action": "invite", "to": 200}), // 超长 call_id
	}
	for _, f := range cases {
		if err := sendJSON(wsA, f); err != nil {
			t.Fatalf("send failed: %v", err)
		}
	}
	_ = wsB.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
	if _, _, err := wsB.ReadMessage(); err == nil {
		t.Fatal("非法信令不应被转发")
	}
}

// TestCallReply 单测代答帧构造：from/to 互换，action 覆写。
func TestCallReply(t *testing.T) {
	sig := &callSignal{CallID: "c-9", Action: "invite", From: 100, To: 200}
	f := callReply(sig, "busy")
	got := parseCallSignal(t, f)
	if got.Action != "busy" || got.From != 200 || got.To != 100 || got.CallID != "c-9" {
		t.Fatalf("代答帧内容不符: %+v", got)
	}
}
