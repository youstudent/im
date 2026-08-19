/** 验证会话列表接口：peer_read_seq 是否返回、未读数是否正确。 */
const BASE = 'http://127.0.0.1:8080'
async function post(path, body, token) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + path, { method: 'POST', headers: h, body: JSON.stringify(body) })
  return res.json()
}
async function get(path, token) {
  const res = await fetch(BASE + path, { headers: { Authorization: 'Bearer ' + token } })
  return res.json()
}

const suffix = String(Date.now()).slice(-6)
const accA = 'cla_' + suffix
const accB = 'clb_' + suffix

async function reg(acc) {
  const r = await post('/api/v1/auth/register', { nickname: 'ConvList' + acc, account: acc, password: 'test1234' })
  if (r.code !== 0) throw new Error('register failed: ' + r.code + ' ' + r.message)
  return r.data
}

const a = await reg(accA)
const b = await reg(accB)
console.log('A uid', a.user.uid, 'B uid', b.user.uid)

// B 的会话列表（此时 A 还没给 B 发过消息）
let list = await get('/api/v1/conversations', b.access_token)
console.log('B 初始会话数 =', (list.data || []).length)

// A 通过 HTTP 给 B 发一条消息
const msgRes = await post('/api/v1/conversations', {
  conv_id: '0', target_id: b.user.uid, conv_type: 1, type: 1,
  msg_id: String(Date.now() + 7), content: 'conv test msg ' + Date.now(),
}, a.access_token)
console.log('A 发消息返回 code =', msgRes.code, 'msg_id =', msgRes.data && msgRes.data.id)

// 再查 B 的会话列表，找到与 A 的会话
list = await get('/api/v1/conversations', b.access_token)
const aConv = (list.data || []).find((c) => String(c.target_id) === String(a.user.uid))
console.log('B 与 A 的会话:', JSON.stringify(aConv))
console.log('B 的 peer_read_seq =', aConv && aConv.peer_read_seq, ' unread =', aConv && aConv.unread)

// A 的会话列表，查看 peer_read_seq
const aList = await get('/api/v1/conversations', a.access_token)
const bConv = (aList.data || []).find((c) => String(c.target_id) === String(b.user.uid))
console.log('A 与 B 的会话 peer_read_seq =', bConv && bConv.peer_read_seq)

process.exit(0)
