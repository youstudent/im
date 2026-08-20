/**
 * 开发者工具：解密本机 SQLCipher 加密库，导出为明文 .db 供 DB Browser 等工具查看。
 *
 * 运行（在 desktop_client 目录，应用须先关闭，避免文件占用）：
 *   node_modules\electron\dist\electron.exe scripts\decrypt_db.mjs <uid> [输出路径]
 *   不传 uid 时列出本机已有密钥的所有账户
 *
 * 原理：
 *   1. 读取 {userData}/db-keyring.json，用 Electron safeStorage（Windows DPAPI）解出密钥 hex
 *   2. 以 cipher='sqlcipher' + key 打开 accounts/{uid}/workchat.db（只读）
 *   3. 新建明文库，复制 schema 后逐表流式拷贝数据（与 db.js 迁移同方案）
 *   默认输出到库文件旁：workchat.plain.db（不影响原加密库）
 */
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import fs from 'node:fs'
import { app, safeStorage } from 'electron'

const require = createRequire(import.meta.url)
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const Database = require(path.join(root, 'node_modules', 'better-sqlite3-multiple-ciphers'))

// CLI 直启时 Electron 默认名为 'Electron'，userData 会指错目录；显式对齐 package.json 的 name
app.setName('desktop-client')

const uidArg = process.argv[2] || ''
const outArg = process.argv[3] || ''

// 存储根：与 db.js 一致，优先 storage-config.json，缺省 {userData}/storage
function storageRoot() {
  try {
    const cfg = JSON.parse(fs.readFileSync(path.join(app.getPath('userData'), 'storage-config.json'), 'utf8'))
    if (typeof cfg.storageRoot === 'string' && cfg.storageRoot.trim()) return cfg.storageRoot.trim()
  } catch {}
  return path.join(app.getPath('userData'), 'storage')
}

// 从 keyring 解出指定账户密钥（不生成新密钥：仅读取，避免误建残留条目）
function readKey(uid) {
  const file = path.join(app.getPath('userData'), 'db-keyring.json')
  let ring = {}
  try {
    ring = JSON.parse(fs.readFileSync(file, 'utf8'))
  } catch {
    return { err: 'keyring 不存在或不可读: ' + file }
  }
  const entry = ring[uid]
  if (!entry || typeof entry.key !== 'string') return { err: `keyring 中无账户 ${uid}`, uids: Object.keys(ring) }
  if (entry.mode === 'plain') return { key: entry.key }
  if (!safeStorage.isEncryptionAvailable()) return { err: 'safeStorage 不可用，无法解密 safe 模式密钥' }
  try {
    return { key: safeStorage.decryptString(Buffer.from(entry.key, 'base64')) }
  } catch (e) {
    return { err: 'safeStorage 解密失败（须以生成密钥的同一 Windows 用户运行）: ' + e.message }
  }
}

function listAccounts() {
  const file = path.join(app.getPath('userData'), 'db-keyring.json')
  let ring = {}
  try {
    ring = JSON.parse(fs.readFileSync(file, 'utf8'))
  } catch {
    console.log('未找到 keyring:', file)
    return
  }
  const uids = Object.keys(ring)
  if (!uids.length) {
    console.log('keyring 为空，本机尚无加密账户')
    return
  }
  console.log('本机已有密钥的账户：')
  for (const uid of uids) {
    const dbPath = path.join(storageRoot(), 'accounts', uid, 'workchat.db')
    console.log(`  uid=${uid}  mode=${ring[uid].mode}  库文件=${fs.existsSync(dbPath) ? dbPath : '(缺失)'}`)
  }
}

async function main() {
  await app.whenReady()
  if (!uidArg) {
    listAccounts()
    return
  }

  const r = readKey(uidArg)
  if (r.err) {
    console.error('[失败]', r.err)
    if (r.uids?.length) console.error('已有账户:', r.uids.join(', '))
    process.exitCode = 1
    return
  }
  console.log(`[ok] 账户 ${uidArg} 密钥已解出（hex ${r.key.length / 2} 字节）:`, r.key)

  const dbPath = path.join(storageRoot(), 'accounts', uidArg, 'workchat.db')
  if (!fs.existsSync(dbPath)) {
    console.error('[失败] 库文件不存在:', dbPath)
    process.exitCode = 1
    return
  }
  const outPath = outArg || path.join(path.dirname(dbPath), 'workchat.plain.db')

  // 只读打开加密库：先设 cipher 再设 key（与 db.js 连接顺序一致）
  const src = new Database(dbPath, { readonly: true })
  try {
    src.pragma("cipher = 'sqlcipher'")
    src.pragma(`key = "x'${r.key}'"`)
    src.prepare('SELECT count(*) FROM sqlite_master').get() // 触发实际读取，密钥错误在此抛出
  } catch (e) {
    src.close()
    console.error('[失败] 加密库打开失败（密钥不符或文件损坏）:', e.message)
    process.exitCode = 1
    return
  }

  try {
    fs.rmSync(outPath, { force: true })
  } catch {}
  const dst = new Database(outPath) // 不设 key → 明文输出
  try {
    const schemas = src.prepare("SELECT sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%'").all()
    for (const s of schemas) dst.exec(s.sql)
    const tables = src.prepare("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").all()
    for (const t of tables) {
      const cols = src.prepare(`PRAGMA table_info("${t.name}")`).all()
      const colNames = cols.map((c) => `"${c.name}"`).join(', ')
      const placeholders = cols.map(() => '?').join(', ')
      const insert = dst.prepare(`INSERT INTO "${t.name}" (${colNames}) VALUES (${placeholders})`)
      const select = src.prepare(`SELECT ${colNames} FROM "${t.name}"`)
      let n = 0
      dst.transaction(() => {
        for (const row of select.iterate()) {
          insert.run(...cols.map((c) => row[c.name]))
          n++
        }
      })()
      console.log(`  表 ${t.name}: ${n} 行`)
    }
  } finally {
    src.close()
    dst.close()
  }
  console.log('[ok] 明文库已导出:', outPath)
  console.log('提示：明文库仅供本地排查，切勿上传/同步；排查完成后建议删除。')
}

main()
  .catch((e) => {
    console.error('[异常]', e)
    process.exitCode = 1
  })
  .finally(() => app.quit())
