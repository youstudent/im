/**
 * 本地存储：SQLite 连接与多账户会话管理。
 *
 * 设计依据 docs/桌面端本地存储实施计划.md 3.1：
 * - 按账户物理隔离：每个登录账户一个独立数据库文件 accounts/{uid}/workchat.db
 * - 主进程单活连接：同一时刻只持有一个账户的 DB 句柄
 * - 渲染进程不传 owner_uid，所有读写作用于当前会话账户的库
 *
 * SQLCipher 全库加密（docs/桌面端本地存储方案.md 第 7 节进阶方案）：
 * - 驱动：better-sqlite3-multiple-ciphers（better-sqlite3 的加密 fork，API 兼容）
 * - 每账户随机密钥由 keyring.js 以 safeStorage 加密保管
 * - 存量明文库启动时探测并自动就地迁移为加密库（原文件保留 .plain.bak 兜底）
 */
const path = require('node:path')
const fs = require('node:fs')
const { app } = require('electron')
const Database = require('better-sqlite3-multiple-ciphers')
const keyring = require('./keyring')

// 统一加密方案：SQLite3MultipleCiphers 支持多种 cipher，显式选定 sqlcipher 方案，
// 每次连接必须先设 cipher 再设 key（方案不持久化在文件头）。
const CIPHER_SCHEME = "cipher = 'sqlcipher'"

function keyPragma(keyHex) {
  return `key = "x'${keyHex}'"`
}

// ---- 存储根目录配置：存于 {userData}/storage-config.json ----
// 不能存 SQLite：数据库自身位置依赖该配置，需在打开任何库之前可读。
const CONFIG_NAME = 'storage-config.json'
let cachedRoot = null

function configPath() {
  return path.join(app.getPath('userData'), CONFIG_NAME)
}

function defaultRoot() {
  return path.join(app.getPath('userData'), 'storage')
}

function loadConfig() {
  try {
    const cfg = JSON.parse(fs.readFileSync(configPath(), 'utf8'))
    if (cfg && typeof cfg === 'object') return cfg
  } catch {}
  return {}
}

function loadConfigRoot() {
  const cfg = loadConfig()
  if (typeof cfg.storageRoot === 'string' && cfg.storageRoot.trim()) return cfg.storageRoot.trim()
  return null
}

function saveConfig(cfg) {
  try {
    fs.writeFileSync(configPath(), JSON.stringify(cfg, null, 2), 'utf8')
  } catch (e) {
    console.warn('[store] 写入存储配置失败:', e?.message || e)
  }
}

function saveConfigRoot(root) {
  const cfg = loadConfig()
  cfg.storageRoot = root
  saveConfig(cfg)
}

// 存储根目录：优先用配置值，缺省 {userData}/storage
function storageRoot() {
  if (!cachedRoot) cachedRoot = loadConfigRoot() || defaultRoot()
  return cachedRoot
}

// 账户数据库文件路径
function accountDbPath(uid) {
  return path.join(storageRoot(), 'accounts', String(uid), 'workchat.db')
}

const CURRENT_SCHEMA_VERSION = 6

let db = null // 当前会话的 DB 句柄
let sessionUid = null // 当前会话账户 uid

// 打开某账户的库（幂等：重复打开同一 uid 不重建连接）
function openSession(uid) {
  uid = String(uid || '').trim()
  if (!uid) throw new Error('openSession: uid 不能为空')
  if (db && sessionUid === uid) return sessionUid
  closeSession()

  const dbPath = accountDbPath(uid)
  fs.mkdirSync(path.dirname(dbPath), { recursive: true })
  const keyHex = keyring.getKey(uid)

  // 先恢复上次会话中断的加密迁移（残留 .enc / .plain.bak）
  recoverInterruptedMigration(dbPath, keyHex)

  // 探测库状态：new / plain / encrypted / error
  const state = probeDb(dbPath, keyHex)
  if (state === 'error') {
    throw new Error('数据库打开失败（密钥不符或文件损坏）')
  }
  if (state === 'plain') {
    migratePlainToEncrypted(dbPath, keyHex)
  }

  const next = new Database(dbPath)
  next.pragma(CIPHER_SCHEME)
  next.pragma(keyPragma(keyHex))
  next.pragma('journal_mode = WAL')
  migrate(next)
  db = next
  sessionUid = uid
  console.log('[store] 会话已打开(SQLCipher 加密):', uid, dbPath)
  // 启动后台定时任务（清理/保留期/备份；惰性 require 避免循环依赖）
  try {
    require('./scheduler').start()
  } catch (e) {
    console.warn('[store] 定时任务启动失败:', e?.message || e)
  }
  return sessionUid
}

// ---- SQLCipher 加密：探测 / 中断恢复 / 明文迁移 ----

// 探测库文件状态（加密库与明文库文件头均为 SQLite format 3，无法仅凭头区分，需尝试打开）
function probeDb(dbPath, keyHex) {
  let size = 0
  try {
    size = fs.statSync(dbPath).size
  } catch {
    return 'new'
  }
  if (size === 0) return 'new'

  // 先无钥尝试：可读 = 明文库
  try {
    const p = new Database(dbPath, { readonly: true })
    try {
      p.prepare('SELECT count(*) AS c FROM sqlite_master').get()
      return 'plain'
    } finally {
      p.close()
    }
  } catch {}

  // 再带钥尝试：可读 = 已加密且密钥正确
  try {
    const c = new Database(dbPath, { readonly: true })
    try {
      c.pragma(CIPHER_SCHEME)
      c.pragma(keyPragma(keyHex))
      c.prepare('SELECT count(*) AS c FROM sqlite_master').get()
      return 'encrypted'
    } finally {
      c.close()
    }
  } catch {}

  return 'error'
}

// 恢复上次中断的迁移：主库缺失但存在 .enc（完成迁移）或 .plain.bak（回滚后重新迁移）
function recoverInterruptedMigration(dbPath, keyHex) {
  if (fs.existsSync(dbPath)) return
  const encPath = dbPath + '.enc'
  const bakPath = dbPath + '.plain.bak'
  if (fs.existsSync(encPath)) {
    // 验证 .enc 是密钥可读的完整加密库 → 完成迁移；否则丢弃（回退 .bak）
    let valid = false
    try {
      const c = new Database(encPath, { readonly: true })
      try {
        c.pragma(CIPHER_SCHEME)
        c.pragma(keyPragma(keyHex))
        c.prepare('SELECT count(*) AS c FROM sqlite_master').get()
        valid = true
      } finally {
        c.close()
      }
    } catch {}
    if (valid) {
      fs.renameSync(encPath, dbPath)
      console.log('[store] 已恢复中断的加密迁移: .enc → 主库')
    } else {
      try {
        fs.rmSync(encPath, { force: true })
      } catch {}
    }
  }
  if (!fs.existsSync(dbPath) && fs.existsSync(bakPath)) {
    // 加密产物不可用：回滚到明文库，后续 probe 会重新触发迁移
    fs.renameSync(bakPath, dbPath)
    console.log('[store] 已回滚中断的加密迁移至明文库，将重试迁移')
  }
  for (const suf of ['-wal', '-shm']) {
    try {
      fs.rmSync(dbPath + suf, { force: true })
    } catch {}
  }
}

// 明文库就地迁移为加密库：新建加密空库 → 复制 schema → 逐表流式拷数据 → 替换文件。
// 原明文库改名 .plain.bak 保留兜底；SQLite3MultipleCiphers 未注册 sqlcipher_export，故逐表复制。
function migratePlainToEncrypted(dbPath, keyHex) {
  const encPath = dbPath + '.enc'
  const bakPath = dbPath + '.plain.bak'
  try {
    fs.rmSync(encPath, { force: true })
  } catch {}

  // 先把 WAL 落回主库，避免复制时遗漏未 checkpoint 的数据
  const w = new Database(dbPath)
  try {
    w.pragma('wal_checkpoint(TRUNCATE)')
  } catch {}
  w.close()

  const src = new Database(dbPath, { readonly: true })
  const dst = new Database(encPath)
  dst.pragma(CIPHER_SCHEME)
  dst.pragma(keyPragma(keyHex))
  try {
    // 复制 schema：跳过 sqlite_ 前缀的内部表（如 sqlite_sequence，由 AUTOINCREMENT 自动维护）
    const schemas = src.prepare("SELECT sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%'").all()
    for (const s of schemas) dst.exec(s.sql)
    const tables = src.prepare("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").all()
    for (const t of tables) {
      const cols = src.prepare(`PRAGMA table_info("${t.name}")`).all()
      const colNames = cols.map((c) => `"${c.name}"`).join(', ')
      const placeholders = cols.map(() => '?').join(', ')
      const insert = dst.prepare(`INSERT INTO "${t.name}" (${colNames}) VALUES (${placeholders})`)
      const select = src.prepare(`SELECT ${colNames} FROM "${t.name}"`)
      // 流式逐行拷贝（iterate 不全量载入内存），单事务保证速度与原子性
      dst.transaction(() => {
        for (const row of select.iterate()) {
          insert.run(...cols.map((c) => row[c.name]))
        }
      })()
    }
  } finally {
    src.close()
    dst.close()
  }

  // 替换：原库改名兜底，加密库上位
  fs.renameSync(dbPath, bakPath)
  fs.renameSync(encPath, dbPath)
  for (const suf of ['-wal', '-shm']) {
    try {
      fs.rmSync(dbPath + suf, { force: true })
    } catch {}
  }
  console.log('[store] 明文库已迁移为 SQLCipher 加密库:', dbPath)
}

// 关闭当前会话（登出/被踢/应用退出）；DB 文件保留在磁盘
function closeSession() {
  // 停止后台定时任务（惰性 require 避免循环依赖）
  try {
    require('./scheduler').stop()
  } catch {}
  if (db) {
    // 关闭前把 WAL 落回主库：减少 -wal/-shm 残留，降低 Windows 上文件占用导致删不掉的概率
    try {
      db.pragma('wal_checkpoint(TRUNCATE)')
    } catch {}
    try {
      db.close()
    } catch (e) {
      console.warn('[store] 关闭数据库异常:', e?.message || e)
    }
    db = null
  }
  sessionUid = null
}

// 获取当前会话的 DB 句柄；未打开时返回 null（调用方据此返回空/拒绝）
function getDb() {
  return db
}

function getSessionUid() {
  return sessionUid
}

function getStorageRoot() {
  return storageRoot()
}

// ---- 建表迁移：schema_version 存于 kv，每个账户库独立迁移 ----
function migrate(next) {
  // kv 表需最先存在（承载版本号）
  next.exec('CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY, value TEXT)')
  const row = next.prepare('SELECT value FROM kv WHERE key = ?').get('schema_version')
  let version = row ? Number(row.value) || 0 : 0

  if (version < 1) {
    next.exec(`
      CREATE TABLE IF NOT EXISTS conversations (
        id              TEXT PRIMARY KEY,
        owner_uid       TEXT NOT NULL,
        type            INTEGER,
        target_id       TEXT,
        last_msg        TEXT,
        last_msg_time   INTEGER DEFAULT 0,
        unread          INTEGER DEFAULT 0,
        last_synced_seq INTEGER DEFAULT 0,
        peer_read_seq   INTEGER DEFAULT 0,
        muted           INTEGER DEFAULT 0,
        pinned          INTEGER DEFAULT 0
      );
      CREATE INDEX IF NOT EXISTS idx_conv_owner ON conversations(owner_uid);

      CREATE TABLE IF NOT EXISTS messages (
        local_id    INTEGER PRIMARY KEY AUTOINCREMENT,
        server_id   TEXT,
        conv_id     TEXT NOT NULL,
        seq         INTEGER DEFAULT 0,
        sender_uid  TEXT,
        sender_name TEXT,
        type        INTEGER,
        content     TEXT,
        extra       TEXT,
        status      INTEGER DEFAULT 0,
        created_at  INTEGER,
        sync_state  TEXT NOT NULL DEFAULT 'synced',
        UNIQUE(conv_id, server_id)
      );
      CREATE INDEX IF NOT EXISTS idx_msg_conv_seq ON messages(conv_id, seq);
    `)
    next
      .prepare('INSERT INTO kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value')
      .run('schema_version', '1')
    version = 1
  }

  if (version < 2) {
    // v2：敏感会话不落盘标记（仅存标记，不存内容；开启时由仓储层清除该会话消息）
    next.exec('ALTER TABLE conversations ADD COLUMN no_persist INTEGER NOT NULL DEFAULT 0')
    next
      .prepare('INSERT INTO kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value')
      .run('schema_version', '2')
    version = 2
  }

  if (version < 3) {
    // v3：语音已播放标记（未读红点持久化从 localStorage 迁入消息表，随消息同库同生命周期）
    next.exec('ALTER TABLE messages ADD COLUMN voice_played INTEGER NOT NULL DEFAULT 0')
    next
      .prepare('INSERT INTO kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value')
      .run('schema_version', '3')
    version = 3
  }

  if (version < 4) {
    // v4：会话草稿（切换会话不丢输入内容，列表红色 [草稿] 前缀；纯本地，不同步服务端）
    next.exec('ALTER TABLE conversations ADD COLUMN draft TEXT')
    next
      .prepare('INSERT INTO kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value')
      .run('schema_version', '4')
    version = 4
  }

  if (version < 5) {
    // v5：会话最后消息发送者（群聊列表拼 "发送者: 内容" 前缀；0/空表示系统/无）
    next.exec('ALTER TABLE conversations ADD COLUMN last_sender_uid TEXT')
    next.exec('ALTER TABLE conversations ADD COLUMN last_sender_name TEXT')
    next
      .prepare('INSERT INTO kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value')
      .run('schema_version', '5')
    version = 5
  }

  if (version < 6) {
    // v6：标记未读（右键手动挂起红点，纯本地状态；收到新消息/打开会话自动清除）
    next.exec('ALTER TABLE conversations ADD COLUMN marked_unread INTEGER NOT NULL DEFAULT 0')
    next
      .prepare('INSERT INTO kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value')
      .run('schema_version', String(CURRENT_SCHEMA_VERSION))
    version = CURRENT_SCHEMA_VERSION
  }

  // SQLCipher 加密标记（供运维排查；非 schema 版本，不递增 schema_version）
  next
    .prepare('INSERT INTO kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value')
    .run('db_encrypted', '1')
}

module.exports = {
  openSession,
  closeSession,
  getDb,
  getSessionUid,
  getStorageRoot: storageRoot,
  accountDbPath,
  setStorageRoot,
  cleanupPendingDeletes,
}

// ---- 存储路径迁移：关闭会话 → 移动整个 storage 目录 → 切换配置 → 重开会话 ----
// 同盘 rename（原子快速）；跨盘 cpSync + 删源（先完整复制并校验，再删源，失败保留原目录）。

// 异步等待（重试间隔不阻塞主进程事件循环，避免迁移期间卡死 UI）
function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms))
}

// 记录待清理残留目录（删源失败时改名留下，下次启动重试删除）
function addPendingDelete(p) {
  const cfg = loadConfig()
  const list = Array.isArray(cfg.pendingDeletes) ? cfg.pendingDeletes : []
  if (!list.includes(p)) list.push(p)
  cfg.pendingDeletes = list
  saveConfig(cfg)
}

// 启动时重试删除迁移残留目录（上次因文件占用删不掉的源目录）
function cleanupPendingDeletes() {
  const cfg = loadConfig()
  const list = Array.isArray(cfg.pendingDeletes) ? cfg.pendingDeletes : []
  if (!list.length) return
  const rest = []
  for (const p of list) {
    try {
      fs.rmSync(p, { recursive: true, force: true, maxRetries: 3, retryDelay: 200 })
      console.log('[store] 已清理迁移残留目录:', p)
    } catch (e) {
      rest.push(p)
      console.warn('[store] 迁移残留目录仍被占用，下次再试:', p, e?.message || e)
    }
  }
  cfg.pendingDeletes = rest
  saveConfig(cfg)
}

// 删除源目录：重试数次（Windows 句柄释放有延迟）；仍失败则改名留待下次启动清理，
// 不阻断迁移（此时数据已完整复制到目标并校验通过）。
async function removeSourceDir(src) {
  let lastErr = null
  for (let i = 0; i < 4; i++) {
    try {
      fs.rmSync(src, { recursive: true, force: true, maxRetries: 3, retryDelay: 200 })
      return
    } catch (e) {
      lastErr = e
      await sleep(300)
    }
  }
  // 文件仍被占用：整目录改名到残留名（目录改名不受子文件锁影响），延后删除
  const leftover = `${src}.old-${Date.now()}`
  try {
    fs.renameSync(src, leftover)
    addPendingDelete(leftover)
    console.warn('[store] 源目录暂被占用，已改名留待清理:', leftover)
  } catch {
    throw lastErr
  }
}

async function moveDir(src, dest) {
  // 同盘 rename：Windows 上子文件瞬时占用（索引/杀毒）可能导致 EBUSY/EPERM，重试数次
  let renameErr = null
  for (let i = 0; i < 3; i++) {
    try {
      fs.renameSync(src, dest)
      return
    } catch (e) {
      renameErr = e
      if (e.code === 'EXDEV') break // 跨盘：走复制 + 删源
      if (e.code !== 'EBUSY' && e.code !== 'EPERM' && e.code !== 'EACCES') throw e
      await sleep(300)
    }
  }
  // rename 持续失败（跨盘/文件占用）：降级为复制 + 删源（下方有复制校验与失败回滚）
  fs.cpSync(src, dest, { recursive: true })
  // 校验关键账户库已复制再删源目录
  const srcAccounts = path.join(src, 'accounts')
  if (fs.existsSync(srcAccounts)) {
    for (const uid of fs.readdirSync(srcAccounts)) {
      const f = path.join(dest, 'accounts', uid, 'workchat.db')
      if (!fs.existsSync(f)) throw new Error(`复制校验失败：缺少 ${uid}/workchat.db`)
    }
  }
  await removeSourceDir(src)
}

// 迁移存储根目录；返回新路径。失败时抛错并保持原目录可用。
async function setStorageRoot(newRoot) {
  const target = path.resolve(String(newRoot || '').trim())
  const current = storageRoot()
  if (!target) throw new Error('路径为空')
  const normTarget = path.normalize(target).toLowerCase()
  const normCurrent = path.normalize(current).toLowerCase()
  if (normTarget === normCurrent) return current
  if (normTarget.startsWith(normCurrent + path.sep)) throw new Error('不能选择当前存储目录的子目录')
  // 目标已含 WorkChat 数据时拒绝，避免两套数据混淆
  if (fs.existsSync(path.join(target, 'accounts'))) throw new Error('目标目录已包含 WorkChat 数据，请换一个目录')
  // 创建并探测可写
  fs.mkdirSync(target, { recursive: true })
  const probe = path.join(target, '.write-test')
  fs.writeFileSync(probe, 'ok')
  fs.rmSync(probe)

  const uid = getSessionUid()
  // 关闭前先把 WAL 全部落回主库：减少 -wal/-shm 文件，降低 Windows 上文件占用导致删不掉的概率
  const handle = getDb()
  if (handle) {
    try {
      handle.pragma('wal_checkpoint(TRUNCATE)')
    } catch {}
  }
  closeSession()
  // 等系统（索引/杀毒）释放文件句柄的短暂窗口
  await sleep(200)
  try {
    if (fs.existsSync(current)) await moveDir(current, target)
  } catch (e) {
    // 迁移失败：清理目标目录残留（源目录未删时数据仍在原处），恢复原会话
    try {
      if (fs.existsSync(current)) fs.rmSync(target, { recursive: true, force: true })
    } catch {}
    if (uid) {
      try {
        openSession(uid)
      } catch {}
    }
    throw new Error('迁移失败：' + (e?.message || e))
  }
  cachedRoot = target
  saveConfigRoot(target)
  if (uid) openSession(uid)
  console.log('[store] 存储路径已迁移:', current, '→', target)
  return target
}
