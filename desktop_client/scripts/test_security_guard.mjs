/** 冒烟验证 P0 安全修复（真实库）：①二维码确认必须登录 ②历史消息归属校验防越权。 */
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
const a = await call('POST', '/api/v1/auth/register', { nickname: '安全A', account: 'seca_' + suffix, password: 'test1234' })
const b = await call('POST', '/api/v1/auth/register', { nickname: '安全B', account: 'secb_' + suffix, password: 'test1234' })
const c = await call('POST', '/api/v1/auth/register', { nickname: '安全C', account: 'secc_' + suffix, password: 'test1234' })

// ① 二维码确认：未登录直接 confirm 必须被拒（旧漏洞：任意传 uid 即可接管账号）
const qr = await call('POST', '/api/v1/auth/qrcode/create', {})
console.log('未登录 confirm →', await call('POST', '/api/v1/auth/qrcode/confirm', { qrcode_id: qr.qrcode_id, uid: a.user.uid }, null, true), '（期望拦截）')
// 登录后确认自己的二维码仍然正常（功能不受影响）
await call('POST', '/api/v1/auth/qrcode/confirm', { qrcode_id: qr.qrcode_id, uid: 999999 }, a.access_token) // 请求体 uid 应被忽略
const poll = await call('POST', '/api/v1/auth/qrcode/poll', { qrcode_id: qr.qrcode_id })
const okUid = poll.login && poll.login.user.uid === a.user.uid
console.log('已登录 confirm 后 poll 签发的 uid =', poll.login && poll.login.user.uid, '（期望 = A 本人', a.user.uid + '，请求体伪造 uid 被忽略:', okUid + '）')
if (!okUid) throw new Error('confirm 仍信任请求体 uid！')

// ② 历史归属校验：A 发消息给 B，第三方 C 拉取该会话历史必须被拒
await call('POST', '/api/v1/conversations', { conv_id: '0', target_id: b.user.uid, conv_type: 1, type: 1, msg_id: String(Date.now() + 1), content: '私密内容' }, a.access_token)
const aConvs = await call('GET', '/api/v1/conversations', null, a.access_token)
const convId = aConvs[0].id
console.log('C 越权拉历史 →', await call('GET', `/api/v1/conversations/${convId}/messages`, null, c.access_token, true), '（期望拦截）')
console.log('C 越权增量拉取 →', await call('GET', `/api/v1/conversations/${convId}/messages?after_seq=0`, null, c.access_token, true), '（期望拦截）')
const histA = await call('GET', `/api/v1/conversations/${convId}/messages`, null, a.access_token)
const histB = await call('GET', `/api/v1/conversations/${convId}/messages`, null, b.access_token)
console.log('A/B 本人拉历史 =', histA.length, '/', histB.length, '条（期望 1 / 1）')
if (histA.length !== 1 || histB.length !== 1) throw new Error('参与者拉取历史异常')

console.log('PASS')
process.exit(0)
