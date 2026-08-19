/**
 * 语音通话模块：WebRTC P2P 媒体 + WS 信令状态机（1对1 单聊纯语音）。
 *
 * 状态流转：idle → outgoing(拨出振铃) / incoming(来电) → connecting(已应答待连通) → connected(通话中) → ended → idle
 * 信令动作（与服务端 gateway/call.go 对齐）：
 *   invite / answer / ice / reject / busy / cancel / hangup（透传）；
 *   offline（服务端代答：对端离线）；busy.set / busy.clear（服务端维护忙线，不透传）。
 *
 * 通话结束通过 onCallEnded 回调向 UI 层汇报（通话记录落库由 MainWindow 完成，双端各自记录）。
 */
import { reactive } from 'vue'
import { wsClient } from './ws'

// ICE 服务器：内置公共 STUN（NAT 打洞），如需更强穿透可后续在配置中追加 TURN
const ICE_SERVERS = [{ urls: ['stun:stun.l.google.com:19302', 'stun:stun.qq.com:3478'] }]

const ANSWER_TIMEOUT = 30 * 1000 // 拨出方：30s 无人接听自动取消
const RING_TIMEOUT = 15 * 1000 // 被叫方：15s 未响应按未接处理
const END_HOLD_MS = 1800 // 结束后状态保留时长（供 UI 展示结果文案），随后复位 idle

// 全局通话状态（UI 直接绑定）
export const callState = reactive({
  status: 'idle', // idle | outgoing | incoming | connecting | connected | ended
  callId: '',
  direction: '', // out 拨出 / in 来电
  peer: null, // { uid, name, avatar, color, convId }；来电时 name/avatar 由 MainWindow 补充
  result: '', // completed | missed | declined | cancelled | busy | offline | failed
  endReason: '', // 结束文案（failed 场景为具体原因）
  duration: 0, // 已通话秒数
  muted: false, // 本端麦克风静音
})

let pc = null // RTCPeerConnection
let localStream = null // 本地麦克风流
let remoteAudio = null // 远端音频播放元素
let pendingOffer = null // 来电暂存的 offer SDP（接听时消费）
let answerTimer = null // 拨出无应答定时器
let ringTimer = null // 来电未响应定时器
let durTimer = null // 通话计时器
let startedAt = 0 // 接通时刻（ms）
let endCb = null // 通话结束回调

/** 注册通话结束回调：(info) => {}，info: { callId, peer, direction, result, duration, reason } */
export function onCallEnded(fn) {
  endCb = fn
}

function peerUid() {
  return Number(callState.peer && callState.peer.uid) || 0
}

function genCallId() {
  return Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 10)
}

function sendSignal(action, extra = {}) {
  wsClient.sendCall({ call_id: callState.callId, action, to: peerUid(), ...extra })
}

// ---------- 拨出 ----------

/**
 * 发起语音通话。peer: { uid, name, avatar, color, convId }。
 * 麦克风不可用时直接结束并给出原因，UI 层据此 toast。
 */
export async function startCall(peer) {
  if (callState.status !== 'idle' || !peer || !peer.uid) return
  const callId = genCallId()
  Object.assign(callState, {
    status: 'outgoing',
    callId,
    direction: 'out',
    peer: { ...peer },
    result: '',
    endReason: '',
    duration: 0,
    muted: false,
  })
  try {
    localStream = await navigator.mediaDevices.getUserMedia({
      audio: { echoCancellation: true, noiseSuppression: true },
    })
  } catch (e) {
    console.warn('[call] getUserMedia failed', e)
    finish('failed', '无法访问麦克风，请检查设备与权限')
    return
  }
  // await 期间状态可能已变化（极端竞态），守卫
  if (callState.callId !== callId) {
    stopLocalStream()
    return
  }
  createPeerConnection()
  localStream.getTracks().forEach((t) => pc.addTrack(t, localStream))
  try {
    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
  } catch (e) {
    console.warn('[call] createOffer failed', e)
    finish('failed', '呼叫初始化失败')
    return
  }
  wsClient.sendCall({
    call_id: callId,
    action: 'invite',
    to: Number(peer.uid),
    payload: { sdp: pc.localDescription },
  })
  // 无应答守卫：30s 未收到 answer 自动取消（服务端不代答超时）
  answerTimer = setTimeout(() => {
    if (callState.callId !== callId) return
    if (callState.status === 'outgoing' || callState.status === 'connecting') {
      sendSignal('cancel')
      finish('missed', '对方无应答')
    }
  }, ANSWER_TIMEOUT)
}

// ---------- 来电处理 ----------

function onInvite(callId, from, payload) {
  // 本端已有通话（拨出/来电/通话中）：回 busy，服务端忙线键兜底
  if (callState.status !== 'idle') {
    wsClient.sendCall({ call_id: callId, action: 'busy', to: Number(from) })
    return
  }
  pendingOffer = (payload && payload.sdp) || null
  Object.assign(callState, {
    status: 'incoming',
    callId,
    direction: 'in',
    peer: { uid: from },
    result: '',
    endReason: '',
    duration: 0,
    muted: false,
  })
  ringTimer = setTimeout(() => {
    if (callState.callId === callId && callState.status === 'incoming') {
      finish('missed', '未接听')
    }
  }, RING_TIMEOUT)
}

/** 接听来电：取麦克风 → 应答 SDP → 等待 ICE 连通。 */
export async function acceptCall() {
  if (callState.status !== 'incoming' || !pendingOffer) return
  const callId = callState.callId
  clearRingTimer()
  try {
    localStream = await navigator.mediaDevices.getUserMedia({
      audio: { echoCancellation: true, noiseSuppression: true },
    })
  } catch (e) {
    console.warn('[call] getUserMedia failed', e)
    sendSignal('reject')
    finish('failed', '无法访问麦克风，请检查设备与权限')
    return
  }
  if (callState.callId !== callId) {
    stopLocalStream()
    return
  }
  callState.status = 'connecting'
  createPeerConnection()
  localStream.getTracks().forEach((t) => pc.addTrack(t, localStream))
  try {
    await pc.setRemoteDescription(pendingOffer)
    pendingOffer = null
    const answer = await pc.createAnswer()
    await pc.setLocalDescription(answer)
  } catch (e) {
    console.warn('[call] accept failed', e)
    finish('failed', '接听失败，请重试')
    return
  }
  sendSignal('answer', { payload: { sdp: pc.localDescription } })
}

/** 拒接来电。 */
export function rejectCall() {
  if (callState.status !== 'incoming') return
  sendSignal('reject')
  finish('declined', '已拒接')
}

// ---------- 通话中操作 ----------

/** 麦克风静音切换（track.enabled=false：不传输音频但保持连接）。 */
export function toggleMute() {
  if (!localStream) return
  const next = !callState.muted
  localStream.getAudioTracks().forEach((t) => {
    t.enabled = !next
  })
  callState.muted = next
}

/** 挂断：通话中发 hangup；拨出中发 cancel；来电中按拒接处理。 */
export function hangup() {
  const st = callState.status
  if (st === 'connected' || st === 'connecting') {
    sendSignal('hangup')
    finish('completed', '')
  } else if (st === 'outgoing') {
    sendSignal('cancel')
    finish('cancelled', '已取消')
  } else if (st === 'incoming') {
    rejectCall()
  }
}

// ---------- WebRTC ----------

function createPeerConnection() {
  pc = new RTCPeerConnection({ iceServers: ICE_SERVERS })
  pc.onicecandidate = (e) => {
    if (e.candidate && callState.callId) {
      sendSignal('ice', { payload: { candidate: e.candidate } })
    }
  }
  pc.ontrack = (e) => {
    // 远端音频：挂到隐藏 audio 元素播放（Electron 渲染进程支持自动播放）
    const stream = e.streams && e.streams[0]
    if (!stream) return
    if (!remoteAudio) {
      remoteAudio = new Audio()
      remoteAudio.autoplay = true
    }
    remoteAudio.srcObject = stream
  }
  pc.onconnectionstatechange = () => {
    if (!pc) return
    if (pc.connectionState === 'connected' && callState.status === 'connecting') {
      // ICE 连通：进入通话中，开始计时并上报忙线（服务端据此拦截并发呼叫）
      clearAnswerTimer()
      callState.status = 'connected'
      startedAt = Date.now()
      callState.duration = 0
      durTimer = setInterval(() => {
        callState.duration = Math.floor((Date.now() - startedAt) / 1000)
      }, 1000)
      wsClient.sendCall({ call_id: callState.callId, action: 'busy.set' })
    } else if (pc.connectionState === 'failed') {
      // 打洞失败/媒体断连：结束通话
      if (callState.status === 'connected' || callState.status === 'connecting') {
        finish('failed', '网络连接失败，通话已中断')
      }
    }
  }
}

// ---------- 信令入口 ----------

function handleSignal(body) {
  if (!body || !body.call_id) return
  const { call_id: callId, action, from, payload } = body
  // 本端相关守卫：非当前通话的信令忽略（残留帧/并发呼叫）
  const mine = callState.callId === callId
  switch (action) {
    case 'invite':
      onInvite(callId, from, payload)
      break
    case 'answer':
      if (mine && pc && payload && payload.sdp) {
        clearAnswerTimer()
        callState.status = 'connecting'
        pc.setRemoteDescription(payload.sdp).catch((e) => {
          console.warn('[call] setRemoteDescription(answer) failed', e)
          finish('failed', '连接建立失败')
        })
      }
      break
    case 'ice':
      if (mine && pc && payload && payload.candidate) {
        pc.addIceCandidate(payload.candidate).catch(() => {})
      }
      break
    case 'reject':
      if (mine) finish('declined', '对方已拒绝')
      break
    case 'busy':
      if (mine) finish('busy', '对方正在通话中')
      break
    case 'offline':
      if (mine) finish('offline', '对方当前不在线')
      break
    case 'cancel':
      // 拨出方取消：被叫侧按未接处理
      if (mine) finish('missed', '对方已取消呼叫')
      break
    case 'hangup':
      if (mine) finish(callState.status === 'connected' ? 'completed' : 'missed', '')
      break
    default:
      break
  }
}

wsClient.on('call', handleSignal)

// ---------- 结束与清理 ----------

function clearAnswerTimer() {
  if (answerTimer) {
    clearTimeout(answerTimer)
    answerTimer = null
  }
}

function clearRingTimer() {
  if (ringTimer) {
    clearTimeout(ringTimer)
    ringTimer = null
  }
}

function stopLocalStream() {
  if (localStream) {
    localStream.getTracks().forEach((t) => {
      try {
        t.stop()
      } catch {}
    })
    localStream = null
  }
}

// 结束通话：清理资源 → 更新状态 → 回调 UI 落通话记录 → 延时复位 idle
function finish(result, reason) {
  const snapshot = {
    callId: callState.callId,
    peer: callState.peer ? { ...callState.peer } : null,
    direction: callState.direction,
    result,
    duration: callState.duration,
    reason,
  }
  // 通话中/接通中结束需清忙线键（状态变更前发送，cleanup 依赖当前状态判断）
  const wasActive = callState.status === 'connected' || callState.status === 'connecting'
  clearAnswerTimer()
  clearRingTimer()
  if (durTimer) {
    clearInterval(durTimer)
    durTimer = null
  }
  if (wasActive && callState.callId) {
    wsClient.sendCall({ call_id: callState.callId, action: 'busy.clear' })
  }
  if (pc) {
    try {
      pc.close()
    } catch {}
    pc = null
  }
  stopLocalStream()
  if (remoteAudio) {
    remoteAudio.srcObject = null
  }
  pendingOffer = null

  callState.status = 'ended'
  callState.result = result
  callState.endReason = reason || ''
  try {
    endCb && endCb(snapshot)
  } catch (e) {
    console.error('[call] onCallEnded callback error', e)
  }
  setTimeout(() => {
    if (callState.status === 'ended') {
      Object.assign(callState, {
        status: 'idle',
        callId: '',
        direction: '',
        peer: null,
        result: '',
        endReason: '',
        duration: 0,
        muted: false,
      })
    }
  }, END_HOLD_MS)
}
