/** 冒烟验证 P1 安全修复（真实库）：登录限流、发送守卫、群信息守卫、预签名约束、版本发布强制 SHA-256。 */
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
const a = await call('POST', '/api/v1/auth/register', { nickname: 'P1A', account: 'p1a_' + suffix, password: 'test1234' })
const b = await call('POST', '/api/v1/auth/register', { nickname: 'P1B', account: 'p1b_' + suffix, password: 'test1234' })

// 1) 登录限流：窗口内尝试超限后拒绝
let last = ''
for (let i = 0; i < 11; i++) {
  const r = await fetch(BASE + '/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ account: 'p1a_' + suffix, password: 'wrong' + i }) })
  last = (await r.json()).message
}
console.log('连续错误登录 11 次 →', last, '（期望限流提示）')
if (!last.includes('频繁')) throw new Error('登录限流未生效')

// 2) 发送守卫：目标不存在拒绝；正常用户放行
console.log('发给不存在用户 →', await call('POST', '/api/v1/conversations', { conv_id: '0', target_id: 999999999, conv_type: 1, type: 1, msg_id: String(Date.now() + 1), content: 'x' }, a.access_token, true), '（期望拦截）')
await call('POST', '/api/v1/conversations', { conv_id: '0', target_id: b.user.uid, conv_type: 1, type: 1, msg_id: String(Date.now() + 2), content: '正常消息' }, a.access_token)
console.log('发给真实用户 → 正常放行')

// 3) 群守卫：A 建群，B 非成员发消息/查群信息均被拒
const g = await call('POST', '/api/v1/groups', { name: 'P1守卫群' + suffix, members: [] }, a.access_token)
console.log('非成员查群信息 →', await call('GET', `/api/v1/groups/${g.g_uid}`, null, b.access_token, true), '（期望拦截）')
console.log('非成员群发消息 →', await call('POST', '/api/v1/conversations', { conv_id: '0', target_id: g.g_uid, conv_type: 2, type: 1, msg_id: String(Date.now() + 3), content: '灌水' }, b.access_token, true), '（期望拦截）')
const gInfo = await call('GET', `/api/v1/groups/${g.g_uid}`, null, a.access_token)
console.log('成员查群信息 → 正常（群名', gInfo.name + '）')

// 4) 预签名约束：html 拒绝、size 非法拒绝、png 正常
console.log('presign .html →', await call('POST', '/api/v1/files/presign', { file_name: 'x.html', type: 'file', size: 100, content_type: 'text/html' }, a.access_token, true), '（期望拦截）')
console.log('presign size=0 →', await call('POST', '/api/v1/files/presign', { file_name: 'x.png', type: 'image', size: 0, content_type: 'image/png' }, a.access_token, true), '（期望拦截）')
const ok = await call('POST', '/api/v1/files/presign', { file_name: 'x.png', type: 'image', size: 100, content_type: 'image/png' }, a.access_token)
console.log('presign .png 正常 →', ok.object_key ? 'OK' : 'FAIL')

// 5) 版本发布：无 SHA-256 拒绝；带 SHA-256 成功且 latest 返回摘要（用低版本号 0.0.1 避免触发客户端更新提示）
const admin = await call('POST', '/api/admin/login', { username: 'admin', password: 'admin123' })
console.log('发布无 SHA-256 →', await call('POST', '/api/admin/version', { version: '0.0.1', download_url: 'https://example.com/x.exe' }, admin.access_token, true), '（期望拦截）')
const sha = 'ab'.repeat(32)
await call('POST', '/api/admin/version', { version: '0.0.1', download_url: 'https://example.com/x.exe', sha256: sha, release_notes: 'P1 冒烟' }, admin.access_token)
const latest = await call('GET', '/api/v1/version/latest')
console.log('发布带 SHA-256 → latest.sha256 =', latest.version.sha256.slice(0, 8) + '…（期望 ' + sha.slice(0, 8) + '…）')
if (latest.version.sha256 !== sha) throw new Error('latest 未返回 sha256')

console.log('PASS')
process.exit(0)
