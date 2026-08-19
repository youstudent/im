/** 验证：A 发消息后，A 与 B 各自会话的 conv_id 是否一致（决定历史/已读是否互通）。 */
const BASE = 'http://127.0.0.1:8080'
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

const suffix = String(Date.now()).slice(-4)
const a = await post('/api/v1/auth/register', { nickname: 'CIDA', account: 'cida_' + suffix, password: 'test1234' })
const b = await post('/api/v1/auth/register', { nickname: 'CIDB', account: 'cidb_' + suffix, password: 'test1234' })
const aUID = a.user.uid
const bUID = b.user.uid

// A 发两条消息给 B
for (let i = 1; i <= 2; i++) {
  await post('/api/v1/conversations', { conv_id: '0', target_id: bUID, conv_type: 1, type: 1, msg_id: String(Date.now() + i), content: 'cid msg ' + i }, a.access_token)
}

// 查 A 和 B 的会话列表
const aConvs = await get('/api/v1/conversations', a.access_token)
const bConvs = await get('/api/v1/conversations', b.access_token)
const aConv = aConvs.find((c) => String(c.target_id) === String(bUID))
const bConv = bConvs.find((c) => String(c.target_id) === String(aUID))
console.log('A 会话 conv_id =', aConv && aConv.id, ' last_msg =', aConv && aConv.last_msg)
console.log('B 会话 conv_id =', bConv && bConv.id, ' last_msg =', bConv && bConv.last_msg)
console.log('两者 conv_id 一致？', aConv && bConv && String(aConv.id) === String(bConv.id))

// A 会话历史
const aHist = await get('/api/v1/conversations/' + aConv.id + '/messages', a.access_token)
console.log('A 会话历史条数 =', (aHist || []).length, ' 内容 =', JSON.stringify((aHist || []).map((m) => m.content)))
// B 会话历史（用 B 的 conv_id）
const bHist = await get('/api/v1/conversations/' + bConv.id + '/messages', b.access_token)
console.log('B 会话历史条数 =', (bHist || []).length, ' 内容 =', JSON.stringify((bHist || []).map((m) => m.content)))
process.exit(0)
