/** 验证管理后台群组列表接口 /api/admin/groups 是否修复（此前 500 报错）。 */
const BASE = 'http://127.0.0.1:8080'
async function req(path, method = 'GET', body, token) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + path, { method, headers: h, body: body ? JSON.stringify(body) : undefined })
  const j = await res.json()
  return { status: res.status, body: j }
}

// 登录 admin
const login = await req('/api/admin/login', 'POST', { username: 'admin', password: 'admin123' })
if (login.body.code !== 0) {
  console.log('admin 登录失败:', JSON.stringify(login.body))
  process.exit(1)
}
const token = login.body.data.token || login.body.data.access_token
console.log('admin 登录成功')

// 调用群组列表接口
const r = await req('/api/admin/groups?offset=0&limit=10', 'GET', null, token)
console.log('GET /api/admin/groups => HTTP', r.status, 'code', r.body.code, 'message', r.body.message)
if (r.status === 200 && r.body.code === 0) {
  console.log('群组列表条数 =', (r.body.data.list || []).length, ' total =', r.body.data.total)
  console.log('PASS：群组管理列表接口正常')
} else {
  console.log('FAIL：接口仍报错', JSON.stringify(r.body))
}

// 调用数据看板接口，验证趋势数据
const d = await req('/api/admin/dashboard', 'GET', null, token)
console.log('\nGET /api/admin/dashboard => HTTP', d.status, 'code', d.body.code)
if (d.status === 200 && d.body.code === 0) {
  const data = d.body.data
  console.log('users =', data.users, ' groups =', data.groups, ' messages =', data.messages, ' online =', data.online)
  console.log('user_trend(7d) =', JSON.stringify(data.user_trend))
  console.log('message_trend(7d) =', JSON.stringify(data.message_trend))
  const okTrend = Array.isArray(data.user_trend) && data.user_trend.length === 7 && Array.isArray(data.message_trend) && data.message_trend.length === 7
  console.log(okTrend ? 'PASS：看板返回 7 天趋势数据' : 'FAIL：看板趋势数据不完整')
} else {
  console.log('FAIL：看板接口报错', JSON.stringify(d.body))
}
process.exit(0)
