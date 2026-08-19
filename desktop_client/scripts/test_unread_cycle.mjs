/** 完整验证未读闭环：A 发消息 → B 未读(unread>0) → B 发送已读回执 → unread 归 0 → 刷新仍为 0。 */
const BASE = 'http://127.0.0.1:8080'
const WS_URL = 'ws://127.0.0.1:8080/ws'
async function post(path, body, token) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + path, { method: 'POST', headers: h, body: JSON.stringify(body) })
  const j = await res.json()
  if (j.code !== 0) throw new Error(path + ': ' + j.message)
  return j.data
}
async function get(path, token) {
  const res = await fetch(BASE + path, { headers: { Authorization: 'Bearer ' + token } })
  const j = await res.json()
  if (j.code !== 0) throw new Error(path + ': ' + j.message)
  return j.data
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

const suffix = String(Date.now()).slice(-4)
const a = await post('/api/v1/auth/register', { nickname: '周期A', account: 'cyca_' + suffix, password: 'test1234' })
const b = await post('/api/v1/auth/register', { nickname: '周期B', account: 'cycb_' + suffix, password: 'test1234' })
const aUID = a.user.uid
const bUID = b.user.uid

for (let i = 1; i <= 3; i++) {
  await post('/api/v1/conversations', { conv_id: '0', target_id: bUID, conv_type: 1, type: 1, msg_id: String(Date.now() + i), content: 'cycle msg ' + i }, a.access_token)
}

const getUnread = async () => {
  const bConvs = await get('/api/v1/conversations', b.access_token)
  const conv = bConvs.find((c) => String(c.target_id) === String(aUID))
  return { unread: conv && conv.unread, convId: conv && conv.id, lastSeq: conv && conv.unread + 999 }
}

// 1) B 未读
let s = await getUnread()
console.log('① B 未读时 unread =', s.unread, '（期望 3）')

// 2) B 建立 WS，发已读回执（读到会话最新 seq）
const ws = new WebSocket(WS_URL)
await new Promise((res, rej) => { ws.onopen = res; ws.onerror = rej })
ws.send(JSON.stringify({ ver: 1, type: 'auth', seq: 1, body: { token: b.access_token } }))
await sleep(600)
// 读 A 会话历史拿最新 seq
const hist = await get('/api/v1/conversations/' + s.convId + '/messages', b.access_token)
const maxSeq = Math.max(...hist.map((m) => m.seq))
console.log('② B 会话最新 seq =', maxSeq)
ws.send(JSON.stringify({ ver: 1, type: 'read', seq: 2, body: { conv_id: s.convId, seq: maxSeq } }))
await sleep(800)
ws.close()

// 3) B 已读后 unread
s = await getUnread()
console.log('③ B 已读后 unread =', s.unread, '（期望 0）')

process.exit(0)
