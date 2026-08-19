/**
 * SQLCipher 全库加密端到端测试（在 Electron 主进程运行，隔离 userData 到临时目录）。
 *
 * 运行：node_modules\electron\dist\electron.exe scripts\test_sqlcipher.mjs
 *
 * 覆盖清单：
 *   1. 新账户首登：库创建即加密，文件无明文表名/内容，keyring 已生成
 *   2. 存量明文库：openSession 自动就地迁移，数据一致，.plain.bak 保留
 *   3. 密钥异常：篡改 keyring 后 openSession 抛错，原库文件不被破坏
 *   4. 备份链路：createBackup 产物加密且密钥可读
 *   5. 登出/重登：closeSession/openSession 循环正常
 *   6. clearAccountData：账户目录与 keyring 条目一并清除，可重建空库
 */
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import fs from 'node:fs'
import os from 'node:os'
import { app } from 'electron'

const require = createRequire(import.meta.url)
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

// 隔离环境：userData 指向临时目录（必须在 app ready 前设置）
const sandbox = fs.mkdtempSync(path.join(os.tmpdir(), 'sqlcipher-e2e-'))
app.setPath('userData', path.join(sandbox, 'userData'))

let passed = 0
let failed = 0
function check(name, ok, extra) {
  if (ok) {
    passed++
    console.log(`[PASS] ${name}${extra ? ' | ' + extra : ''}`)
  } else {
    failed++
    console.log(`[FAIL] ${name}${extra ? ' | ' + extra : ''}`)
  }
}

async function run() {
  const store = require(path.join(root, 'electron', 'store', 'db.js'))
  const storage = require(path.join(root, 'electron', 'store', 'storage.js'))
  const keyring = require(path.join(root, 'electron', 'store', 'keyring.js'))
  const Database = require('better-sqlite3-multiple-ciphers')

  const keyringFile = path.join(app.getPath('userData'), 'db-keyring.json')
  const CIPHER = "cipher = 'sqlcipher'"

  // ---- 1. 新账户：建库即加密 ----
  const uidNew = '1001'
  store.openSession(uidNew)
  const dbPathNew = store.accountDbPath(uidNew)
  store.getDb().exec("CREATE TABLE IF NOT EXISTS probe_mark (v TEXT)")
  store.getDb().prepare('INSERT INTO probe_mark VALUES (?)').run('新库加密标记-机密')
  const keyNew = keyring.getKey(uidNew)
  check('1.1 keyring 生成且密钥可重复获取', typeof keyNew === 'string' && keyNew.length === 64 && keyring.getKey(uidNew) === keyNew)
  check('1.2 keyring 文件存在', fs.existsSync(keyringFile))
  const ring1 = JSON.parse(fs.readFileSync(keyringFile, 'utf8'))
  check('1.3 密钥以 safeStorage 密文存放', ring1[uidNew]?.mode === 'safe' && ring1[uidNew]?.key !== keyNew)
  store.closeSession()
  const rawNew = fs.readFileSync(dbPathNew).toString('latin1')
  check('1.4 新库文件无明文表名/内容', !rawNew.includes('probe_mark') && !rawNew.includes('新库加密标记-机密'))
  // 无钥打开应失败（异常路径也必须 close，避免句柄残留占住文件）
  let blockedNew = false
  let d15 = null
  try {
    d15 = new Database(dbPathNew)
    d15.prepare('SELECT count(*) FROM probe_mark').get()
  } catch (e) {
    blockedNew = /not a database/i.test(String(e.message))
  } finally {
    try {
      d15?.close()
    } catch {}
  }
  check('1.5 无钥打开新库被拒绝', blockedNew)

  // ---- 2. 存量明文库：自动迁移 ----
  // 构造与旧版应用一致的 v1 明文 schema（无 voice_played/no_persist，验证迁移后 v2/v3 ALTER 正常补齐）
  const uidPlain = '1002'
  const dbPathPlain = store.accountDbPath(uidPlain)
  fs.mkdirSync(path.dirname(dbPathPlain), { recursive: true })
  const plain = new Database(dbPathPlain) // 不设 key → 明文
  plain.exec('CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT)')
  plain.exec(`
    CREATE TABLE conversations (
      id TEXT PRIMARY KEY, owner_uid TEXT NOT NULL, type INTEGER, target_id TEXT,
      last_msg TEXT, last_msg_time INTEGER DEFAULT 0, unread INTEGER DEFAULT 0,
      last_synced_seq INTEGER DEFAULT 0, peer_read_seq INTEGER DEFAULT 0,
      muted INTEGER DEFAULT 0, pinned INTEGER DEFAULT 0
    );
    CREATE TABLE messages (
      local_id INTEGER PRIMARY KEY AUTOINCREMENT, server_id TEXT, conv_id TEXT NOT NULL,
      seq INTEGER DEFAULT 0, sender_uid TEXT, sender_name TEXT, type INTEGER,
      content TEXT, extra TEXT, status INTEGER DEFAULT 0, created_at INTEGER,
      sync_state TEXT NOT NULL DEFAULT 'synced', UNIQUE(conv_id, server_id)
    );
  `)
  plain.prepare('INSERT INTO kv(key, value) VALUES (?, ?)').run('schema_version', '1')
  plain.prepare('INSERT INTO conversations (id, owner_uid, last_msg) VALUES (?, ?, ?)').run('c1', uidPlain, '迁移前最后一条')
  plain.prepare('INSERT INTO messages (conv_id, content) VALUES (?, ?)').run('c1', '迁移前消息-A')
  plain.prepare('INSERT INTO messages (conv_id, content) VALUES (?, ?)').run('c1', '迁移前消息-B')
  plain.close()

  store.openSession(uidPlain) // 应触发自动迁移 + v2/v3 schema 补齐
  const migDb = store.getDb()
  const msgs = migDb.prepare('SELECT content FROM messages ORDER BY local_id').all()
  check('2.1 迁移后数据完整', msgs.length === 2 && msgs[0].content === '迁移前消息-A' && msgs[1].content === '迁移前消息-B')
  check('2.2 v3 列已补齐且默认值正确', migDb.prepare('SELECT voice_played FROM messages LIMIT 1').get()?.voice_played === 0)
  check('2.3 v2 列已补齐（conversations.no_persist）', migDb.prepare('SELECT no_persist FROM conversations LIMIT 1').get()?.no_persist === 0)
  check('2.4 kv 写入 db_encrypted 标记', migDb.prepare("SELECT value FROM kv WHERE key='db_encrypted'").get()?.value === '1')
  store.closeSession()
  check('2.5 原明文库保留为 .plain.bak', fs.existsSync(dbPathPlain + '.plain.bak'))
  const rawMig = fs.readFileSync(dbPathPlain).toString('latin1')
  check('2.6 迁移后主库无明文', !rawMig.includes('迁移前消息-A'))

  // ---- 3. 密钥异常：篡改 keyring → 打开失败且文件完好 ----
  const ringRaw = fs.readFileSync(keyringFile, 'utf8')
  const ring = JSON.parse(ringRaw)
  const savedEntry = ring[uidNew]
  ring[uidNew] = { mode: 'plain', key: 'ff'.repeat(32), created: Date.now() } // 错误密钥
  fs.writeFileSync(keyringFile, JSON.stringify(ring, null, 2), 'utf8')
  let openErr = null
  try {
    store.openSession(uidNew)
  } catch (e) {
    openErr = e
  }
  check('3.1 错误密钥打开被拒绝', !!openErr && /密钥不符|文件损坏/.test(String(openErr.message)))
  check('3.2 原库文件未被删除/覆盖', fs.existsSync(dbPathNew))
  // 还原正确密钥
  ring[uidNew] = savedEntry
  fs.writeFileSync(keyringFile, JSON.stringify(ring, null, 2), 'utf8')
  store.openSession(uidNew)
  const probeRow = store.getDb().prepare('SELECT v FROM probe_mark LIMIT 1').get()
  check('3.3 还原密钥后可正常打开', probeRow?.v === '新库加密标记-机密')

  // ---- 4. 备份链路：产物加密且可读 ----
  const bak = await storage.createBackup()
  check('4.1 备份文件已生成', !!bak?.path && fs.existsSync(bak.path))
  const rawBak = fs.readFileSync(bak.path).toString('latin1')
  check('4.2 备份文件无明文', !rawBak.includes('新库加密标记-机密'))
  const bd = new Database(bak.path)
  bd.pragma(CIPHER)
  bd.pragma(`key = "x'${keyNew}'"`)
  const bakRow = bd.prepare('SELECT v FROM probe_mark LIMIT 1').get()
  check('4.3 备份可用同密钥打开并读取', bakRow?.v === '新库加密标记-机密')
  bd.close()

  // ---- 5. 登出/重登循环 ----
  store.closeSession()
  store.openSession(uidNew)
  store.closeSession()
  store.openSession(uidNew)
  check('5.1 close/open 循环后数据仍在', store.getDb().prepare('SELECT count(*) AS c FROM probe_mark').get().c === 1)

  // ---- 5.5 存储路径迁移：加密库随目录迁移后仍可打开（密钥在 userData 不受影响）----
  const newRoot = path.join(sandbox, 'newroot')
  await store.setStorageRoot(newRoot)
  check('5.2 存储根已切换', store.getStorageRoot() === newRoot)
  check('5.3 迁移后加密库可打开且数据一致', store.getDb().prepare('SELECT count(*) AS c FROM probe_mark').get().c === 1)

  // ---- 6. clearAccountData：数据与密钥一并清除并可重建 ----
  const cleared = await storage.clearAccountData()
  // 重建空库时 openSession 会生成新密钥写回 keyring：验证旧密钥已被替换（而非沿用）
  const ringAfter = JSON.parse(fs.readFileSync(keyringFile, 'utf8'))
  check('6.1 clearAccountData 返回 uid', cleared?.uid === uidNew)
  check('6.2 旧密钥已清除（keyring 条目已换新）', !!ringAfter[uidNew] && keyring.getKey(uidNew) !== keyNew)
  check('6.3 重建空库可用', !!store.getDb() && store.getDb().prepare("SELECT value FROM kv WHERE key='schema_version'").get()?.value === '3')
  check('6.4 重建库获得新密钥', typeof keyring.getKey(uidNew) === 'string' && keyring.getKey(uidNew) !== keyNew)
  store.closeSession()

  console.log(`\n===== 结果：${passed} 通过 / ${failed} 失败 =====`)
}

app.whenReady().then(async () => {
  try {
    await run()
  } catch (e) {
    failed++
    console.log('[FATAL]', e?.stack || e)
  } finally {
    try {
      fs.rmSync(sandbox, { recursive: true, force: true })
    } catch {}
    app.exit(failed ? 1 : 0)
  }
})
