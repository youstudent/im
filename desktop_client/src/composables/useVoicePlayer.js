// 语音应用内播放：录音产物以 FILE(type=3) 消息发出（webm/m4a 等音频后缀），
// 气泡内 <audio> 直接播放，全局单 Audio 实例（切播新语音自动停旧；切会话/卸载时停止）。
import { reactive } from 'vue'
import { localdb } from '../api/localdb'
import { isAudioMsg } from '../utils/message'

export function useVoicePlayer({ showToast }) {
  const voiceState = reactive({ id: null, playing: false, duration: 0 })
  // 已播放过的语音（未播放的接收语音显示红点）。
  // 持久化在本地库消息表 voice_played 字段（随消息同库同生命周期，退群清理/保留期清理自动跟随）；
  // 内存 Set 仅作会话内缓存，覆盖网络拉取映射的新对象（本地加载路径直接读字段）
  const voicePlayedSet = reactive(new Set())
  let voiceAudio = null

  // 标记语音已播放：同时打在消息对象上（对象级响应式，立即触发该行重渲染清除红点）并落本地库
  function markVoicePlayed(m) {
    if (!m) return
    m.voicePlayed = true
    if (m.id == null) return
    voicePlayedSet.add(m.id)
    // 已同步消息才有 server_id（pending 行不可能被播放）；浏览器调试环境无本地库时自动降级为仅内存态
    if (!String(m.id).startsWith('local-')) {
      localdb.messages.markVoicePlayed(m.id)
    }
  }

  function voiceSrc(m) {
    return m.extra?.cacheUrl || m.extra?.url || ''
  }

  function isPlayingVoice(m) {
    return voiceState.id === m.id && voiceState.playing
  }

  // 接收的语音未播放过：气泡旁显示红点（自己发送的不显示）；
  // 优先看消息对象上的标记（本地加载时读 voice_played 字段/播放时即时打标），其次查会话内缓存集合
  function voiceUnread(m) {
    return m && m.type === 'in' && !m.isUploading && m.status !== 1 && isAudioMsg(m) && !m.voicePlayed && !voicePlayedSet.has(m.id)
  }

  // 时长展示：优先 extra.duration（发送时携带/探测回填），其次播放中读到的元数据
  function voiceDurationLabel(m) {
    let d = Number(m.extra?.duration) || 0
    if (!d && voiceState.id === m.id && Number.isFinite(voiceState.duration)) d = voiceState.duration
    d = Math.round(d)
    return d > 0 ? `${d}″` : ''
  }

  // 气泡宽度随时长渐变（微信风格）：未知时长给基础宽，最长封顶。
  // 宽度按消息首次渲染时固化：播放中才探测到时长时不再伸缩，避免气泡播放时变宽
  const voiceWidthCache = new Map()
  function voiceBubbleWidth(m) {
    const key = String(m.id ?? '')
    if (voiceWidthCache.has(key)) return voiceWidthCache.get(key)
    const known = Number(m.extra?.duration) || 0
    const px = (known ? Math.min(64 + Math.round(known) * 5, 170) : 64) + 'px'
    if (key) voiceWidthCache.set(key, px)
    return px
  }

  // 语音时长探测：对未知时长的音频串行预加载 metadata 回填 extra.duration，
  // 串行避免大量语音同时发请求；带超时守卫，异常不阻塞后续探测
  const voiceProbing = new Set()
  let voiceProbeChain = Promise.resolve()
  function probeVoiceDuration(m) {
    if (!isAudioMsg(m)) return
    if (Number(m.extra?.duration) > 0) return
    const src = voiceSrc(m)
    if (!src || voiceProbing.has(m.id)) return
    voiceProbing.add(m.id)
    voiceProbeChain = voiceProbeChain.then(() => new Promise((resolve) => {
      const a = new Audio()
      a.preload = 'metadata'
      let settled = false
      const done = () => {
        if (settled) return
        settled = true
        try { a.removeAttribute('src') } catch {}
        resolve()
      }
      a.addEventListener('loadedmetadata', () => {
        if (Number.isFinite(a.duration) && a.duration > 0 && m.extra) {
          m.extra.duration = Math.round(a.duration)
        }
        done()
      })
      a.addEventListener('error', done)
      setTimeout(done, 8000)
      a.src = src
    }))
  }

  function stopVoice() {
    if (voiceAudio) {
      voiceAudio.src = ''
      voiceAudio = null
    }
    voiceState.id = null
    voiceState.playing = false
    voiceState.duration = 0
  }

  function togglePlayVoice(m) {
    const src = voiceSrc(m)
    if (!src) {
      showToast('语音地址无效，无法播放', 'error')
      return
    }
    markVoicePlayed(m) // 点击即视为已播放，移除未读红点
    // 同一条：播放/暂停切换
    if (voiceState.id === m.id && voiceAudio) {
      if (voiceAudio.paused) {
        voiceAudio.play().catch((e) => onVoicePlayReject(e))
      } else {
        voiceAudio.pause()
      }
      return
    }
    // 切播：停旧建新
    stopVoice()
    const audio = new Audio(src)
    voiceAudio = audio
    voiceState.id = m.id
    // 事件守卫一律按元素引用判定（voiceAudio !== audio 即已失效）：
    // 重播同一条语音时，旧元素被 stopVoice 置空 src 触发的延迟 error 与
    // 新元素对应同一消息 id，若按 id 判定会把旧元素的失败误算到新播放上
    // （表现为有声但弹“语音播放失败”且播放动画消失）
    audio.addEventListener('loadedmetadata', () => {
      if (voiceAudio !== audio) return
      voiceState.duration = audio.duration || 0
      // 同步回填 extra.duration：气泡宽度/时长展示不再依赖播放动作
      if (Number.isFinite(audio.duration) && audio.duration > 0 && m.extra && !m.extra.duration) {
        m.extra.duration = Math.round(audio.duration)
      }
    })
    audio.addEventListener('play', () => {
      if (voiceAudio !== audio) return
      if (voiceState.id === m.id) voiceState.playing = true
    })
    audio.addEventListener('pause', () => {
      if (voiceAudio !== audio) return
      if (voiceState.id === m.id) voiceState.playing = false
    })
    audio.addEventListener('ended', () => {
      if (voiceAudio !== audio) return
      if (voiceState.id === m.id) {
        voiceState.playing = false
        voiceState.id = null
      }
    })
    audio.addEventListener('error', async () => {
      if (voiceAudio !== audio) return // 已失效的元素（被切播/置空 src）触发的事件一律忽略
      // 兜底重试：预签名 URL 过期时先下载入缓存，换本地地址重播一次
      if (localdb.available() && m.extra?.url && !m.extra?.cacheUrl) {
        let r = null
        try {
          r = await localdb.fileCache.resolve(m.extra.url, m.extra.key || '', m.extra.name || '')
        } catch {}
        if (r && r.hit && r.cacheUrl && voiceAudio === audio) {
          m.extra.cacheUrl = r.cacheUrl
          audio.src = r.cacheUrl
          audio.play().catch((e) => onVoicePlayReject(e))
          return
        }
      }
      showToast('语音播放失败', 'error')
      voiceState.id = null
      voiceState.playing = false
    })
    audio.play().catch((e) => onVoicePlayReject(e))
  }

  // play() 被拒：快速切换/暂停打断会抛 AbortError，属正常交互不提示；其余才报播放失败
  function onVoicePlayReject(e) {
    if (e && (e.name === 'AbortError' || e.name === 'InterruptedError')) return
    showToast('语音播放失败', 'error')
  }

  return {
    voiceState,
    isPlayingVoice,
    voiceUnread,
    voiceDurationLabel,
    voiceBubbleWidth,
    probeVoiceDuration,
    stopVoice,
    togglePlayVoice,
    voiceSrc,
  }
}
