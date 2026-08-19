/**
 * 通知模块：新消息桌面通知 + 提示音。
 *
 * 设置项持久化在 localStorage（UI 偏好层，秒级生效，无需入库）：
 *  - workchat:notify:desktop  桌面通知开关（默认开）
 *  - workchat:notify:sound    提示音开关（默认开）
 *
 * 桌面通知：Electron 环境走主进程原生 Notification（点击唤起窗口）；
 * 浏览器环境降级为 Web Notification API。
 * 提示音：WebAudio 合成双音「叮咚」，无需音频资源文件。
 */

const KEY_DESKTOP = 'workchat:notify:desktop'
const KEY_SOUND = 'workchat:notify:sound'

function readFlag(key, defaultValue) {
  try {
    const v = localStorage.getItem(key)
    if (v === null) return defaultValue
    return v === '1'
  } catch {
    return defaultValue
  }
}

function writeFlag(key, value) {
  try {
    localStorage.setItem(key, value ? '1' : '0')
  } catch {}
}

export const notifySettings = {
  desktopEnabled() {
    return readFlag(KEY_DESKTOP, true)
  },
  setDesktop(enabled) {
    writeFlag(KEY_DESKTOP, !!enabled)
  },
  soundEnabled() {
    return readFlag(KEY_SOUND, true)
  },
  setSound(enabled) {
    writeFlag(KEY_SOUND, !!enabled)
  },
}

// ---- 提示音：WebAudio 合成「叮咚」双音 ----
let audioCtx = null

export function playNotifySound() {
  try {
    const AC = window.AudioContext || window.webkitAudioContext
    if (!AC) return
    audioCtx = audioCtx || new AC()
    const ctx = audioCtx
    if (ctx.state === 'suspended') ctx.resume()
    const t = ctx.currentTime
    // 单音：正弦波 + 指数衰减包络，短促柔和
    const tone = (freq, start, dur, peak = 0.14) => {
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
    tone(880, 0, 0.22) // 高音「叮」
    tone(659.25, 0.16, 0.3) // 低音「咚」
  } catch {
    /* 音频不可用时静默降级 */
  }
}

// ---- 桌面通知 ----
// Electron：主进程原生通知（点击唤起并聚焦窗口）；浏览器：Web Notification。
export async function showDesktopNotification(title, body) {
  // Electron 环境
  if (typeof window !== 'undefined' && window.electronAPI?.notification?.show) {
    try {
      await window.electronAPI.notification.show(title, body)
      return
    } catch {
      /* 降级到 Web Notification */
    }
  }
  // 浏览器环境
  try {
    if (typeof Notification === 'undefined') return
    if (Notification.permission === 'granted') {
      new Notification(title, { body })
    } else if (Notification.permission !== 'denied') {
      const perm = await Notification.requestPermission()
      if (perm === 'granted') new Notification(title, { body })
    }
  } catch {}
}

/**
 * 新消息提醒统一入口：按设置决定是否播提示音 / 弹桌面通知。
 * @param {string} title 通知标题（发送者昵称 / 群名）
 * @param {string} body  通知正文（消息预览）
 */
export function notifyNewMessage(title, body) {
  if (notifySettings.soundEnabled()) playNotifySound()
  if (notifySettings.desktopEnabled()) showDesktopNotification(title, body)
}
