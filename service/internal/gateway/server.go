// server.go：WebSocket 长连接网关。处理建连鉴权、心跳、消息收发、已读回执。
package gateway

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"im/service/internal/config"
	"im/service/internal/pkg/jwt"
	"im/service/internal/pkg/log"
	"im/service/internal/store/redis"
)

// Upgrader WebSocket 升级器。
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// 允许任意来源（跨域/桌面端 file:// 均放行）
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Server 网关服务。
type Server struct {
	hub       *Hub
	jwt       *jwt.Manager
	handler   MessageHandler
	pongWait  time.Duration // 读超时：客户端在此间隔内无任何帧则判定失联踢线
	writeWait time.Duration // 写超时
}

// MessageHandler 业务回调：由服务端装配时注入，处理业务帧并返回需要投递的帧。
type MessageHandler interface {
	// HandleMsg 处理发送消息，返回 (推送帧, 回显帧, 接收方 uid 列表)。
	// 推送帧仅对新消息非 nil（幂等重发不重复推送接收方）；回显帧始终非 nil（供发送方确认/渲染）。
	HandleMsg(uid int64, body json.RawMessage) (pushFrame, echoFrame *Frame, recipients []int64, err error)
	// HandleRead 处理已读回执，返回需要广播给发送方的 read.sync 帧（可为 nil）与发送方 uid 列表。
	HandleRead(uid int64, body json.RawMessage) (*Frame, []int64, error)
	// HandleAck 处理送达回执，返回需要转发给消息发送方的 ack 帧（可为 nil）。
	HandleAck(uid int64, body json.RawMessage) (*Frame, []int64, error)
}

// NewServer 创建网关服务。rdb 为 im 封装的 Redis 客户端。
// cfg 为网关心跳/超时配置；为零值时使用安全默认值。
func NewServer(rdb *redis.Client, jwtMgr *jwt.Manager, handler MessageHandler, node string, cfg config.Gateway) *Server {
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 15
	}
	if cfg.WriteWait <= 0 {
		cfg.WriteWait = 15
	}
	if cfg.PongWait <= 0 {
		cfg.PongWait = 60 // 默认 60s：客户端 15s 心跳，60s 无任何帧则踢线
	}
	return &Server{
		hub:       NewHub(rdb.Client, node),
		jwt:       jwtMgr,
		handler:   handler,
		pongWait:  time.Duration(cfg.PongWait) * time.Second,
		writeWait: time.Duration(cfg.WriteWait) * time.Second,
	}
}

// Hub 返回连接注册中心。
func (s *Server) Hub() *Hub { return s.hub }

// HandleWS 升级为 WebSocket 并进入消息循环。
func (s *Server) HandleWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	client := newClient(conn, s)
	defer client.Close()

	// 首帧 auth 读超时（审计 P1）：恶意连接建连后不发帧可永久挂死占资源，
	// 10 秒内未完成鉴权即断开；鉴权成功后由 readLoop 接管读超时。
	_ = conn.SetReadDeadline(time.Now().Add(authWait))
	// 首帧必须为 auth，携带 access token
	if !client.auth() {
		return
	}
	s.hub.Add(client)
	log.L().Info("ws connected", "uid", client.uid, "remote", c.ClientIP())
	defer func() {
		s.hub.Remove(client)
		log.L().Info("ws disconnected", "uid", client.uid, "remote", c.ClientIP())
	}()
	// 断线重连补发：连接建立后补发离线期间收到的消息
	if n := s.hub.FlushOffline(client.uid, client); n > 0 {
		log.L().Info("offline messages flushed", "uid", client.uid, "count", n)
	}
	client.readLoop()
}

// authWait 首帧鉴权等待上限：建连后超过该时间未完成 auth 即断开（防 DoS 挂连接）。
const authWait = 10 * time.Second

// ---------- 客户端连接封装 ----------

type client struct {
	conn *websocket.Conn
	srv  *Server
	uid  int64
	mu   sync.Mutex // 写保护
}

func newClient(conn *websocket.Conn, srv *Server) *client {
	return &client{conn: conn, srv: srv}
}

func (cl *client) UID() int64          { return cl.uid }
func (cl *client) Send(_ int, data []byte) error {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	_ = cl.conn.SetWriteDeadline(time.Now().Add(cl.srv.writeWait))
	return cl.conn.WriteMessage(websocket.TextMessage, data)
}
func (cl *client) Close() error {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.conn.Close()
}

// auth 读取首帧并鉴权。
func (cl *client) auth() bool {
	_, data, err := cl.conn.ReadMessage()
	if err != nil {
		return false
	}
	var frame Frame
	if err := json.Unmarshal(data, &frame); err != nil || frame.Type != FrameAuth {
		return false
	}
	// auth body: { token: "..." }
	var body struct {
		Token string `json:"token"`
	}
	raw, _ := json.Marshal(frame.Body)
	_ = json.Unmarshal(raw, &body)
	claims, err := cl.srv.jwt.Parse(body.Token)
	if err != nil || claims.Type != "access" {
		return false
	}
	cl.uid = claims.UID
	return true
}

// readLoop 读取客户端帧并分发。实现心跳检测（读超时踢线）：
// 客户端在 pongWait 内未发送任何帧（含 heartbeat），则判定失联，断开连接。
func (cl *client) readLoop() {
	cl.conn.SetReadLimit(64 * 1024)
	_ = cl.conn.SetReadDeadline(time.Now().Add(cl.srv.pongWait))
	for {
		_, data, err := cl.conn.ReadMessage()
		if err != nil {
			// 读超时（客户端失联）或连接被对端关闭
			log.L().Info("ws read timeout or closed", "uid", cl.uid)
			break
		}
		// 收到任意帧即视为活跃，刷新读超时
		_ = cl.conn.SetReadDeadline(time.Now().Add(cl.srv.pongWait))
		var frame Frame
		if err := json.Unmarshal(data, &frame); err != nil {
			continue
		}
		cl.handle(&frame)
	}
}

func (cl *client) handle(f *Frame) {
	switch f.Type {
	case FrameHeartbeat:
		// 心跳续期在线状态（审计 P0）：presence 键 TTL 90s，不续期会在连接存活期间被误判离线
		cl.srv.hub.TouchPresence(cl.uid)
		cl.Send(1, mustJSON(NewFrame(FrameHeartbeat, f.Seq, gin.H{"pong": true})))
	case FrameMsg:
		cl.handleMsg(f)
	case FrameAck:
		if cl.srv.handler != nil {
			ackFrame, toUIDs, err := cl.srv.handler.HandleAck(cl.uid, rawBody(f))
			if err == nil && ackFrame != nil {
				// 把 ack 转发给消息发送方（接收方确认送达）
				for _, uid := range toUIDs {
					if uid == cl.uid {
						continue
					}
					cl.srv.hub.Push(uid, ackFrame)
				}
			}
		}
	case FrameRead:
		cl.handleRead(f)
	default:
		// typing 等暂不处理
	}
}

func (cl *client) handleMsg(f *Frame) {
	if cl.srv.handler == nil {
		return
	}
	pushFrame, echoFrame, recipients, err := cl.srv.handler.HandleMsg(cl.uid, rawBody(f))
	if err != nil {
		cl.Send(1, mustJSON(NewFrame("msg.error", f.Seq, gin.H{"error": err.Error()})))
		return
	}
	// 推送接收方（仅新消息；幂等重发 pushFrame 为 nil，不重复推送）
	if pushFrame != nil {
		for _, uid := range recipients {
			if uid == cl.uid {
				continue
			}
			cl.srv.hub.Push(uid, pushFrame)
		}
	}
	// 回显给发送方（用于确认 + 替换乐观渲染）。重复消息不重复推送接收方，但回显用于发送方本地状态同步。
	if echoFrame != nil {
		cl.Send(1, mustJSON(echoFrame))
	}
}

func (cl *client) handleRead(f *Frame) {
	if cl.srv.handler == nil {
		return
	}
	syncFrame, toUIDs, err := cl.srv.handler.HandleRead(cl.uid, rawBody(f))
	if err != nil {
		return
	}
	if syncFrame == nil {
		return
	}
	// 把已读回执广播给消息发送方（单聊对端），让对端把消息标记为已读
	for _, uid := range toUIDs {
		if uid == cl.uid {
			continue
		}
		cl.srv.hub.Push(uid, syncFrame)
	}
}

// ---------- 工具 ----------

func rawBody(f *Frame) json.RawMessage {
	b, _ := json.Marshal(f.Body)
	return b
}

func mustJSON(f *Frame) []byte {
	b, _ := json.Marshal(f)
	return b
}

// ErrNotFound 未找到（供上游判断）。
var ErrNotFound = errors.New("not found")
