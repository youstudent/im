/**
 * WebSocket 长连接客户端：建连鉴权、心跳、消息收发、已读回执、断线重连。
 * 帧格式对齐服务端 docs/IM系统架构设计.md 4.2。
 */
import { tokenStore } from './token'
import { trySilentRefresh } from './http'

const WS_URL = 'ws://127.0.0.1:8080/ws'

let ws = null
let reconnectTimer = null
let heartbeatTimer = null
let heartbeatMiss = 0
let shouldReconnect = true
let seq = 0

// 鉴权失败自愈：连接后秒断且未收到任何帧 ≈ 首帧鉴权失败（token 过期/无效），
// 先静默刷新再重连，避免“拿失效 token 无限重连”死循环；连续失败超限则判定登录失效
let openedAt = 0
let gotFrame = false
let authFailStreak = 0
const AUTH_FAIL_MAX = 3

// WS 连接状态：connected 已连接 / connecting 连接中 / disconnected 已断开
let connectionState = 'disconnected'

// ack 确认 / 超时重发
const pendingAcks = new Map() // msgId -> { body, sendAt, retries }
let ackTimer = null
const ACK_TIMEOUT = 5000 // 5s 未确认视为超时
const ACK_MAX_RETRIES = 3 // 最多重发 3 次

// 事件订阅
const listeners = {
  message: [], // (msg) => {} 收到新消息
  read: [], // (data) => {} 对方已读
  open: [],
  close: [],
  error: [],
}

function emit(type, payload) {
  ;(listeners[type] || []).forEach((fn) => {
    try {
      fn(payload)
    } catch (e) {
      console.error('[ws] listener error', type, e)
    }
  })
}

export const wsClient = {
  on(type, fn) {
    listeners[type] = listeners[type] || []
    listeners[type].push(fn)
    return () => {
      listeners[type] = listeners[type].filter((f) => f !== fn)
    }
  },

  isConnected() {
    return !!ws && ws.readyState === WebSocket.OPEN
  },

  // 获取当前连接状态：'connected' | 'connecting' | 'disconnected'
  getStatus() {
    return connectionState
  },

  connect() {
    shouldReconnect = true
    if (this.isConnected()) return
    doConnect()
  },

  disconnect() {
    shouldReconnect = false
    stopHeartbeat()
    setConnectionState('disconnected')
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    if (ws) {
      ws.close()
      ws = null
    }
  },

  // 发送消息帧（convType: 1 单聊 / 2 群聊）
  // conv_id / msg_id 为雪花 ID，服务端以字符串接收，故显式转字符串避免精度丢失
  // type: 1 文本 / 2 图片 / 3 文件；extra: 图片/文件的 URL 等元数据（JSON 字符串）
  // 消息发送后进入"待确认队列"，未收到 ack 时超时重发（保证送达）
  // 返回是否真正发出：连接非 OPEN 时返回 false（不静默丢弃，修复审计 P0），调用方据此降级 HTTP
  sendMessage(msgId, convId, targetId, convType, type, content, extra) {
    const body = {
      msg_id: String(msgId),
      conv_id: String(convId),
      // 服务端 SendReq.target_id 为 int64：本地库/占位会话的 targetId 可能是字符串，统一转数字
      target_id: Number(targetId),
      conv_type: convType,
      type: type,
      content: content,
    }
    if (extra) body.extra = extra
    if (!sendFrame('msg', body)) return false
    trackPendingAck(msgId, body)
    return true
  },

  // 已读回执（conv_id 为雪花 ID，传字符串）
  sendRead(convId, seq) {
    sendFrame('read', { conv_id: String(convId), seq })
  },

  // 送达回执（conv_id / msg_id 传字符串）
  sendAck(convId, msgId) {
    sendFrame('ack', { conv_id: String(convId), msg_id: String(msgId) })
  },
}

// 更新连接状态并通知订阅者（status 事件）。
function setConnectionState(state) {
  if (connectionState === state) return
  connectionState = state
  emit('status', state)
}

function doConnect() {
  if (!shouldReconnect) return
  if (typeof WebSocket === 'undefined') return
  // 已有连接在建立中/已建立：不重复建连（防重连定时器与登录后手动 connect 竞态出双 socket）
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) return
  setConnectionState('connecting')
  ws = new WebSocket(WS_URL)

  ws.onopen = async () => {
    openedAt = Date.now()
    gotFrame = false
    heartbeatMiss = 0
    startHeartbeat()
    // 重连成功：重发待确认队列中的消息（服务端按 msg_id 幂等去重，不会重复落库/推送；
    // 修复旧实现直接清空队列导致"消息从未到达服务端"场景下静默丢失）
    resendPendingAcks()
    setConnectionState('connected')
    // 首帧鉴权：token 缺失（过期清理/解密失败）时先静默刷新再试，避免拿空/旧 token 去认证
    let token = await tokenStore.getAccessToken()
    if (!token && (await trySilentRefresh())) token = await tokenStore.getAccessToken()
    if (!token) {
      giveUpReconnect()
      try { ws && ws.close() } catch {}
      return
    }
    sendFrame('auth', { token })
    emit('open')
  }

  ws.onmessage = (e) => {
    let frame
    try {
      frame = JSON.parse(e.data)
    } catch {
      return
    }
    gotFrame = true
    handleFrame(frame)
  }

  ws.onclose = async () => {
    emit('close')
    setConnectionState('disconnected')
    stopHeartbeat()
    ws = null
    // 判断是否登录失效：用同步检查 refresh token 是否存在（不走 IPC 解密）。
    // 旧实现用异步 getRefreshToken：应用退出瞬间主进程 IPC 失败会返回 null，
    // 被误判为登录失效 → 清空令牌，导致重启后需要重新登录。
    if (!tokenStore.hasRefreshToken()) {
      shouldReconnect = false
      if (typeof window !== 'undefined') {
        window.dispatchEvent(new CustomEvent('auth:expired', { detail: { source: 'ws' } }))
      }
      return
    }
    if (!shouldReconnect) return
    // 连接后秒断且未收到任何帧：大概率首帧鉴权失败（access token 过期/无效）。
    // 先静默刷新换新 token 再重连，打破“拿失效 token 无限重连”死循环；
    // 连续 AUTH_FAIL_MAX 次仍失败则判定登录失效，避免永久空转。
    const quickAuthFail = !gotFrame && openedAt > 0 && Date.now() - openedAt < 5000
    if (quickAuthFail) {
      authFailStreak++
      if (authFailStreak > AUTH_FAIL_MAX) {
        // 超限后再给一次刷新机会：期间用户可能已重新登录（refresh 有效），避免误杀新会话；
        // 刷新仍失败才判定登录失效（终止空转）
        if (await trySilentRefresh()) {
          authFailStreak = 0
          scheduleReconnect()
          return
        }
        giveUpReconnect()
        return
      }
      trySilentRefresh().finally(() => scheduleReconnect())
      return
    }
    authFailStreak = 0
    scheduleReconnect()
  }

  ws.onerror = (e) => {
    emit('error', e)
  }
}

function handleFrame(frame) {
  switch (frame.type) {
    case 'heartbeat':
      // 服务端心跳响应，清空缺失计数
      heartbeatMiss = 0
      break
    case 'msg.push':
      // 收到消息：回送达回执（ack），让发送方确认
      const b = frame.body || {}
      if (b.id && b.conv_id) {
        sendFrame('ack', { conv_id: b.conv_id, msg_id: b.id })
      }
      emit('message', frame.body)
      break
    case 'read.sync':
      emit('read', frame.body)
      break
    case 'msg.error':
      emit('error', frame.body)
      break
    case 'ack':
      // 送达回执：接收方已确认收到该消息，从待确认队列移除
      const ab = frame.body || {}
      if (ab.msg_id) pendingAcks.delete(String(ab.msg_id))
      emit('ack', frame.body)
      break
    case 'social':
      emit('social', frame.body)
      break
    case 'kick':
      // 被服务端强制下线（账号被管理员禁用等）：停止重连、清空令牌、跳转登录页
      const kickBody = frame.body || {}
      handleKicked(kickBody.reason)
      break
    default:
      break
  }
}

// 停止重连并判定登录失效：通知全局跳转登录页（不带 reason，App 展示默认“登录已失效，请重新登录”文案；
// reason 仅用于 kick 等需要展示具体原因的场景）。
function giveUpReconnect() {
  shouldReconnect = false
  stopReconnect()
  setConnectionState('disconnected')
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent('auth:expired', { detail: { source: 'ws' } }))
  }
}

// 被强制下线（如账号被禁用）：停止重连、清空本地登录态并通知全局跳转登录页。
function handleKicked(reason) {
  shouldReconnect = false
  stopHeartbeat()
  stopReconnect()
  if (ws) {
    try {
      ws.close()
    } catch {}
    ws = null
  }
  setConnectionState('disconnected')
  if (typeof window !== 'undefined') {
    window.dispatchEvent(
      new CustomEvent('auth:expired', { detail: { source: 'ws', reason: reason || '账号已被禁用' } })
    )
  }
}

// 停止重连定时器。
function stopReconnect() {
  if (reconnectTimer) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
}

function sendFrame(type, body) {
  seq++
  const frame = { ver: 1, type, seq, body }
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(frame))
    return true
  }
  // 连接不可用：帧未发出，返回 false 由调用方决定降级策略（修复断线瞬间静默丢帧）
  return false
}

// 心跳：约 30s 发一次；连续未收到响应则判定断线重连
function startHeartbeat() {
  stopHeartbeat()
  heartbeatTimer = setInterval(() => {
    heartbeatMiss++
    sendFrame('heartbeat', {})
    if (heartbeatMiss > 3) {
      // 服务端疑似不可达，主动断开触发重连
      try {
        ws && ws.close()
      } catch {}
    }
  }, 15000)
}

function stopHeartbeat() {
  if (heartbeatTimer) {
    clearInterval(heartbeatTimer)
    heartbeatTimer = null
  }
  heartbeatMiss = 0
}

function scheduleReconnect() {
  if (reconnectTimer) return
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    doConnect()
  }, 3000)
}

// ===================== ack 确认 / 超时重发 =====================
// 将发送的消息加入待确认队列，并启动超时重发检查。
function trackPendingAck(msgId, body) {
  pendingAcks.set(String(msgId), { body, sendAt: Date.now(), retries: 0 })
  ensureAckTimer()
}

// 确保 ack 超时检查定时器在运行。
function ensureAckTimer() {
  if (ackTimer) return
  ackTimer = setInterval(checkPendingAcks, 1000)
}

// 检查待确认消息：超过 ACK_TIMEOUT 未确认的重发；超过最大重试次数的放弃。
function checkPendingAcks() {
  if (pendingAcks.size === 0) {
    if (ackTimer) {
      clearInterval(ackTimer)
      ackTimer = null
    }
    return
  }
  // 断线期间不消耗重试次数：消息留在队列，重连后由本地库 pending 队列/后续检查处理（修复审计 P0）
  if (!ws || ws.readyState !== WebSocket.OPEN) return
  const now = Date.now()
  pendingAcks.forEach((entry, msgId) => {
    if (now - entry.sendAt < ACK_TIMEOUT) return
    if (entry.retries >= ACK_MAX_RETRIES) {
      // 重发次数用尽，放弃并移除（服务端已落库，接收方可通过离线队列/历史拉取补偿）
      console.warn('[ws] ack 未确认，放弃重发', msgId)
      pendingAcks.delete(msgId)
      return
    }
    // 重发该消息
    entry.retries++
    entry.sendAt = now
    if (ws && ws.readyState === WebSocket.OPEN) {
      sendFrame('msg', entry.body)
      console.warn('[ws] 消息未确认，重发', msgId, '次数', entry.retries)
    }
  })
}

// 连接建立后重发待确认队列中的消息（审计 P0 残余修复）：
// 消息若从未到达服务端（发送瞬间断线且 HTTP 降级也失败），服务端离线补发无从兑现，
// 重连后逐条重发是唯一兼容手段；服务端按 msg_id 幂等去重，重复重发无副作用。
function resendPendingAcks() {
  if (pendingAcks.size === 0) return
  const now = Date.now()
  pendingAcks.forEach((entry, msgId) => {
    entry.retries = 0
    entry.sendAt = now
    sendFrame('msg', entry.body)
    console.log('[ws] 重连后重发待确认消息', msgId)
  })
  ensureAckTimer()
}
