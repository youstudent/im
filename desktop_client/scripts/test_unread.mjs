/** 验证服务端未读数：A 发消息给 B（B 未读），B 会话 unread 是否 > 0。 */
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
const a = await post('/api/v1/auth/register', { nickname: '未读A', account: 'unra_' + suffix, password: 'test1234' })
const b = await post('/api/v1/auth/register', { nickname: '未读B', account: 'unrb_' + suffix, password: 'test1234' })
const aUID = a.user.uid
const bUID = b.user.uid

// A 发 3 条消息给 B
for (let i = 1; i <= 3; i++) {
  await post('/api/v1/conversations', { conv_id: '0', target_id: bUID, conv_type: 1, type: 1, msg_id: String(Date.now() + i), content: 'unread msg ' + i }, a.access_token)
}

// 查 B 的会话 unread（B 还没读，应有未读）
const bConvs = await get('/api/v1/conversations', b.access_token)
const bConv = bConvs.find((c) => String(c.target_id) === String(aUID))
console.log('B 会话 unread =', bConv && bConv.unread, '（期望 > 0）')
console.log('B 会话 =', JSON.stringify(bConv))

process.exit(0)
