/** 冒烟验证 P1/P2 优化（真实库）：搜索分表路由+权限范围、unread_count 撤回递减。 */
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
const kw = '烟测' + suffix // 唯一关键字，避免历史数据干扰
const a = await post('/api/v1/auth/register', { nickname: '烟测A', account: 'ska_' + suffix, password: 'test1234' })
const b = await post('/api/v1/auth/register', { nickname: '烟测B', account: 'skb_' + suffix, password: 'test1234' })
const aUID = a.user.uid
const bUID = b.user.uid

// A 发 2 条给 B
const m1 = await post('/api/v1/conversations', { conv_id: '0', target_id: bUID, conv_type: 1, type: 1, msg_id: String(Date.now() + 1), content: kw + ' 第一条' }, a.access_token)
await post('/api/v1/conversations', { conv_id: String(m1.conv_id), target_id: bUID, conv_type: 1, type: 1, msg_id: String(Date.now() + 2), content: kw + ' 第二条' }, a.access_token)

const unreadOf = async (tok, peerUID) => {
  const convs = await get('/api/v1/conversations', tok)
  const c = convs.find((x) => String(x.target_id) === String(peerUID))
  return c && c.unread
}
console.log('B unread =', await unreadOf(b.access_token, aUID), '（期望 2）')

// 搜索：B 能搜到 2 条（走分表路由 SQL）；搜索范围仅限自身会话
const hitsB = await get('/api/v1/conversations/search?keyword=' + encodeURIComponent(kw), b.access_token)
console.log('B 搜索命中 =', hitsB.length, '（期望 2），首条 sender_name =', hitsB[0] && hitsB[0].message.sender_name)

// A 撤回第一条 → B 未读 2-1=1（撤回递减，不虚高）
await post(`/api/v1/conversations/${m1.conv_id}/recall`, { msg_id: String(m1.id) }, a.access_token)
console.log('撤回一条后 B unread =', await unreadOf(b.access_token, aUID), '（期望 1）')

process.exit(0)
