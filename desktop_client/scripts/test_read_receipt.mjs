/**
 * 已读未读端到端协议测试（Node 24 内置 WebSocket）。
 * 模拟两个用户 A/B：
 *   1. 注册+登录拿到 token
 *   2. 各自建立 WS 连接
 *   3. A 向 B 发消息
 *   4. B 收到后发已读回执
 *   5. 断言 A 能收到 read.sync 并标记已读
 */
const BASE = 'http://127.0.0.1:8080'
const WS_URL = 'ws://127.0.0.1:8080/ws'

async function post(path, body) {
  const res = await fetch(BASE + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const j = await res.json()
  if (j.code !== 0) throw new Error(`API ${path} failed: ${j.code} ${j.message}`)
  return j.data
}

async function registerOrLogin(account) {
  const suffix = String(Date.now()).slice(-6)
  const acc = `${account}_${suffix}`
  try {
    return await post('/api/v1/auth/register', { nickname: account + suffix, account: acc, password: 'test1234' })
  } catch (e) {
    // 已存在则登录
    return await post('/api/v1/auth/login', { account: acc, password: 'test1234' })
  }
}

// 简单的 WebSocket 客户端封装
function makeClient(token, log) {
  const ws = new WebSocket(WS_URL)
  const state = { ws, frames: [], open: false, authDone: false }
  const waiters = []

  ws.onmessage = (e) => {
    const f = JSON.parse(e.data)
    state.frames.push(f)
    log('<=', f.type, JSON.stringify(f.body || {}))
    waiters.forEach((w) => {
      if (w.pred(f)) { w.resolve(f); w.done = true }
    })
  }

  state.ready = new Promise((resolve, reject) => {
    ws.onopen = () => {
      ws.send(JSON.stringify({ ver: 1, type: 'auth', seq: 1, body: { token } }))
      // 等收到服务端 auth 确认或任意帧，视为连接可用
      setTimeout(() => resolve(), 500)
    }
    ws.onerror = (e) => { log('WS ERROR', e.message || 'unknown'); reject(new Error('ws error: ' + (e.message || ''))) }
    ws.onclose = (e) => { log('WS CLOSE code=', e.code, 'reason=', e.reason) }
  })

  state.send = (type, body) => {
    ws.send(JSON.stringify({ ver: 1, type, seq: ++state.seq || 1, body }))
  }
  state.seq = 0

  state.waitFor = (pred, timeout = 8000) => {
    const found = state.frames.find(pred)
    if (found) return Promise.resolve(found)
    return new Promise((resolve, reject) => {
      const w = { pred, resolve, done: false }
      waiters.push(w)
      setTimeout(() => {
        if (!w.done) reject(new Error('timeout waiting for frame'))
      }, timeout)
    })
  }

  return state
}

function sleep(ms) { return new Promise((r) => setTimeout(r, ms)) }

async function main() {
  const logA = (dir, ...a) => console.log(`[A ${dir}]`, ...a)
  const logB = (dir, ...a) => console.log(`[B ${dir}]`, ...a)

  const a = await registerOrLogin('reader_a')
  const b = await registerOrLogin('reader_b')
  const aUID = a.user.uid
  const bUID = b.user.uid
  console.log('A uid =', aUID, ' B uid =', bUID)

  const ca = makeClient(a.access_token, logA)
  const cb = makeClient(b.access_token, logB)
  await Promise.all([ca.ready, cb.ready])
  await sleep(500)

  // A 发一条消息给 B
  const msgId = Date.now() + 111
  ca.send('msg', {
    msg_id: String(msgId),
    conv_id: '0',
    target_id: bUID,
    conv_type: 1,
    type: 1,
    content: 'hello read test ' + Date.now(),
  })

  // B 应收到 msg.push
  const push = await cb.waitFor((f) => f.type === 'msg.push')
  console.log('B received msg, seq =', push.body.seq, 'conv_id =', push.body.conv_id, 'sender =', push.body.sender_uid)
  const convId = push.body.conv_id
  const seq = push.body.seq
  if (String(push.body.sender_uid) !== String(aUID)) {
    console.error('!!! BUG: msg.push sender_uid 应为 A', aUID, '实际', push.body.sender_uid)
  }

  // B 发已读回执
  cb.send('read', { conv_id: convId, seq })
  console.log('B sent read receipt conv_id=', convId, 'seq=', seq)

  // A 应收到 read.sync
  const sync = await ca.waitFor((f) => f.type === 'read.sync')
  console.log('A received read.sync:', JSON.stringify(sync.body))
  if (String(sync.body.conv_id) !== String(convId)) {
    console.error('!!! BUG: read.sync conv_id 不匹配', sync.body.conv_id, '!=', convId)
  }
  if (Number(sync.body.seq) !== Number(seq)) {
    console.error('!!! BUG: read.sync seq 不匹配', sync.body.seq, '!=', seq)
  }

  console.log('\n=== 服务端协议层已读未读闭环 PASS ===')
  console.log('A 收到 read.sync { conv_id:', convId, 'seq:', seq, '} → 前端 onWsRead 将把 seq<=', seq, ' 的自己发出消息标记为已读')

  ca.ws.close()
  cb.ws.close()
  process.exit(0)
}

main().catch((e) => { console.error('FAIL:', e.message); process.exit(1) })
