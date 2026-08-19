/** 冒烟验证阶段二：会话列表差量接口 changed_since（空会话恒返回保证完整，有消息会话按时间过滤）。 */
const BASE = 'http://127.0.0.1:8080'
async function call(method, path, body, token, expectFail = false) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + path, { method, headers: h, body: body ? JSON.stringify(body) : undefined })
  const j = await res.json()
  if (expectFail) {
    if (j.code === 0) throw new Error(path + ' 期望失败但成功了')
    return j.message
  }
  if (j.code !== 0) throw new Error(path + ': ' + j.message)
  return j.data
}

const suffix = String(Date.now()).slice(-4)
const a = await call('POST', '/api/v1/auth/register', { nickname: '差量A', account: 'dfa_' + suffix, password: 'test1234' })
const b = await call('POST', '/api/v1/auth/register', { nickname: '差量B', account: 'dfb_' + suffix, password: 'test1234' })

// 1) A 申请 B 并接受 → 双方产生"空会话"（无消息）
await call('POST', '/api/v1/friends/requests', { to_uid: b.user.uid, message: 'hi' }, a.access_token)
const bReqs = await call('GET', '/api/v1/friends/requests', null, b.access_token)
const req = bReqs.find((r) => Number(r.from_uid) === Number(a.user.uid))
if (!req) throw new Error('B 未收到好友申请')
await call('POST', `/api/v1/friends/requests/${req.id}/handle`, { accept: true }, b.access_token)

// 2) 空会话：未来时间的差量查询也必须返回（保证新会话不遗漏）
const diffFuture1 = await call('GET', `/api/v1/conversations?changed_since=${Math.floor(Date.now() / 1000) + 3600}`, null, b.access_token)
const emptyConv = diffFuture1.find((c) => Number(c.target_id) === Number(a.user.uid) && !c.last_msg_time)
console.log('空会话在未来时间差量中返回 →', emptyConv ? '是（完整）' : '否')
if (!emptyConv) throw new Error('空会话未被差量返回')

// 3) A 给 B 发消息 → 会话获得 last_msg_time
await call('POST', '/api/v1/conversations', { conv_id: '0', target_id: b.user.uid, conv_type: 1, type: 1, msg_id: String(Date.now() + 1), content: '差量测试' }, a.access_token)
const full = await call('GET', '/api/v1/conversations', null, b.access_token)
const t = full[0].last_msg_time
console.log('全量列表 =', full.length, '条，last_msg_time =', t)
if (full.length !== 1 || !t) throw new Error('全量列表异常')

// 4) changed_since=1 → 全部；changed_since=未来 → 空（有消息会话被正确过滤）
const sincePast = await call('GET', '/api/v1/conversations?changed_since=1', null, b.access_token)
const sinceFuture = await call('GET', `/api/v1/conversations?changed_since=${t + 3600}`, null, b.access_token)
console.log('changed_since=1 →', sincePast.length, '条（期望 1）；changed_since=未来 →', sinceFuture.length, '条（期望 0）')
if (sincePast.length !== 1 || sinceFuture.length !== 0) throw new Error('差量过滤不正确')

console.log('PASS')
process.exit(0)
