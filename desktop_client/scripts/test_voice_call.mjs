/**
 * 语音通话信令端到端测试（Node 24 内置 WebSocket）。
 * 前置：服务端已在 127.0.0.1:8080 运行（含本次 call 信令改动的新二进制）。
 *
 * 注意：场景 5（离线代答）用固定账号 callc_offline_target 作为离线目标：
 * 该账号仅 HTTP 注册/登录拿 uid，全程不开 WS（presence 仅由 WS 连接写入）→ 恒为真离线。
 * 若用“本次新注册且曾连过 WS”的账号，服务端 pongWait 窗口内残留 presence 会误判在线。
 *
 * 覆盖场景：
 *   1. invite 转发：A 呼叫 B，B 收到 invite 且 from 为 A 的真实 uid
 *   2. answer/ice/hangup 透传
 *   3. 对端离线 invite → 发起方收到 offline 代答
 *   4. 忙线 invite → 发起方收到 busy 代答（busy.set/busy.clear 维护）
 *   5. 好友校验：对非好友发起 invite 被静默丢弃
 */
const BASE = 'http://127.0.0.1:8080'
const WS_URL = 'ws://127.0.0.1:8080/ws'

let failed = 0
function ok(cond, label) {
  if (cond) {
    console.log('PASS:', label)
  } else {
    failed++
    console.error('FAIL:', label)
  }
}

async function call(method, path, body, token, allowError = false) {
  // 服务端偏态 500（如 MySQL 陈旧连接）重试一次，避免环境抖动误判测试失败
  for (let attempt = 0; attempt < 2; attempt++) {
    const res = await fetch(BASE + path, {
      method,
      headers: {
        'Content-Type': 'application/json',
        ...(token ? { Authorization: 'Bearer ' + token } : {}),
      },
      body: body ? JSON.stringify(body) : undefined,
    })
    const j = await res.json()
    if (j.code !== 0) {
      if (res.status >= 500 && attempt === 0) continue
      if (allowError) return { __error: j.message }
      throw new Error(`API ${path} failed: ${j.code} ${j.message}`)
    }
    return j.data
  }
}

async function registerOrLogin(account) {
  const suffix = String(Date.now()).slice(-6)
  const acc = `${account}_${suffix}`
  try {
    return await call('POST', '/api/v1/auth/register', {
      nickname: account + suffix,
      account: acc,
      password: 'test1234',
    })
  } catch (e) {
    return await call('POST', '/api/v1/auth/login', { account: acc, password: 'test1234' })
  }
}

// 登录已存在的固定账号；不存在则注册。全程不建 WS 连接，保证无 presence → 真离线
async function ensureOfflineAccount(account) {
  try {
    return await call('POST', '/api/v1/auth/login', { account, password: 'test1234' })
  } catch {
    return await call('POST', '/api/v1/auth/register', {
      nickname: account,
      account,
      password: 'test1234',
    })
  }
}

async function makeFriends(a, b) {
  const reqRes = await call(
    'POST',
    '/api/v1/friends/requests',
    { to_uid: b.user.uid, message: 'hi' },
    a.access_token,
    true
  )
  if (reqRes && reqRes.__error) {
    // 幂等：已是好友（固定账号二次运行）无需再申请
    if (String(reqRes.__error).includes('已是你的好友')) return
    throw new Error('好友申请失败: ' + reqRes.__error)
  }
  const reqs = await call('GET', '/api/v1/friends/requests', null, b.access_token)
  const req = reqs.find((r) => Number(r.from_uid) === Number(a.user.uid))
  if (!req) throw new Error('B 未收到 A 的好友申请')
  await call('POST', `/api/v1/friends/requests/${req.id}/handle`, { accept: true }, b.access_token)
}

function makeClient(token, log) {
  const ws = new WebSocket(WS_URL)
  const state = { ws, frames: [] }
  const waiters = []

  ws.onmessage = (e) => {
    const f = JSON.parse(e.data)
    state.frames.push(f)
    log('<=', f.type)
    waiters.forEach((w) => {
      if (!w.done && w.pred(f)) {
        w.resolve(f)
        w.done = true
      }
    })
  }

  state.ready = new Promise((resolve, reject) => {
    ws.onopen = () => {
      ws.send(JSON.stringify({ ver: 1, type: 'auth', seq: 1, body: { token } }))
      setTimeout(resolve, 400)
    }
    ws.onerror = () => reject(new Error('ws error'))
  })

  state.seq = 1
  state.send = (type, body) => {
    ws.send(JSON.stringify({ ver: 1, type, seq: ++state.seq, body }))
    log('=>', type)
  }
  state.sendCall = (body) => state.send('call', body)

  state.waitFor = (pred, timeout = 5000) => {
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
  state.close = () => {
    try { ws.close() } catch {}
  }
  return state
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function main() {
  // ---- 准备：注册 A/B/D，A 与 B 互为好友（D 与 A 非好友）----
  const a = await registerOrLogin('calla')
  const b = await registerOrLogin('callb')
  const d = await registerOrLogin('calld')
  await makeFriends(a, b)
  // 离线目标：固定账号，仅 HTTP 拿 uid，不建 WS（无 presence 键 → 恒离线）；
  // 需与 A 互为好友，否则信令在好友校验层被静默拦截（到不了在线/离线判定）
  const c = await ensureOfflineAccount('callc_offline_target')
  await makeFriends(a, c)
  const aUID = a.user.uid
  const bUID = b.user.uid
  console.log('A =', aUID, 'B =', bUID)

  const logA = (...args) => console.log('[A]', ...args)
  const logB = (...args) => console.log('[B]', ...args)
  const logD = (...args) => console.log('[D]', ...args)

  const ca = makeClient(a.access_token, logA)
  const cb = makeClient(b.access_token, logB)
  const cd = makeClient(d.access_token, logD)
  await Promise.all([ca.ready, cb.ready, cd.ready])

  const callId = 'e2e-' + Date.now()

  // ---- 1. invite 转发 ----
  ca.sendCall({ call_id: callId, action: 'invite', to: Number(bUID), from: 999999, payload: { sdp: 'offer-fake-sdp' } })
  const invite = await cb.waitFor((f) => f.type === 'call.push' && f.body?.action === 'invite')
  ok(invite.body.from === Number(aUID), 'invite 转发且 from 覆写为真实 uid（防伪造）')
  ok(invite.body.payload?.sdp === 'offer-fake-sdp', 'invite 携带 SDP 载荷')

  // ---- 2. answer / ice / hangup 透传 ----
  cb.sendCall({ call_id: callId, action: 'answer', to: Number(aUID), payload: { sdp: 'answer-sdp' } })
  const answer = await ca.waitFor((f) => f.type === 'call.push' && f.body?.action === 'answer')
  ok(answer.body.from === Number(bUID), 'answer 透传')

  cb.sendCall({ call_id: callId, action: 'ice', to: Number(aUID), payload: { candidate: 'cand-1' } })
  const ice = await ca.waitFor((f) => f.type === 'call.push' && f.body?.action === 'ice')
  ok(ice.body.payload?.candidate === 'cand-1', 'ice 透传')

  // ---- 3. 忙线：B 置忙后 A 再呼叫 → busy 代答 ----
  cb.sendCall({ call_id: callId, action: 'busy.set' })
  await sleep(300)
  const callId2 = callId + '-busy'
  ca.sendCall({ call_id: callId2, action: 'invite', to: Number(bUID) })
  let busyReply = null
  try {
    busyReply = await ca.waitFor((f) => f.type === 'call.push' && f.body?.call_id === callId2 && f.body?.action === 'busy')
  } catch {}
  ok(busyReply && busyReply.body.from === Number(bUID), '对端忙线时收到 busy 代答')
  cb.sendCall({ call_id: callId, action: 'busy.clear' })
  await sleep(300)

  // ---- 4. 挂断透传 ----
  ca.sendCall({ call_id: callId, action: 'hangup', to: Number(bUID) })
  const hangup = await cb.waitFor((f) => f.type === 'call.push' && f.body?.action === 'hangup')
  ok(hangup.body.from === Number(aUID), 'hangup 透传')

  // ---- 5. 对端离线 invite → offline 代答（C 已登录拿 uid 但不连 WS）----
  let offlineChecked = false
  if (c && c.user && c.user.uid) {
    const callId3 = callId + '-offline'
    ca.sendCall({ call_id: callId3, action: 'invite', to: Number(c.user.uid) })
    let offlineReply = null
    try {
      offlineReply = await ca.waitFor((f) => f.type === 'call.push' && f.body?.call_id === callId3 && f.body?.action === 'offline')
    } catch {}
    ok(offlineReply && offlineReply.body.from === Number(c.user.uid), '对端离线时收到 offline 代答')
    offlineChecked = true
  }
  if (!offlineChecked) {
    console.log('SKIP: 离线代答（无可用离线目标账号，二次运行本脚本即可覆盖）')
  }

  // ---- 6. 好友校验：A 呼叫非好友 D，D 不应收到任何 call.push ----
  ca.sendCall({ call_id: callId + '-stranger', action: 'invite', to: Number(d.user.uid) })
  await sleep(800)
  const dGotCall = cd.frames.some((f) => f.type === 'call.push')
  ok(!dGotCall, '非好友 invite 被好友校验拦截（D 未收到信令）')

  ca.close()
  cb.close()
  cd.close()
  // 等待服务端完成连接清理（presence 删除），避免残留在线状态干扰下次运行的离线场景
  await sleep(800)

  console.log(failed === 0 ? '\nALL PASS' : `\n${failed} FAILED`)
  process.exit(failed === 0 ? 0 : 1)
}

main().catch((e) => {
  console.error('E2E error:', e)
  process.exit(1)
})
