/**
 * 文件上传接口：基于后端 OSS 预签名，前端直传阿里云 OSS。
 */
import { http } from './http'

/**
 * 获取预签名上传 URL。
 * @param {string} fileName 原始文件名（含扩展名）
 * @param {'image'|'file'|'voice'} type 资源类型
 * @param {number} size 文件字节数
 * @param {string} contentType 文件 MIME 类型（OSS 签名绑定该 Content-Type）
 * @param {number} duration 语音时长（秒，仅 voice 上报，后端校验 ≤60s）
 * @returns {Promise<{object_key:string,upload_url:string,download_url:string,expire_in:number}>}
 */
function presign(fileName, type, size, contentType, duration = 0) {
  const body = { file_name: fileName, type, size, content_type: contentType }
  if (type === 'voice') body.duration = Number(duration) || 0
  return http.post('/files/presign', body)
}

/**
 * 上传文件到 OSS（PUT 预签名 URL），返回 download_url。
 * 注意：OSS 预签名签名时绑定了 Content-Type，上传请求必须携带相同的 Content-Type 头，否则签名校验失败。
 * @param {File} file
 * @param {'image'|'file'|'voice'} type
 * @param {{duration?: number}} meta 语音附加时长（秒）
 */
export async function uploadFile(file, type = 'file', meta = {}) {
  const contentType = file.type || 'application/octet-stream'
  const presignRes = await presign(file.name, type, file.size, contentType, meta.duration || 0)
  if (!presignRes || !presignRes.upload_url) {
    throw new Error('获取上传链接失败')
  }
  const res = await fetch(presignRes.upload_url, {
    method: 'PUT',
    body: file,
    headers: { 'Content-Type': contentType },
  })
  if (!res.ok) {
    throw new Error('上传失败，状态码 ' + res.status)
  }
  // 预签名 URL 通常是 ?Expires=xxx&Signature=xxx 形式，返回下载地址
  return {
    objectKey: presignRes.object_key,
    downloadUrl: presignRes.download_url,
  }
}

export const fileApi = { presign, uploadFile }
