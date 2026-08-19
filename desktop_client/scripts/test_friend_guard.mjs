/** 冒烟验证好友申请守卫（真实库）：不可加自己、不可加已是好友、目标不存在拦截。 */
const BASE = 'http://127.0.0.1:8080'
async function call(method, path, body, token, expectFail = false) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + path, { method, headers: h, body: body ? JSON.stringify(body) : undefined })
  const j = await res.json()
  if (expectFail) {
    if (j.code === 0) throw new Error(path + ' 期望被拦截但成功了')
    return j.message
  }
  if (j.code !== 0) throw new Error(path + ': ' + j.message)
  return j.data
}

const suffix = String(Date.now()).slice(-4)
const a = await call('POST', '/api/v1/auth/register', { nickname: '守卫A', account: 'fga_' + suffix, password: 'test1234' })
const b = await call('POST', '/api/v1/auth/register', { nickname: '守卫B', account: 'fgb_' + suffix, password: 'test1234' })
const aUID = a.user.uid
const bUID = b.user.uid

// 1) 不可添加自己
console.log('加自己 →', await call('POST', '/api/v1/friends/requests', { to_uid: aUID, message: '' }, a.access_token, true), '（期望拦截）')

// 2) 目标用户不存在
console.log('加不存在用户 →', await call('POST', '/api/v1/friends/requests', { to_uid: 999999999, message: '' }, a.access_token, true), '（期望拦截）')

// 3) 正常流程：A 申请 B，B 接受，成为好友
await call('POST', '/api/v1/friends/requests', { to_uid: bUID, message: '你好' }, a.access_token)
const bReqs = await call('GET', '/api/v1/friends/requests', null, b.access_token)
const req = bReqs.find((r) => Number(r.from_uid) === Number(aUID))
if (!req) throw new Error('B 未收到 A 的申请')
await call('POST', `/api/v1/friends/requests/${req.id}/handle`, { accept: true }, b.access_token)
const aFriends = await call('GET', '/api/v1/friends', null, a.access_token)
console.log('成为好友后 A 好友数 =', aFriends.length, '（期望 1）')

// 4) 已是好友再申请 → 拦截
console.log('已是好友再申请 →', await call('POST', '/api/v1/friends/requests', { to_uid: bUID, message: 'again' }, a.access_token, true), '（期望拦截）')

console.log('PASS')
process.exit(0)
