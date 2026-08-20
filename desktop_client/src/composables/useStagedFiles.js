// 附件暂存区 / 语音录制 / 拖拽上传 / 暂存媒体发送
// 图片/文件选择或拖入后先在输入框上方暂存，点击发送才调真实上传接口
import { ref, computed } from 'vue'
import { fileApi } from '../api/file'
import { localdb } from '../api/localdb'
import { MSG_TYPE, formatMsgTime } from '../utils/format'
import { messagePreview } from '../utils/message'
import {
  isAllowedFile, isAllowedImage, pickFile,
  IMAGE_ACCEPT, FILE_ACCEPT, AUDIO_EXT_RE,
  IMAGE_MAX_SIZE, VOICE_MAX_SIZE, MAX_UPLOAD_SIZE, VOICE_MAX_SECONDS,
} from '../utils/fileGuard'

export function useStagedFiles(ctx) {
  const { activeId, hasActiveContact, currentContact, realConvMap, showToast, sendMessage } = ctx

  // 按会话隔离：暂存项绑定当前会话 id，切换会话互不影响，切回时恢复
  const stagedFilesMap = ref({}) // { [会话id]: [{ id, file, kind, previewUrl, name, size }] }
  const stagedFiles = computed(() => stagedFilesMap.value[activeId.value] || [])

  // 当前会话的暂存列表（无则创建）；无激活会话时返回 null
  function currentStagedList() {
    const key = activeId.value
    if (!key) return null
    if (!stagedFilesMap.value[key]) stagedFilesMap.value[key] = []
    return stagedFilesMap.value[key]
  }

  // 暂存附件：统一校验白名单/大小（toastSkip：按钮选择单文件场景静默跳过非法项；duration：录音时长；voice：录音产物按语音类型处理）
  function stageFiles(files, { toastSkip = false, duration = 0, voice = false } = {}) {
    let added = 0
    for (const f of files || []) {
      // 类型判定：录音产物（voice=true）按语音处理，其余按图片/文件白名单
      let kind = ''
      if (voice && AUDIO_EXT_RE.test(f.name)) kind = 'voice'
      else if (isAllowedImage(f.name)) kind = 'image'
      else if (isAllowedFile(f.name)) kind = 'file'
      if (!kind) {
        if (toastSkip) showToast('不支持发送该类型文件，仅支持图片与文档/压缩包/音视频等常见格式', 'error')
        else showToast(`不支持的文件类型：${f.name}`, 'error')
        continue
      }
      // 分类大小限制：图片 20MB / 语音 10MB / 普通文件 200MB（后端同规则拒绝，前置拦截免无效上传）
      const limit = kind === 'image' ? IMAGE_MAX_SIZE : kind === 'voice' ? VOICE_MAX_SIZE : MAX_UPLOAD_SIZE
      if (f.size > limit) {
        const limitText = kind === 'image' ? '20MB' : kind === 'voice' ? '10MB' : '200MB'
        const typeName = kind === 'image' ? '图片' : kind === 'voice' ? '语音' : '文件'
        if (toastSkip) showToast(`${typeName}大小超限（上限 ${limitText}）`, 'error')
        else showToast(`「${f.name}」大小超限（上限 ${limitText}），已跳过`, 'error')
        continue
      }
      const list = currentStagedList()
      if (!list) {
        if (toastSkip) showToast('请先选择会话再添加附件', 'error')
        continue
      }
      list.push({
        id: `stg-${Date.now()}-${Math.floor(Math.random() * 1000)}-${added}`,
        file: f,
        kind,
        previewUrl: kind === 'image' ? URL.createObjectURL(f) : '',
        name: f.name,
        size: f.size,
        duration: duration || 0, // 录音产物携带时长，随 extra 发出供气泡展示
      })
      added++
    }
    if (!toastSkip && added > 1) showToast(`已添加 ${added} 个附件，点击发送发出`, 'info')
  }

  // 移除暂存附件（释放预览 ObjectURL；列表清空后移除会话键）
  function removeStaged(id) {
    const list = stagedFilesMap.value[activeId.value]
    if (!list) return
    const idx = list.findIndex((s) => s.id === id)
    if (idx < 0) return
    const [item] = list.splice(idx, 1)
    if (item.previewUrl) URL.revokeObjectURL(item.previewUrl)
    if (!list.length) delete stagedFilesMap.value[activeId.value]
  }

  // 选择图片：白名单/大小校验后暂存到输入区（点击发送才真正上传，参考微信）
  // accept 不用 image/*（包含 svg，属后端危险类型黑名单），改用与后端对齐的扩展名列表
  async function sendImage() {
    const file = await pickFile(IMAGE_ACCEPT)
    if (!file) return
    stageFiles([file], { toastSkip: true })
  }

  // 选择文件：白名单/大小校验后暂存到输入区（点击发送才真正上传）
  async function sendFile() {
    const file = await pickFile(FILE_ACCEPT)
    if (!file) return
    stageFiles([file], { toastSkip: true })
  }

  // ===== 语音录制：点击开始 / 再点结束，录音产物进附件暂存区（随发送一起上传，webm 在文件白名单内）=====
  const recording = ref(false)
  let mediaRecorder = null
  let recordChunks = []
  let recordStream = null
  let recordStartedAt = 0 // 录音开始时间：结束时计算时长随 extra 发出
  let recordStopTimer = null // 60s 上限自动停止定时器

  async function toggleRecordVoice() {
    // 录音中：再点一次结束
    if (recording.value) {
      recording.value = false
      if (recordStopTimer) {
        clearTimeout(recordStopTimer)
        recordStopTimer = null
      }
      mediaRecorder?.stop()
      return
    }
    if (!navigator.mediaDevices?.getUserMedia) {
      showToast('当前环境不支持录音', 'error')
      return
    }
    // 麦克风检测：无任何音频输入设备 → 提示（不弹权限直接告知）
    try {
      const devices = await navigator.mediaDevices.enumerateDevices()
      if (!devices.some((d) => d.kind === 'audioinput')) {
        showToast('未检测到麦克风设备，请检查是否已连接麦克风', 'error')
        return
      }
    } catch {}
    // 申请麦克风权限：拒绝/无设备时 getUserMedia 会抛错
    try {
      recordStream = await navigator.mediaDevices.getUserMedia({ audio: true })
    } catch (e) {
      console.warn('[StagedFiles] 麦克风访问失败:', e?.message || e)
      showToast('无法访问麦克风，请检查系统权限或麦克风连接', 'error')
      return
    }
    recordChunks = []
    mediaRecorder = new MediaRecorder(recordStream)
    mediaRecorder.ondataavailable = (e) => {
      if (e.data && e.data.size) recordChunks.push(e.data)
    }
    mediaRecorder.onstop = () => {
      recordStream?.getTracks().forEach((t) => t.stop())
      recordStream = null
      const mime = mediaRecorder?.mimeType || 'audio/webm'
      const blob = new Blob(recordChunks, { type: mime })
      recordChunks = []
      if (!blob.size) {
        showToast('录音时长过短，请重试', 'error')
        return
      }
      const ext = mime.includes('mp4') || mime.includes('aac') ? 'm4a' : 'webm'
      // 时长夹在 1~60s：定时器到点自动停止，此处兼容微小的计时漂移，避免后端 60s 校验拒绝
      const durSec = Math.max(1, Math.min(VOICE_MAX_SECONDS, Math.round((Date.now() - recordStartedAt) / 1000)))
      const d = new Date()
      const pad = (n) => String(n).padStart(2, '0')
      const name = `语音_${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}_${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}.${ext}`
      stageFiles([new File([blob], name, { type: mime })], { toastSkip: true, duration: durSec, voice: true })
      showToast('录音已加入暂存，点击发送发出', 'info')
    }
    mediaRecorder.start()
    recordStartedAt = Date.now()
    recording.value = true
    // 60s 上限：到点自动结束录音并提示（与后端语音时长校验一致）
    recordStopTimer = setTimeout(() => {
      recordStopTimer = null
      if (recording.value) {
        showToast(`录音已达 ${VOICE_MAX_SECONDS} 秒上限，已自动结束`, 'info')
        toggleRecordVoice()
      }
    }, VOICE_MAX_SECONDS * 1000)
  }

  // 乐观预览保护：附件上传期间（大文件可达数十秒）消息尚未落库，列表重建
  // （切页/事件刷新）会用本地库/服务端的旧摘要覆盖内存中的乐观预览（如 [视频] 变回 [文件]），
  // 此处按会话记录乐观预览，applyConvList 应用时优先还原；发送流程结束后清理。
  const uploadingPreviewMap = ref({}) // { [convId或面板id]: { preview, time } }

  // 暂存附件发送：本地立即展示预览 + 旋转加载态（微信风格）→ 后台上传 OSS → 成功后才真正发出；
  // 上传失败气泡置为发送失败；上传成功后的服务端发送/回显/超时守卫复用 sendMessage 既有机制
  async function sendStagedMedia(item) {
    const contact = currentContact.value
    if (!contact || !item) return
    const isImage = item.kind === 'image'
    const msgType = isImage ? MSG_TYPE.IMAGE : MSG_TYPE.FILE
    // 上传中：本地预览直显 + 旋转 spinner；URL/名称等元数据随上传成功后的真实值发出
    const prepared = {
      isUploading: true,
      extra: isImage
        ? { url: item.previewUrl, cacheUrl: item.previewUrl, name: item.name, size: item.size }
        : { name: item.name, size: item.size, duration: item.duration || 0 },
      time: formatMsgTime(Math.floor(Date.now() / 1000)),
    }
    // 记录乐观预览：上传窗口内列表刷新不丢失（语音/视频按 extra.name 识别为 [语音]/[视频]）
    const previewKey = String(realConvMap.value[contact.id] || contact.convId || contact.id)
    uploadingPreviewMap.value[previewKey] = {
      preview: messagePreview({ msgType, extra: prepared.extra, text: '', type: 'out' }),
      time: Math.floor(Date.now() / 1000),
    }
    const sendP = sendMessage(msgType, '', null, { prepared, deferSend: true })
    // 释放暂存预览地址：乐观消息 extra 已持有同一 blob URL，重复 revoke 会导致预览失效
    item.previewUrl = ''
    try {
      // 上传类型：录音产物按 voice 上报（后端校验时长≤60s/大小≤10MB），时长随预签名请求携带
      const { downloadUrl, objectKey } = await fileApi.uploadFile(item.file, item.kind, { duration: item.duration || 0 })
      const optimistic = await sendP
      if (optimistic) optimistic.isUploading = false
      // 上传成功：带真实 URL 走服务端发送（WS/HTTP 兜底与回显替换由 sendMessage 接管）
      await optimistic?.__send?.({
        content: downloadUrl || '',
        extra: { url: downloadUrl, key: objectKey, name: item.name, size: item.size, duration: item.duration || 0 },
      })
    } catch (e) {
      console.warn('[StagedFiles] 上传失败:', e?.message || e)
      const optimistic = await sendP
      if (optimistic && optimistic.isPending) {
        optimistic.isUploading = false
        optimistic.readAt = '发送失败'
        optimistic.isPending = false
        if (optimistic.localId) localdb.messages.setSyncState(optimistic.localId, 'failed')
      }
      showToast('上传失败，请稍后重试', 'error')
    } finally {
      // 发送流程结束（成功/失败）：解除乐观预览保护，后续由 WS 回显/正常摘要接管
      delete uploadingPreviewMap.value[previewKey]
    }
  }

  // ===== 拖拽上传：拖动图片/文件到聊天区释放即发送（复用白名单/大小校验）=====
  const dragging = ref(false)
  let dragDepth = 0

  // 窗口级拖拽拦截：防止拖文件到非聊天区时 Electron 默认导航到 file:// 白屏
  function preventWindowDrag(e) {
    if ((e.dataTransfer?.types || []).includes('Files')) {
      e.preventDefault()
    }
  }

  function onChatDragEnter(e) {
    if (!(e.dataTransfer?.types || []).includes('Files')) return
    e.preventDefault()
    dragDepth++
    dragging.value = hasActiveContact.value
  }

  function onChatDragOver(e) {
    if (!(e.dataTransfer?.types || []).includes('Files')) return
    e.preventDefault()
    e.dataTransfer.dropEffect = hasActiveContact.value ? 'copy' : 'none'
  }

  function onChatDragLeave() {
    dragDepth = Math.max(0, dragDepth - 1)
    if (!dragDepth) dragging.value = false
  }

  // 释放：拖入的文件先进暂存区（输入框上方预览），点击发送才真正上传；白名单/大小校验由 stageFiles 统一处理
  function onChatDrop(e) {
    e.preventDefault()
    dragDepth = 0
    dragging.value = false
    if (!hasActiveContact.value) return
    const files = Array.from(e.dataTransfer?.files || [])
    if (!files.length) return
    stageFiles(files)
  }

  return {
    stagedFilesMap, stagedFiles, stageFiles, removeStaged,
    sendImage, sendFile,
    recording, toggleRecordVoice,
    uploadingPreviewMap, sendStagedMedia,
    dragging, preventWindowDrag, onChatDragEnter, onChatDragOver, onChatDragLeave, onChatDrop,
  }
}
