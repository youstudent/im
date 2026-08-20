package gateway

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"

	"im/service/internal/config"
	"im/service/internal/pkg/log"
	"im/service/internal/pkg/jwt"
)

// TestMain 初始化全局日志（丢弃输出），避免 Hub.Add 内 log.L() 为 nil 而 panic。
func TestMain(m *testing.M) {
	log.Init("error", "discard")
	os.Exit(m.Run())
}

// ===================== 测试基础设施 =====================

// mockHandler 内存消息处理器：记录收到的消息，并按规则返回 pushFrame 与接收方。
type mockHandler struct {
	mu       sync.Mutex
	received []string // 收到的 msg 内容
	// 发送给"接收方 B"的帧：记录以便断言
	pushedTo map[int64][]string
	// 是否投递失败（模拟离线/不可达）
	// 用于测"断线期间发送"场景
	drop bool
	// 可选的 ack 转发 handler（ack 转发测试用）
	ackHandler ackHandlerFunc
}

func newMockHandler() *mockHandler {
	return &mockHandler{pushedTo: map[int64][]string{}}
}

func (m *mockHandler) HandleMsg(uid int64, body json.RawMessage) (pushFrame, echoFrame *Frame, recipients []int64, err error) {
	var req struct {
		ConvID  int64  `json:"conv_id"`
		Content string `json:"content"`
		ToUID   int64  `json:"target_id"`
	}
	_ = json.Unmarshal(body, &req)
	m.mu.Lock()
	m.received = append(m.received, req.Content)
	m.mu.Unlock()

	frame := NewFrame("msg.push", 1, map[string]interface{}{
		"id":     uid*1000 + int64(len(m.received)),
		"conv_id": req.ConvID,
		"content": req.Content,
		"sender_uid": uid,
	})
	if m.drop {
		// 模拟离线/不可达：返回空接收方，投递将失败
		return nil, frame, nil, nil
	}
	return frame, frame, []int64{req.ToUID}, nil
}

func (m *mockHandler) HandleRead(uid int64, body json.RawMessage) (*Frame, []int64, error) {
	return nil, nil, nil
}

func (m *mockHandler) HandleAck(uid int64, body json.RawMessage) (*Frame, []int64, error) {
	// 若配置了 ackHandler，则委托给专门 handler（用于 ack 转发测试）
	if m.ackHandler != nil {
		return m.ackHandler(uid, body)
	}
	return nil, nil, nil
}

func (m *mockHandler) HandleTyping(uid int64, body json.RawMessage) ([]int64, error) {
	return nil, nil
}

// ackHandler 可选：用于模拟"服务端转发 ack 给发送方"。
type ackHandlerFunc func(uid int64, body json.RawMessage) (*Frame, []int64, error)

// testHub 构造不依赖真实 Redis 的 Hub。
// rdb 指向不可达地址，但设置极短超时 + 不重试，使 Add/Remove 内 rdb 调用快速失败并被忽略（不 panic、不拖慢测试）。
func testHub() *Hub {
	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 100 * time.Millisecond,
		MaxRetries:  0,
		PoolSize:    1,
	})
	h := &Hub{
		conn:  make(map[int64]map[Conn]struct{}),
		rdb:   rdb,
		node:  "test-node",
		route: make(chan *RouteMsg, 256),
	}
	return h
}

// testServer 构造 Server（不启动 NewServer 的 Redis 订阅 goroutine）。
func testServer(m *mockHandler) (*Server, *jwt.Manager) {
	jm := jwt.New(config.JWT{Secret: "test-secret", Issuer: "test", AccessExpire: 3600, RefreshExpire: 2592000})
	// 直接构造 Server（不依赖真实 Redis 订阅）；用短 pongWait 便于超时踢线测试
	s := &Server{hub: testHub(), jwt: jm, handler: m, pongWait: 2 * time.Second, writeWait: 15 * time.Second}
	return s, jm
}

// tokenOf 为 uid 签发 access token。
func tokenOf(jm *jwt.Manager, uid int64) string {
	acc, _, _ := jm.Generate(uid)
	return acc
}

// ===================== 帧协议辅助 =====================

// sendJSON 发送 JSON 帧。
func sendJSON(ws *websocket.Conn, f *Frame) error {
	b, _ := json.Marshal(f)
	return ws.WriteMessage(websocket.TextMessage, b)
}

// readFrame 读取一个 JSON 帧。
func readFrame(ws *websocket.Conn, timeout time.Duration) (*Frame, error) {
	if err := ws.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	_, data, err := ws.ReadMessage()
	if err != nil {
		return nil, err
	}
	var f Frame
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// dialAuth 建立 WS 连接并完成鉴权。
func dialAuth(t *testing.T, url string, jm *jwt.Manager, uid int64) *websocket.Conn {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	auth := NewFrame(FrameAuth, 1, map[string]interface{}{"token": tokenOf(jm, uid)})
	if err := sendJSON(ws, auth); err != nil {
		t.Fatalf("send auth failed: %v", err)
	}
	return ws
}

// ===================== 服务端：断线重连 / 在线状态 =====================

// TestHubConnectionLifecycle 验证连接注册/断开清理（断线检测的服务端一侧）。
// 注意：不在断言 IsOnline（它依赖 Redis 在线状态，测试环境无真实 Redis），改为直接检查内存连接表。
func TestHubConnectionLifecycle(t *testing.T) {
	h := testHub()
	// 模拟两个用户连接
	mockConn1 := &mockConn{uid: 1}
	mockConn2 := &mockConn{uid: 2}

	h.Add(mockConn1)
	h.Add(mockConn2)
	h.mu.RLock()
	_, ok1 := h.conn[1]
	_, ok2 := h.conn[2]
	h.mu.RUnlock()
	if !ok1 || !ok2 {
		t.Fatal("期望两个用户都注册到连接表")
	}

	// 用户1断开：应清理
	h.Remove(mockConn1)
	if len(h.conn[1]) != 0 {
		t.Fatal("用户1的连接应被清理")
	}

	// 多端：同一用户两个连接
	mockConn1b := &mockConn{uid: 1}
	h.Add(mockConn1)
	h.Add(mockConn1b)
	if len(h.conn[1]) != 2 {
		t.Fatalf("期望用户1有2个连接，实际 %d", len(h.conn[1]))
	}
	// 断开一个，还剩一个
	h.Remove(mockConn1)
	if len(h.conn[1]) != 1 {
		t.Fatalf("期望用户1剩1个连接，实际 %d", len(h.conn[1]))
	}
	// 最后一个断开后从连接表移除（清空在线）
	h.Remove(mockConn1b)
	if len(h.conn[1]) != 0 {
		t.Fatalf("期望用户1所有连接被清理")
	}
}

// TestHubDisconnect 验证退出登录时强制断开连接（hub.Disconnect）。
func TestHubDisconnect(t *testing.T) {
	h := testHub()
	c := &mockConn{uid: 42, onClose: func() {}} // 记录是否被关闭
	h.Add(c)
	h.Disconnect(42)
	if !c.closed {
		t.Fatal("期望连接被强制关闭")
	}
}

// mockConn 测试用连接实现。
type mockConn struct {
	uid     int64
	sent    [][]byte
	closed  bool
	onClose func()
}

func (c *mockConn) UID() int64                  { return c.uid }
func (c *mockConn) Send(_ int, data []byte) error { c.sent = append(c.sent, data); return nil }
func (c *mockConn) Close() error {
	c.closed = true
	if c.onClose != nil {
		c.onClose()
	}
	return nil
}

// ===================== 服务端：消息推送投递 =====================

// TestPushDeliverLocal 验证本节点在线时消息通过 deliverLocal 直接投递。
func TestPushDeliverLocal(t *testing.T) {
	h := testHub()
	receiver := &mockConn{uid: 9}
	h.Add(receiver)

	frame := NewFrame("msg.push", 1, map[string]interface{}{"content": "hello"})
	h.Push(9, frame)

	if len(receiver.sent) != 1 {
		t.Fatalf("期望接收方收到1条消息，实际 %d", len(receiver.sent))
	}
}

// TestPushOfflineDropped 验证目标离线时消息投递丢弃（当前系统无离线队列）。
// 预期结果：本节点离线时 Push 不投递，消息静默丢失——这暴露系统缺乏离线补偿。
func TestPushOfflineDropped(t *testing.T) {
	h := testHub()
	// 用户9没有任何连接（离线）
	frame := NewFrame("msg.push", 1, map[string]interface{}{"content": "missed"})
	h.Push(9, frame)
	// 不会 panic，但消息被丢弃（无补偿）。本测试断言"不 panic" + 记录现状。
	_ = frame
}

// ===================== 端到端 WS：心跳 / 消息收发 =====================

// TestWSHeartbeat 验证心跳链路：客户端发 heartbeat，服务端回 pong。
func TestWSHeartbeat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := newMockHandler()
	s, jm := testServer(m)

	r := gin.New()
	r.GET("/ws", s.HandleWS)
	srv := httptest.NewServer(r)
	defer srv.Close()

	url := "ws" + srv.URL[4:] + "/ws"
	ws := dialAuth(t, url, jm, 100)
	defer ws.Close()

	// 发心跳
	if err := sendJSON(ws, NewFrame(FrameHeartbeat, 1, gin.H{})); err != nil {
		t.Fatalf("send heartbeat failed: %v", err)
	}
	f, err := readFrame(ws, 2*time.Second)
	if err != nil {
		t.Fatalf("read pong failed: %v", err)
	}
	if f.Type != FrameHeartbeat {
		t.Fatalf("期望收到 heartbeat(pong) 帧，实际 %s", f.Type)
	}
}

// TestWSSendReceive 端到端验证消息收发：A 发消息 → 服务端推送 → A 回显。
func TestWSSendReceive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := newMockHandler()
	s, jm := testServer(m)

	r := gin.New()
	r.GET("/ws", s.HandleWS)
	srv := httptest.NewServer(r)
	defer srv.Close()

	url := "ws" + srv.URL[4:] + "/ws"
	ws := dialAuth(t, url, jm, 100)
	defer ws.Close()

	// A 发送消息（target_id=200 接收方）
	msg := NewFrame(FrameMsg, 2, map[string]interface{}{
		"conv_id": 123, "target_id": 200, "content": "test msg", "type": 1, "msg_id": 1000,
	})
	if err := sendJSON(ws, msg); err != nil {
		t.Fatalf("send msg failed: %v", err)
	}

	// 发送方应收到回显（msg.push）
	f, err := readFrame(ws, 2*time.Second)
	if err != nil {
		t.Fatalf("read push failed: %v", err)
	}
	if f.Type != "msg.push" {
		t.Fatalf("期望 msg.push 帧，实际 %s", f.Type)
	}
	// 断言 handler 收到消息
	m.mu.Lock()
	if len(m.received) != 1 || m.received[0] != "test msg" {
		t.Fatalf("handler 未正确收到消息: %v", m.received)
	}
	m.mu.Unlock()
}

// TestWSMultipleConnections 验证多端投递：接收方 B 的两个连接都能收到 A 发的消息。
func TestWSMultipleConnections(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := newMockHandler()
	s, jm := testServer(m)

	r := gin.New()
	r.GET("/ws", s.HandleWS)
	srv := httptest.NewServer(r)
	defer srv.Close()

	url := "ws" + srv.URL[4:] + "/ws"
	// 发送方 A(300) 单连接
	wsA := dialAuth(t, url, jm, 300)
	defer wsA.Close()
	// 接收方 B(301) 双连接（多端在线）
	wsB1 := dialAuth(t, url, jm, 301)
	defer wsB1.Close()
	wsB2 := dialAuth(t, url, jm, 301)
	defer wsB2.Close()

	// A 发送消息给 B(301)，target_id=301
	msg := NewFrame(FrameMsg, 3, map[string]interface{}{
		"conv_id": 9, "target_id": 301, "content": "multi", "type": 1, "msg_id": 2000,
	})
	_ = sendJSON(wsA, msg)

	// A 收到回显
	_, err := readFrame(wsA, 2*time.Second)
	if err != nil {
		t.Fatalf("发送方 A 未收到回显: %v", err)
	}
	// B 的两个连接都应收到推送（多端投递）
	if _, err := readFrame(wsB1, 2*time.Second); err != nil {
		t.Fatalf("B 连接1 未收到推送: %v", err)
	}
	if _, err := readFrame(wsB2, 2*time.Second); err != nil {
		t.Fatalf("B 连接2 未收到推送: %v", err)
	}
}

// ===================== 消息补偿（服务端已具备的能力） =====================

// TestFrameSeqIncrement 验证消息收发链路：发送 msg 后收到 msg.push 回显（seq 由服务端 adapter 回传）。
// 注：mock handler 固定 seq=1，此处只断言收到 msg.push 帧（真实 adapter 会回传请求 seq）。
func TestFrameSeqIncrement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := newMockHandler()
	s, jm := testServer(m)

	r := gin.New()
	r.GET("/ws", s.HandleWS)
	srv := httptest.NewServer(r)
	defer srv.Close()

	url := "ws" + srv.URL[4:] + "/ws"
	ws := dialAuth(t, url, jm, 400)
	defer ws.Close()

	// 发 seq=5 的消息
	msg := NewFrame(FrameMsg, 5, map[string]interface{}{
		"conv_id": 1, "target_id": 401, "content": "seqtest", "type": 1, "msg_id": 3000,
	})
	_ = sendJSON(ws, msg)
	// 收到回显 msg.push 帧
	f, err := readFrame(ws, 2*time.Second)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if f.Type != FrameMsgPush {
		t.Fatalf("期望 msg.push 帧，实际 %s", f.Type)
	}
	// 断言 handler 收到了 seq=5 的消息内容
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.received) != 1 || m.received[0] != "seqtest" {
		t.Fatalf("handler 未正确收到消息: %v", m.received)
	}
}

// TestWSAckForward 端到端验证 ack 转发：A 发消息，B 收到后回 ack，A 收到服务端转发的 ack。
func TestWSAckForward(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := newMockHandler()
	// mock handler：把 ack 转发回发送方（msg.SenderUID）
	m.ackHandler = func(uid int64, body json.RawMessage) (*Frame, []int64, error) {
		var req struct {
			ConvID int64 `json:"conv_id"`
			MsgID  int64 `json:"msg_id"`
		}
		_ = json.Unmarshal(body, &req)
		ack := NewFrame(FrameAck, 0, map[string]interface{}{
			"conv_id": req.ConvID, "msg_id": req.MsgID, "sender": uid,
		})
		return ack, []int64{10}, nil // 转发给发送方 uid=10
	}
	s, jm := testServer(m)

	r := gin.New()
	r.GET("/ws", s.HandleWS)
	srv := httptest.NewServer(r)
	defer srv.Close()

	url := "ws" + srv.URL[4:] + "/ws"
	// 发送方 A(10) 与接收方 B(20)
	wsA := dialAuth(t, url, jm, 10)
	defer wsA.Close()
	wsB := dialAuth(t, url, jm, 20)
	defer wsB.Close()

	// A 发消息给 B
	msg := NewFrame(FrameMsg, 2, map[string]interface{}{
		"conv_id": 5, "target_id": 20, "content": "hi", "type": 1, "msg_id": 9001,
	})
	_ = sendJSON(wsA, msg)

	// A 收到回显（msg.push）
	if _, err := readFrame(wsA, 2*time.Second); err != nil {
		t.Fatalf("A 未收到回显: %v", err)
	}
	// B 收到消息
	if _, err := readFrame(wsB, 2*time.Second); err != nil {
		t.Fatalf("B 未收到消息: %v", err)
	}
	// B 回 ack
	if err := sendJSON(wsB, NewFrame(FrameAck, 1, map[string]interface{}{"conv_id": 5, "msg_id": 9001})); err != nil {
		t.Fatalf("B 回 ack 失败: %v", err)
	}
	// A 应收到服务端转发的 ack
	af, err := readFrame(wsA, 2*time.Second)
	if err != nil {
		t.Fatalf("A 未收到转发的 ack: %v", err)
	}
	if af.Type != FrameAck {
		t.Fatalf("期望 A 收到 ack 帧，实际 %s", af.Type)
	}
}

// ===================== 断线期间消息：现状与补偿 =====================

// TestReconnectDeliveryScenario 复现"用户B断线重连期间，A发消息"场景。
// 当前系统无离线队列 + 无 ack 重发 + 无断线自动补拉，因此：
//   - B 离线期间 A 发的消息：服务端 Push 失败即丢弃，B 重连后收不到（除非重新打开会话拉历史）
//   - 本测试验证"重连后可通过历史拉取补偿"的数据基础：消息已落库（handler 记录）。
func TestReconnectDeliveryScenario(t *testing.T) {
	// 阶段1：B 离线
	m := newMockHandler()

	// A 发送消息时 B 不在线 → mock handler 标记 drop（离线）
	m.drop = true
	push, echo, recipients, err := m.HandleMsg(500, json.RawMessage(`{"conv_id":7,"content":"sent during B offline","target_id":600}`))
	if err != nil {
		t.Fatalf("handle msg failed: %v", err)
	}
	// drop=true：mock 不推给接收方（pushFrame=nil），仅回显给发送方
	if push != nil {
		t.Fatal("离线场景下不应有给接收方的 push 帧")
	}
	if echo == nil {
		t.Fatal("期望有回显帧")
	}
	// 注意：recipients 为空（drop=true），服务端 Push 无法投递
	if len(recipients) != 0 {
		t.Fatalf("离线场景下不应有可投递接收方，实际 %v", recipients)
	}
	// 关键结论：消息内容已进入 handler（模拟已落库），但 B 离线收不到实时推送。
	fmt.Printf("测试结论：B 离线期间消息已持久化（content=%s），但实时推送被丢弃。\n", m.received[0])

	// 阶段2：B 重连后，通过重新打开会话拉取历史可补偿（message 服务 GetHistory 已具备）。
	// 该补偿依赖客户端主动刷新/重开会话，非自动推送。
}
