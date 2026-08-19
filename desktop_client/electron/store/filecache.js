/**
 * 本地存储：文件缓存（图片/文件）。
 * 依据 docs/桌面端本地存储方案.md 第 4 节：
 *   {storageRoot}/files/{sha256[:2]}/{sha256}.{ext}
 *   内容哈希命名天然去重，命中直接读，不再重复下载；预签名 URL 过期也不受影响。
 *
 * 渲染进程通过 wcfile:// 自定义协议读取缓存文件（main.js 注册 protocol.handle），
 * 不暴露本地文件路径，符合 contextIsolation 安全规范。
 */
const fs = require('node:fs')
const path = require('node:path')
const crypto = require('node:crypto')
const { pathToFileURL } = require('node:url')
const { getStorageRoot, getSessionUid } = require('./db')

const SCHEME = 'wcfile'

function filesDir() {
  return path.join(getStorageRoot(), 'files')
}

// 缓存键：优先 OSS object_key（稳定、不随签名 URL 过期变化），否则按 URL 哈希
function cacheKey(key, url) {
  const raw = key && String(key).trim() ? String(key).trim() : String(url || '')
  return crypto.createHash('sha256').update(raw).digest('hex')
}

// 扩展名：优先从文件名取，其次 URL 路径；仅允许白名单字符
function extOf(name, url) {
  const src = name || url || ''
  const base = src.split(/[\\/]/).pop().split('?')[0]
  const m = /\.([a-zA-Z0-9]{1,8})$/.exec(base)
  return m ? m[1].toLowerCase() : 'bin'
}

// hash + ext → 磁盘路径（分桶两级目录）
function filePathFor(hash, ext) {
  return path.join(filesDir(), hash.slice(0, 2), `${hash}.${ext}`)
}

// wcfile://{hash}.{ext} → 磁盘路径（协议服务用；严格校验，防路径穿越）
function resolveProtocolPath(hashExt) {
  const m = /^([a-f0-9]{64})\.([a-zA-Z0-9]{1,8})$/.exec(hashExt || '')
  if (!m) return null
  return filePathFor(m[1], m[2])
}

// 同键下载去重（并发请求同一文件只下载一次）
const inflight = new Map()

/**
 * 解析媒体资源为缓存地址：
 * - 命中缓存：直接返回 wcfile 地址（hit=true）
 * - 未命中：后台下载到缓存；下载完成后渲染进程可用 cacheUrl 替换 src
 * 返回 { hit, cacheUrl, localPath }；URL 非法或未登录时返回 { hit:false }
 */
async function resolveMedia({ url, key, name }) {
  if (!url || !/^https?:\/\//.test(url)) return { hit: false }
  const hash = cacheKey(key, url)
  const ext = extOf(name, url)
  const filePath = filePathFor(hash, ext)
  const cacheUrl = `${SCHEME}://${hash}.${ext}`

  // 已缓存：直接命中
  try {
    if (fs.statSync(filePath).size > 0) return { hit: true, cacheUrl, localPath: filePath }
  } catch {}

  // 未登录（无会话）时不下载，避免无主文件堆积
  if (!getSessionUid()) return { hit: false }

  // 下载（同键去重）
  let p = inflight.get(hash)
  if (!p) {
    p = (async () => {
      const res = await fetch(url) // 预签名 URL 自带鉴权
      if (!res.ok) throw new Error(`下载失败: HTTP ${res.status}`)
      const buf = Buffer.from(await res.arrayBuffer())
      if (!buf.length) throw new Error('下载内容为空')
      fs.mkdirSync(path.dirname(filePath), { recursive: true })
      const tmp = filePath + '.part'
      fs.writeFileSync(tmp, buf)
      fs.renameSync(tmp, filePath) // 原子替换，避免半截文件
    })()
    inflight.set(hash, p)
    p.finally(() => inflight.delete(hash))
  }
  try {
    await p
    return { hit: true, cacheUrl, localPath: filePath }
  } catch (e) {
    console.warn('[filecache] 下载失败:', e?.message || e)
    return { hit: false }
  }
}

// 打开/另存缓存文件：返回 { localPath }；未命中时触发下载后再返回
async function openLocal({ url, key, name }) {
  const r = await resolveMedia({ url, key, name })
  if (!r.hit) return { ok: false }
  return { ok: true, localPath: r.localPath, name: name || '' }
}

// 占用统计 / 清理 辅助
function filesSize() {
  let total = 0
  const stack = [filesDir()]
  while (stack.length) {
    const dir = stack.pop()
    let entries
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true })
    } catch {
      continue
    }
    for (const e of entries) {
      const p = path.join(dir, e.name)
      if (e.isDirectory()) stack.push(p)
      else if (e.isFile()) {
        try {
          total += fs.statSync(p).size
        } catch {}
      }
    }
  }
  return total
}

// 供 main.js 注册协议处理：wcfile://{hash}.{ext} → 本地文件
function createProtocolHandler() {
  return async (request) => {
    try {
      const u = new URL(request.url)
      const filePath = resolveProtocolPath(u.host + (u.pathname || '').replace(/^\//, ''))
      if (!filePath) return new Response('bad request', { status: 400 })
      const buf = fs.readFileSync(filePath)
      return new Response(buf)
    } catch {
      return new Response('not found', { status: 404 })
    }
  }
}

module.exports = { SCHEME, resolveMedia, openLocal, filesSize, createProtocolHandler, pathToFileURL }
