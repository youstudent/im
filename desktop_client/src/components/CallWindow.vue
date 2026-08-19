<template>
  <div v-if="callState.status !== 'idle'" class="call-overlay" role="dialog" aria-label="语音通话">
    <div class="call-panel">
      <!-- 头像 -->
      <div class="call-avatar" :style="{ background: peerColor }">
        <span>{{ peerAvatar }}</span>
      </div>
      <!-- 对方名称 -->
      <div class="call-name">{{ peerName }}</div>
      <!-- 状态文案 -->
      <div class="call-status" :class="{ alert: isEndedAlert }">{{ statusText }}</div>

      <!-- 控制按钮区 -->
      <div class="call-actions">
        <!-- 来电：拒接 + 接听 -->
        <template v-if="callState.status === 'incoming'">
          <div class="call-btn-group">
            <button class="call-btn reject" aria-label="拒接" @click="rejectCall">
              <svg viewBox="0 0 24 24" width="26" height="26">
                <path d="M5 8.5c0-1.6 1.2-2.8 2.8-2.8.9 0 1.7.5 2.2 1.2l1.1 1.5c.3.4.3.9 0 1.2l-1.3 1.6a9.4 9.4 0 0 0 4.4 4.4l1.6-1.3c.3-.3.8-.3 1.2 0l1.5 1.1c.7.5 1.2 1.3 1.2 2.2 0 1.6-1.2 2.8-2.8 2.8h-.5C10 19.6 4.4 14 3.6 7.6v-.5z"
                  fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" transform="rotate(135 12 12)" />
              </svg>
            </button>
            <span class="call-btn-label">拒接</span>
          </div>
          <div class="call-btn-group">
            <button class="call-btn accept pulse" aria-label="接听" @click="acceptCall">
              <svg viewBox="0 0 24 24" width="26" height="26">
                <path d="M5 8.5c0-1.6 1.2-2.8 2.8-2.8.9 0 1.7.5 2.2 1.2l1.1 1.5c.3.4.3.9 0 1.2l-1.3 1.6a9.4 9.4 0 0 0 4.4 4.4l1.6-1.3c.3-.3.8-.3 1.2 0l1.5 1.1c.7.5 1.2 1.3 1.2 2.2 0 1.6-1.2 2.8-2.8 2.8h-.5C10 19.6 4.4 14 3.6 7.6v-.5z"
                  fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
              </svg>
            </button>
            <span class="call-btn-label">接听</span>
          </div>
        </template>

        <!-- 通话中：静音 + 挂断 -->
        <template v-else-if="callState.status === 'connected'">
          <div class="call-btn-group">
            <button
              class="call-btn mute"
              :class="{ active: callState.muted }"
              :aria-label="callState.muted ? '取消静音' : '静音'"
              @click="toggleMute"
            >
              <svg viewBox="0 0 24 24" width="24" height="24">
                <rect x="9.25" y="3.5" width="5.5" height="11" rx="2.75" fill="none" stroke="currentColor" stroke-width="1.8" />
                <path d="M6 11.5a6 6 0 0 0 12 0M12 17.5V21" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
                <line v-if="callState.muted" x1="4.5" y1="4.5" x2="19.5" y2="19.5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
              </svg>
            </button>
            <span class="call-btn-label">{{ callState.muted ? '已静音' : '静音' }}</span>
          </div>
          <div class="call-btn-group">
            <button class="call-btn reject" aria-label="挂断" @click="hangup">
              <svg viewBox="0 0 24 24" width="26" height="26">
                <path d="M5 8.5c0-1.6 1.2-2.8 2.8-2.8.9 0 1.7.5 2.2 1.2l1.1 1.5c.3.4.3.9 0 1.2l-1.3 1.6a9.4 9.4 0 0 0 4.4 4.4l1.6-1.3c.3-.3.8-.3 1.2 0l1.5 1.1c.7.5 1.2 1.3 1.2 2.2 0 1.6-1.2 2.8-2.8 2.8h-.5C10 19.6 4.4 14 3.6 7.6v-.5z"
                  fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" transform="rotate(135 12 12)" />
              </svg>
            </button>
            <span class="call-btn-label">挂断</span>
          </div>
        </template>

        <!-- 拨出/连接中：仅挂断/取消 -->
        <template v-else-if="callState.status === 'outgoing' || callState.status === 'connecting'">
          <div class="call-btn-group">
            <button class="call-btn reject" aria-label="取消呼叫" @click="hangup">
              <svg viewBox="0 0 24 24" width="26" height="26">
                <path d="M5 8.5c0-1.6 1.2-2.8 2.8-2.8.9 0 1.7.5 2.2 1.2l1.1 1.5c.3.4.3.9 0 1.2l-1.3 1.6a9.4 9.4 0 0 0 4.4 4.4l1.6-1.3c.3-.3.8-.3 1.2 0l1.5 1.1c.7.5 1.2 1.3 1.2 2.2 0 1.6-1.2 2.8-2.8 2.8h-.5C10 19.6 4.4 14 3.6 7.6v-.5z"
                  fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" transform="rotate(135 12 12)" />
              </svg>
            </button>
            <span class="call-btn-label">{{ callState.status === 'outgoing' ? '取消' : '挂断' }}</span>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, watch } from 'vue'
import { callState, acceptCall, rejectCall, hangup, toggleMute } from '../api/call'
import { notifySettings, showDesktopNotification } from '../api/notify'

const peerName = computed(() => (callState.peer && callState.peer.name) || '未知联系人')
const peerAvatar = computed(() => (callState.peer && callState.peer.avatar) || '?')
const peerColor = computed(() => (callState.peer && callState.peer.color) || '#64748b')

function formatDuration(sec) {
  const s = Math.max(0, sec || 0)
  const hh = Math.floor(s / 3600)
  const mm = String(Math.floor((s % 3600) / 60)).padStart(2, '0')
  const ss = String(s % 60).padStart(2, '0')
  return hh > 0 ? `${hh}:${mm}:${ss}` : `${mm}:${ss}`
}

// 状态文案
const statusText = computed(() => {
  switch (callState.status) {
    case 'outgoing':
      return '正在呼叫…'
    case 'incoming':
      return '来电 · 语音通话'
    case 'connecting':
      return '正在连接…'
    case 'connected':
      return formatDuration(callState.duration)
    case 'ended':
      return callState.endReason || endResultText(callState.result)
    default:
      return ''
  }
})

// 结束文案（endReason 优先，failed 等场景携带具体原因）
function endResultText(result) {
  switch (result) {
    case 'completed':
      return `通话结束 ${formatDuration(callState.duration)}`
    case 'missed':
      return callState.direction === 'in' ? '未接语音通话' : '对方无应答'
    case 'declined':
      return callState.direction === 'out' ? '对方已拒绝' : '已拒接'
    case 'cancelled':
      return '已取消呼叫'
    case 'busy':
      return '对方正在通话中'
    case 'offline':
      return '对方当前不在线'
    case 'failed':
      return '通话未接通'
    default:
      return '通话结束'
  }
}

// 结束态中需要醒目提示的失败类结果
const isEndedAlert = computed(
  () => callState.status === 'ended' && ['failed', 'busy', 'offline'].includes(callState.result)
)

// ---- 来电铃声：WebAudio 循环振铃（遵守提示音设置，默认关闭时仅弹面板） ----
let ringCtx = null
let ringTimer = null

function startRing() {
  try {
    const AC = window.AudioContext || window.webkitAudioContext
    if (!AC) return
    ringCtx = ringCtx || new AC()
    const ctx = ringCtx
    if (ctx.state === 'suspended') ctx.resume()
    const playOnce = () => {
      // 双音振铃：短促两组，柔和不刺耳
      const t = ctx.currentTime
      const tone = (freq, start, dur, peak = 0.12) => {
        const osc = ctx.createOscillator()
        const gain = ctx.createGain()
        osc.type = 'sine'
        osc.frequency.value = freq
        gain.gain.setValueAtTime(0.0001, t + start)
        gain.gain.exponentialRampToValueAtTime(peak, t + start + 0.02)
        gain.gain.exponentialRampToValueAtTime(0.0001, t + start + dur)
        osc.connect(gain)
        gain.connect(ctx.destination)
        osc.start(t + start)
        osc.stop(t + start + dur + 0.05)
      }
      tone(880, 0, 0.28)
      tone(659.25, 0.32, 0.28)
      tone(880, 0.8, 0.28)
      tone(659.25, 1.12, 0.28)
    }
    playOnce()
    ringTimer = setInterval(playOnce, 2400)
  } catch {
    /* 音频不可用时静默降级 */
  }
}

function stopRing() {
  if (ringTimer) {
    clearInterval(ringTimer)
    ringTimer = null
  }
}

watch(
  () => callState.status,
  (st) => {
    if (st === 'incoming') {
      // 铃声遵守"提示音"设置（默认关闭则仅弹面板）
      if (notifySettings.soundEnabled()) startRing()
      // 窗口未聚焦（最小化/切走）时弹系统通知，保证来电可见
      if (typeof document !== 'undefined' && !document.hasFocus()) {
        showDesktopNotification(peerName.value, '来电 · 语音通话')
      }
    } else {
      stopRing()
    }
  },
  { immediate: true }
)
</script>

<style scoped>
.call-overlay {
  position: fixed;
  inset: 0;
  z-index: 900;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(15, 23, 42, 0.62);
  backdrop-filter: blur(3px);
}

.call-panel {
  width: 340px;
  padding: 40px 28px 34px;
  display: flex;
  flex-direction: column;
  align-items: center;
  background: var(--im-surface, #fff);
  border-radius: 14px;
  box-shadow: 0 18px 48px rgba(15, 23, 42, 0.28);
}

.call-avatar {
  width: 76px;
  height: 76px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 30px;
  font-weight: 600;
}

.call-name {
  margin-top: 16px;
  font-size: 18px;
  font-weight: 600;
  color: var(--im-text-primary, #1f2329);
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.call-status {
  margin-top: 8px;
  font-size: 13px;
  color: var(--im-text-secondary, #646a73);
}

.call-status.alert {
  color: var(--im-danger, #f04545);
}

.call-actions {
  margin-top: 34px;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  gap: 56px;
}

.call-btn-group {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
}

.call-btn {
  width: 56px;
  height: 56px;
  border-radius: 50%;
  border: none;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  color: #fff;
  transition: transform 0.12s ease, opacity 0.12s ease;
}

.call-btn:hover {
  opacity: 0.9;
}

.call-btn:active {
  transform: scale(0.94);
}

.call-btn.accept {
  background: var(--im-online, #22c55e);
}

.call-btn.reject {
  background: var(--im-danger, #f04545);
}

.call-btn.mute {
  background: #e8eaee;
  color: var(--im-text-primary, #1f2329);
}

.call-btn.mute.active {
  background: var(--im-text-secondary, #646a73);
  color: #fff;
}

/* 来电接听按钮呼吸提示 */
.call-btn.pulse {
  animation: call-pulse 1.4s ease-in-out infinite;
}

@keyframes call-pulse {
  0%,
  100% {
    box-shadow: 0 0 0 0 rgba(34, 197, 94, 0.45);
  }
  50% {
    box-shadow: 0 0 0 12px rgba(34, 197, 94, 0);
  }
}

.call-btn-label {
  font-size: 12px;
  color: var(--im-text-secondary, #646a73);
}
</style>
