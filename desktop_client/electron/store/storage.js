/**
 * 本地存储：占用统计 / 清理 / 导出 / 备份 / 保留期。
 * 依据 docs/桌面端本地存储方案.md 第 8 节「承接前端数据存储设置项」。
 *
 * 目录约定：
 *   {storageRoot}/cache/    临时缓存（可安全清理）
 *   {storageRoot}/backups/  聊天记录备份
 */
const fs = require('node:fs')
const path = require('node:path')
const { getDb, getSessionUid, getStorageRoot, accountDbPath, closeSession, openSession } = require('./db')
const { filesSize } = require('./filecache')
const keyring = require('./keyring')

// ---- 目录/文件递归大小 ----
function dirSize(dir) {
  let total = 0
  let entries
  try {
    entries = fs.readdirSync(dir, { withFileTypes: true })
  } catch {
    return 0
  }
  for (const e of entries) {
    const p = path.join(dir, e.name)
    if (e.isDirectory()) total += dirSize(p)
    else if (e.isFile()) {
      try {
        total += fs.statSync(p).size
      } catch {}
    }
  }
  return total
}

function fileSize(p) {
  try {
    return fs.statSync(p).size
  } catch {
    return 0
  }
}

// ---- 占用统计：DB 主库（含 WAL/SHM）+ files 文件缓存 + cache 目录 ----
function getUsage() {
  const root = getStorageRoot()
  const uid = getSessionUid()
  let dbSize = 0
  if (uid) {
    const dbPath = accountDbPath(uid)
    dbSize = fileSize(dbPath) + fileSize(dbPath + '-wal') + fileSize(dbPath + '-shm')
  }
  const filesBytes = filesSize()
  const cacheSize = dirSize(path.join(root, 'cache'))
  return { dbSize, filesSize: filesBytes, cacheSize, totalSize: dbSize + filesBytes + cacheSize, root }
}

// ---- 立即清理：清空 cache 目录 + VACUUM 压缩主库，返回释放字节数 ----
function clearCache() {
  const root = getStorageRoot()
  const before = getUsage().totalSize
  const cacheDir = path.join(root, 'cache')
  try {
    fs.rmSync(cacheDir, { recursive: true, force: true })
  } catch {}
  try {
    fs.mkdirSync(cacheDir, { recursive: true })
  } catch {}
  const db = getDb()
  if (db) {
    try {
      db.exec('VACUUM')
    } catch (e) {
      console.warn('[store] VACUUM 失败:', e?.message || e)
    }
  }
  const after = getUsage().totalSize
  return { freed: Math.max(0, before - after) }
}

// ---- 保留期清理：删除超期已同步消息（seq>0），返回删除行数 ----
function purgeOldMessages(days) {
  const db = getDb()
  if (!db) return { deleted: 0 }
  const d = Number(days)
  if (!d || d <= 0) return { deleted: 0 } // 0/负数 = 永久保存
  const cutoff = Math.floor(Date.now() / 1000) - d * 86400
  const info = db.prepare('DELETE FROM messages WHERE seq > 0 AND created_at > 0 AND created_at < ?').run(cutoff)
  return { deleted: info.changes || 0 }
}

// ---- 导出聊天记录：TXT / HTML ----
function fmtTime(unixSec) {
  if (!unixSec) return ''
  const d = new Date(unixSec * 1000)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function escapeHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

// 汇总当前账户全部会话消息（会话标题行 + 消息行）
function collectExportRows() {
  const db = getDb()
  const uid = getSessionUid()
  if (!db || !uid) return []
  const convs = db.prepare('SELECT * FROM conversations WHERE owner_uid = ? ORDER BY last_msg_time DESC').all(uid)
  const rows = []
  for (const c of convs) {
    const msgs = db
      .prepare('SELECT * FROM messages WHERE conv_id = ? AND (seq > 0 OR server_id IS NOT NULL) ORDER BY COALESCE(NULLIF(seq,0), 9223372036854775807), created_at ASC')
      .all(String(c.id))
    rows.push({ conv: c, msgs })
  }
  return rows
}

function exportMessages(filePath, format) {
  const rows = collectExportRows()
  const uid = getSessionUid()
  const now = fmtTime(Math.floor(Date.now() / 1000))
  fs.mkdirSync(path.dirname(filePath), { recursive: true })

  if (format === 'html') {
    const parts = [
      '<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8">',
      `<title>WorkChat 聊天记录导出</title><style>`,
      `body{font-family:"Segoe UI",system-ui,sans-serif;background:#f5f6f7;margin:0;padding:24px}`,
      `.conv{background:#fff;border-radius:10px;padding:16px 20px;margin-bottom:16px;box-shadow:0 1px 3px rgba(0,0,0,.06)}`,
      `.conv h2{margin:0 0 12px;font-size:16px}`,
      `.msg{padding:6px 0;border-top:1px solid #f0f1f2;font-size:14px}`,
      `.meta{color:#8a919f;font-size:12px;margin-right:8px}`,
      `.me{color:#2563eb}`,
      `</style></head><body>`,
      `<h1>WorkChat 聊天记录导出</h1><p>账户 ${escapeHtml(uid || '')} · 导出时间 ${escapeHtml(now)}</p>`,
    ]
    for (const { conv, msgs } of rows) {
      parts.push(`<div class="conv"><h2>会话 ${escapeHtml(conv.id)}</h2>`)
      if (!msgs.length) parts.push('<div class="msg"><span class="meta">（无消息）</span></div>')
      for (const m of msgs) {
        const mine = m.sender_uid != null && String(m.sender_uid) === String(uid)
        const name = mine ? '我' : m.sender_name || `用户 ${m.sender_uid}`
        const label = m.status === 1 ? '[已撤回] ' : ''
        parts.push(
          `<div class="msg"><span class="meta">${escapeHtml(fmtTime(m.created_at))}</span>` +
            `<b class="${mine ? 'me' : ''}">${escapeHtml(name)}</b>：${label}${escapeHtml(m.content || '')}</div>`
        )
      }
      parts.push('</div>')
    }
    parts.push('</body></html>')
    fs.writeFileSync(filePath, parts.join('\n'), 'utf8')
  } else {
    // txt
    const lines = [`WorkChat 聊天记录导出`, `账户: ${uid}`, `导出时间: ${now}`, '']
    for (const { conv, msgs } of rows) {
      lines.push(`===== 会话 ${conv.id} =====`)
      if (!msgs.length) lines.push('（无消息）')
      for (const m of msgs) {
        const mine = m.sender_uid != null && String(m.sender_uid) === String(uid)
        const name = mine ? '我' : m.sender_name || `用户 ${m.sender_uid}`
        const label = m.status === 1 ? '[已撤回] ' : ''
        lines.push(`[${fmtTime(m.created_at)}] ${name}: ${label}${m.content || ''}`)
      }
      lines.push('')
    }
    fs.writeFileSync(filePath, lines.join('\n'), 'utf8')
  }
  return { path: filePath, conversations: rows.length }
}

// ---- 备份：VACUUM INTO 复制到 backups/ 或指定目录 ----
// SQLCipher 加密库下 VACUUM INTO 产物继承同密钥加密（已验证），
// 不能用 db.backup()：备份 API 会产出明文页拷贝，破坏加密。
// 恢复时用 keyring.getKey(uid) 打开备份文件即可。
async function createBackup(destDir) {
  const db = getDb()
  const uid = getSessionUid()
  if (!db || !uid) throw new Error('未打开账户会话，无法备份')
  const dir = destDir || path.join(getStorageRoot(), 'backups')
  fs.mkdirSync(dir, { recursive: true })
  const stamp = new Date()
    .toISOString()
    .replace(/[-:T]/g, '')
    .slice(0, 14)
  const dest = path.join(dir, `workchat-${uid}-${stamp}.db`)
  db.exec(`VACUUM INTO '${dest.replace(/'/g, "''")}'`)
  if (!fs.existsSync(dest)) throw new Error('备份文件未生成')
  return { path: dest }
}

module.exports = { getUsage, clearCache, purgeOldMessages, exportMessages, createBackup, clearAccountData }

// ---- 清除本账户数据：删除 accounts/{uid}/ 目录，随后重建空库保持应用可用 ----
// 服务端仍是事实源，本地库可随时删除重建（重新同步）；离线发送队列一并丢弃。
// files/ 文件缓存按内容哈希全局共享、无法归属账户，不在此清理。
async function clearAccountData() {
  const uid = getSessionUid()
  if (!uid) throw new Error('未打开账户会话，无法清除')
  closeSession()
  const dir = path.dirname(accountDbPath(uid))
  // Windows 上文件句柄（索引/杀毒）释放有延迟，重试数次；仍失败则恢复会话并报错
  let lastErr = null
  for (let i = 0; i < 4; i++) {
    try {
      fs.rmSync(dir, { recursive: true, force: true, maxRetries: 3, retryDelay: 200 })
      lastErr = null
      break
    } catch (e) {
      lastErr = e
      await new Promise((r) => setTimeout(r, 300))
    }
  }
  if (lastErr) {
    // 删除失败也要恢复会话，避免应用处于无库状态
    try {
      openSession(uid)
    } catch {}
    throw new Error('清除失败：' + (lastErr?.message || lastErr))
  }
  // 同步清除该账户的 SQLCipher 数据库密钥，避免残留（重建时 openSession 会生成新密钥）
  keyring.removeKey(uid)
  openSession(uid) // 重建空库，保持应用可用（后续从服务端重新同步）
  console.log('[store] 已清除账户本地数据:', uid)
  return { uid }
}
