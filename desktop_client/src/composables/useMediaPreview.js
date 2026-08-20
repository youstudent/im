// 图片预览 / 视频应用内播放 / 文件打开
import { ref } from 'vue'
import { localdb } from '../api/localdb'

export function useMediaPreview(ctx) {
  const { currentContact, realConvMap, noPersistSet, showToast, scrollToBottom, stopVoice } = ctx

  // ===== 图片预览 / 文件下载 =====
  const imagePreview = ref(null) // { url, name } | null

  // 图片加载完成回调：图片撑开高度后，若属于当前会话的图片则滚动到底部
  function onImageLoaded(msg) {
    if (!msg) return
    const contact = currentContact.value
    // 仅在图片属于当前会话、且是自己发送的消息时滚动到底部
    if (contact && contact.id && msg.id && contact.messages.some((m) => m.id === msg.id) && msg.type === 'out') {
      scrollToBottom()
    }
  }

  // 点击图片：优先调用系统图片查看器打开（Electron：本地缓存落盘后 shell.openPath）；
  // 浏览器调试环境、不落盘会话或系统打开失败时，降级为应用内大图预览
  async function openImage(msg) {
    const url = msg?.extra?.url
    const contact = currentContact.value
    const convIdStr = String(realConvMap.value[contact?.id] || contact?.convId || '')
    // 不落盘会话：系统打开需先下载落盘缓存，违背敏感会话不落地约定，直接应用内预览
    const noPersist = !!convIdStr && noPersistSet.value.has(convIdStr)
    if (url && localdb.available() && !noPersist) {
      try {
        const r = await localdb.fileCache.open(url, msg.extra?.key || '', msg.extra?.name || '')
        if (r && r.ok) return
      } catch {}
    }
    imagePreview.value = { url: msg.extra?.cacheUrl || url, name: msg.extra?.name || '图片' }
  }

  function closeImagePreview() {
    imagePreview.value = null
  }

  // ===== 视频应用内播放 =====
  // 以 FILE 类型发送的视频按后缀识别：Chromium 可原生解码的格式（mp4/webm/mov）
  // 渲染为视频气泡（缩略图 + 弹层播放器）；其它格式仍走文件卡片由系统程序打开。
  const videoPlayer = ref(null) // { url, name } | null

  function openVideo(msg) {
    const cacheUrl = msg.extra?.cacheUrl
    const url = msg.extra?.url
    const src = cacheUrl || url
    if (!src) {
      showToast('视频地址无效，无法播放', 'error')
      return
    }
    stopVoice() // 避免语音与视频同时出声
    // 本地缓存地址播放失败时可回退远端 URL 重试（见 onPlayerError）
    videoPlayer.value = {
      url: src,
      fallbackUrl: cacheUrl && url ? url : '',
      name: msg.extra?.name || '视频',
    }
  }

  // 播放器加载失败：当前源非 http（如 wcfile 缓存地址）且有远端地址时回退重试一次
  function onPlayerError(e) {
    const p = videoPlayer.value
    if (!p || !p.fallbackUrl) return
    const cur = String((e && e.target && e.target.src) || p.url || '')
    if (cur.startsWith('http')) return
    videoPlayer.value = { ...p, url: p.fallbackUrl, fallbackUrl: '' }
  }

  // 气泡内视频缩略图加载失败：本地缓存地址不可用时回退远端 URL
  function onBubbleVideoError(msg, e) {
    const el = e && e.target
    const url = msg.extra && msg.extra.url
    if (!el || !url) return
    const cur = String(el.src || '')
    if (!cur.startsWith('http')) el.src = url
  }

  // 关闭播放器：v-if 卸载 video 元素即停止播放
  function closeVideo() {
    videoPlayer.value = null
  }

  // 打开文件：优先走本地缓存 + 系统程序打开（Electron），失败兜底浏览器下载
  async function openFile(msg) {
    const url = msg.extra?.url
    if (!url) return
    if (localdb.available()) {
      try {
        const r = await localdb.fileCache.open(url, msg.extra?.key || '', msg.extra?.name || '')
        if (r && r.ok) return
      } catch (e) {
        // 系统无关联应用（如 .webm）时 shell.openPath 报错：提示后走下方下载兜底
        console.warn('[MediaPreview] 系统打开文件失败:', e?.message || e)
        showToast(e?.message || '系统无法打开该文件', 'error')
      }
    }
    // 兜底：触发浏览器/系统下载
    const a = document.createElement('a')
    a.href = url
    a.download = msg.extra?.name || 'file'
    a.target = '_blank'
    a.rel = 'noopener'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
  }

  return {
    imagePreview, onImageLoaded, openImage, closeImagePreview,
    videoPlayer, openVideo, onPlayerError, onBubbleVideoError, closeVideo,
    openFile,
  }
}
