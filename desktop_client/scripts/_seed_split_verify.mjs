/** 临时造数脚本：为拆分重构的 UI 冒烟验证准备账号与消息数据（验证后可删） */
const BASE = 'http://127.0.0.1:8080'
async function call(method, path, body, token) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + path, { method, headers: h, body: body ? JSON.stringify(body) : undefined })
  const j = await res.json()
  if (j.code !== 0) throw new Error(path + ': ' + j.message)
  return j.data
}

const suffix = String(Date.now()).slice(-4)
const ACC_A = 'splita_' + suffix
const ACC_B = 'splitb_' + suffix
const PASSWORD = 'test1234'

const a = await call('POST', '/api/v1/auth/register', { nickname: '拆分A', account: ACC_A, password: PASSWORD })
const b = await call('POST', '/api/v1/auth/register', { nickname: '拆分B', account: ACC_B, password: PASSWORD })

// A 申请 B 并接受
await call('POST', '/api/v1/friends/requests', { to_uid: b.user.uid, message: 'hi' }, a.access_token)
const bReqs = await call('GET', '/api/v1/friends/requests', null, b.access_token)
const req = bReqs.find((r) => Number(r.from_uid) === Number(a.user.uid))
await call('POST', `/api/v1/friends/requests/${req.id}/handle`, { accept: true }, b.access_token)

// B 给 A 发几条文本消息（A 登录后会话列表可见、气泡可渲染）
for (let i = 1; i <= 3; i++) {
  await call(
    'POST', '/api/v1/conversations',
    { conv_id: '0', target_id: a.user.uid, conv_type: 1, type: 1, msg_id: String(Date.now() + i), content: '拆分验证消息 ' + i },
    b.access_token
  )
}
// A 回一条
await call(
  'POST', '/api/v1/conversations',
  { conv_id: '0', target_id: b.user.uid, conv_type: 1, type: 1, msg_id: String(Date.now() + 100), content: '收到，测试回复' },
  a.access_token
)

console.log('ACCOUNT=' + ACC_A)
console.log('PASSWORD=' + PASSWORD)
console.log('SEED OK')
