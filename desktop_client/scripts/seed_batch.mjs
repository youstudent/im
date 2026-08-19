/**
 * 批量造数脚本：
 * 1) 注册 20 个测试账户
 * 2) 每个账户向主账户（19961215413）发起好友申请，并由主账户全部接受
 * 3) 每个账户给主账户发送 20 条消息（文字 / 表情 / 图片 / 文件 各 5 条）
 *
 * 注意服务端风控：同 IP 每日注册上限 20；单账户每秒最多发 20 条消息。
 * 用法：node scripts/seed_batch.mjs
 */
const BASE = 'http://127.0.0.1:8080/api/v1'
const MAIN_ACCOUNT = '19961215413'
const MAIN_PASSWORD = 'w19961215413'
const N = 20 // 注册账户数
const MSGS_PER = 20 // 每账户发送消息数
const PASSWORD = 'Batch12345'

async function call(method, path, body, token) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + path, { method, headers: h, body: body ? JSON.stringify(body) : undefined })
  const j = await res.json().catch(() => ({}))
  if (j.code !== 0) throw new Error(`${method} ${path} → ${j.message || res.status}`)
  return j.data
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const genMsgId = () => String(Date.now()) + String(Math.floor(Math.random() * 1000)).padStart(3, '0')

// ---- 共享素材：1x1 PNG 图片 + 小文本文件（上传一次，所有账户复用） ----
const PNG_B64 = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='

async function uploadShared(token) {
  const items = [
    { name: 'workchat-test.png', type: 'image', bytes: Buffer.from(PNG_B64, 'base64'), contentType: 'image/png' },
    { name: 'workchat-test.txt', type: 'file', bytes: Buffer.from('WorkChat 批量测试文件\n生成时间: ' + new Date().toISOString(), 'utf8'), contentType: 'text/plain' },
  ]
  const out = {}
  for (const it of items) {
    const pre = await call('POST', '/files/presign', {
      file_name: it.name,
      type: it.type,
      size: it.bytes.length,
      content_type: it.contentType,
    }, token)
    if (!pre || !pre.upload_url) throw new Error('获取上传链接失败')
    const res = await fetch(pre.upload_url, {
      method: 'PUT',
      body: it.bytes,
      headers: { 'Content-Type': it.contentType },
    })
    if (!res.ok) throw new Error(`OSS 上传失败: ${res.status}`)
    out[it.type] = { url: pre.download_url, key: pre.object_key, name: it.name, size: it.bytes.length }
  }
  return out
}

// ---- 1) 登录主账户 ----
console.log('== 登录主账户', MAIN_ACCOUNT)
const main = await call('POST', '/auth/login', { account: MAIN_ACCOUNT, password: MAIN_PASSWORD })
const mainToken = main.access_token
const mainUid = Number(main.user.uid)
console.log('主账户 uid =', mainUid)

// ---- 2) 批量注册 ----
const sfx = String(Date.now()).slice(-6)
const accounts = []
console.log(`== 注册 ${N} 个账户`)
for (let i = 1; i <= N; i++) {
  const account = `wc_batch_${sfx}_${String(i).padStart(2, '0')}`
  const nickname = `批量成员${String(i).padStart(2, '0')}`
  try {
    const r = await call('POST', '/auth/register', { nickname, account, password: PASSWORD })
    accounts.push({ account, nickname, token: r.access_token, uid: Number(r.user.uid) })
    console.log(`  [${i}/${N}] ${account} → uid ${r.user.uid}`)
  } catch (e) {
    console.error(`  [${i}/${N}] ${account} 注册失败: ${e.message}`)
  }
  await sleep(100)
}
if (!accounts.length) {
  console.error('没有注册成功的账户，终止')
  process.exit(1)
}

// ---- 3) 好友申请 + 主账户接受 ----
console.log('== 发起好友申请')
for (const a of accounts) {
  try {
    await call('POST', '/friends/requests', { to_uid: mainUid, message: '你好，我是批量测试账户' }, a.token)
  } catch (e) {
    console.warn(`  ${a.account} 申请失败（可能已是好友）: ${e.message}`)
  }
  await sleep(50)
}
const uidSet = new Set(accounts.map((a) => a.uid))
const reqs = await call('GET', '/friends/requests', null, mainToken)
const pending = (reqs || []).filter((r) => uidSet.has(Number(r.from_uid)))
console.log(`== 主账户接受 ${pending.length} 条好友申请`)
for (const rq of pending) {
  await call('POST', `/friends/requests/${rq.id}/handle`, { accept: true }, mainToken)
  await sleep(50)
}

// ---- 4) 上传共享图片/文件 ----
console.log('== 上传图片/文件素材')
const media = await uploadShared(accounts[0].token)

// ---- 5) 每个账户发送 20 条消息（文字/表情/图片/文件 循环，各 5 条） ----
const EMOJIS = ['😀', '😁', '😂', '🤣', '😊', '😍', '👍', '🎉', '🔥', '❤️']
function buildMessage(a, i) {
  const kind = i % 4 // 1 文字 / 2 表情 / 3 图片 / 0 文件
  if (kind === 1) {
    return { type: 1, content: `你好，我是 ${a.nickname}，这是第 ${i} 条测试消息`, extra: '' }
  }
  if (kind === 2) {
    const e1 = EMOJIS[i % EMOJIS.length]
    const e2 = EMOJIS[(i + 3) % EMOJIS.length]
    return { type: 1, content: `${e1} 第 ${i} 条消息，祝你开心每一天 ${e2}`, extra: '' }
  }
  if (kind === 3) {
    const extra = JSON.stringify({ url: media.image.url, key: media.image.key, name: media.image.name, size: media.image.size })
    return { type: 2, content: media.image.url, extra }
  }
  const extra = JSON.stringify({ url: media.file.url, key: media.file.key, name: media.file.name, size: media.file.size })
  return { type: 3, content: media.file.url, extra }
}

console.log(`== 发送消息：${accounts.length} 个账户 × ${MSGS_PER} 条`)
let sent = 0
let failed = 0
for (const a of accounts) {
  for (let i = 1; i <= MSGS_PER; i++) {
    const m = buildMessage(a, i)
    try {
      await call('POST', '/conversations', {
        conv_id: '0', // 新会话：服务端按 target_id 自动创建
        target_id: mainUid,
        conv_type: 1,
        type: m.type,
        msg_id: genMsgId(),
        content: m.content,
        extra: m.extra,
      }, a.token)
      sent++
    } catch (e) {
      failed++
      console.warn(`  ${a.account} 第 ${i} 条发送失败: ${e.message}`)
    }
    await sleep(120) // 避开"每账户每秒 20 条"风控
  }
  console.log(`  ${a.account} 完成（累计已发 ${sent} 条）`)
}

console.log('== 汇总')
console.log(`注册成功: ${accounts.length}/${N}`)
console.log(`好友申请接受: ${pending.length}`)
console.log(`消息发送成功: ${sent}，失败: ${failed}`)
console.log('DONE')
process.exit(0)
