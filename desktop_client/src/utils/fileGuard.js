// 发送附件类型白名单与大小限制（与后端 file 服务校验规则对齐）

// ===== 发送文件类型白名单：仅允许文档/表格/演示/压缩包/文本/音视频，禁止可执行与脚本类文件；
// 图片不在此列（走独立的"发送图片"按钮）。accept 仅辅助筛选，系统选择器可绕过，须校验兜底 =====
export const FILE_EXT_WHITELIST = new Set([
  // 办公文档
  'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx', 'pdf',
  'wps', 'et', 'dps', 'ofd', 'csv',
  // 文本/代码
  'txt', 'md', 'json', 'xml', 'log',
  // 压缩包
  'zip', 'rar', '7z', 'tar', 'gz',
  // 视频
  'mp4', 'mov', 'avi', 'mkv', 'wmv', 'flv', 'webm', '3gp',
  // 音频
  'mp3', 'wav', 'aac', 'flac', 'm4a', 'ogg', 'wma',
])

export function isAllowedFile(name) {
  const ext = String(name || '').toLowerCase().split('.').pop()
  return !!ext && FILE_EXT_WHITELIST.has(ext)
}

// 文件选择器的 accept 筛选（与白名单一致）
export const FILE_ACCEPT = [...FILE_EXT_WHITELIST].map((e) => '.' + e).join(',')

// ===== 图片白名单：与后端 file 服务 imageExtWhitelist 对齐；
// 不用 image/*（它包含 svg，svg 在后端属危险类型黑名单会被拒绝） =====
export const IMAGE_EXT_WHITELIST = new Set([
  'jpg', 'jpeg', 'png', 'gif', 'bmp', 'webp', 'heic', 'heif', 'tiff', 'ico',
])

export function isAllowedImage(name) {
  const ext = String(name || '').toLowerCase().split('.').pop()
  return !!ext && IMAGE_EXT_WHITELIST.has(ext)
}

export const IMAGE_ACCEPT = [...IMAGE_EXT_WHITELIST].map((e) => '.' + e).join(',')

// 音频后缀识别：录音产物以 FILE(type=3) 发送，按扩展名识别为语音
export const AUDIO_EXT_RE = /\.(webm|m4a|aac|mp3|wav|ogg|flac)$/i

// 视频后缀识别：Chromium 可原生解码的格式（mp4/webm/mov）渲染为视频气泡
export const VIDEO_EXT_RE = /\.(mp4|webm|mov|m4v)$/i

// 单文件上传大小上限（与后端 presign 分类校验一致）：
// 普通文件 200MB / 图片 20MB / 语音 10MB；语音时长另限 60 秒
export const MAX_UPLOAD_SIZE = 200 * 1024 * 1024
export const IMAGE_MAX_SIZE = 20 * 1024 * 1024
export const VOICE_MAX_SIZE = 10 * 1024 * 1024
export const VOICE_MAX_SECONDS = 60

// 文件选择（返回 File 或 null）
export function pickFile(accept) {
  return new Promise((resolve) => {
    const input = document.createElement('input')
    input.type = 'file'
    if (accept) input.accept = accept
    input.onchange = () => {
      const f = input.files && input.files[0]
      resolve(f || null)
    }
    input.click()
  })
}
