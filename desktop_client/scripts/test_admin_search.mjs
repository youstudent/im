/** 验证管理后台用户/群组列表搜索接口（q 参数）。 */
const BASE = 'http://127.0.0.1:8080'
async function req(path, method = 'GET', body, token) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + path, { method, headers: h, body: body ? JSON.stringify(body) : undefined })
  return res.json()
}

const login = await req('/api/admin/login', 'POST', { username: 'admin', password: 'admin123' })
const token = login.data.access_token || login.data.token
console.log('admin 登录成功')

// 用户搜索：搜昵称
const u1 = await req('/api/admin/users?offset=0&limit=10&q=测试', 'GET', null, token)
console.log('用户搜索 q=测试 => code', u1.code, 'total', u1.data && u1.data.total, '条数', (u1.data && u1.data.list || []).length)
console.log('  结果示例 =', JSON.stringify((u1.data && u1.data.list || []).slice(0, 3).map(u => ({ nickname: u.nickname, account: u.account }))))

// 用户搜索：搜账号
const u2 = await req('/api/admin/users?offset=0&limit=10&q=sena', 'GET', null, token)
console.log('用户搜索 q=sena => code', u2.code, 'total', u2.data && u2.data.total)

// 群搜索：搜群名
const g1 = await req('/api/admin/groups?offset=0&limit=10&q=测试群', 'GET', null, token)
console.log('群搜索 q=测试群 => code', g1.code, 'total', g1.data && g1.data.total, '条数', (g1.data && g1.data.list || []).length)
console.log('  结果示例 =', JSON.stringify((g1.data && g1.data.list || []).slice(0, 3).map(g => ({ name: g.name, g_uid: g.g_uid }))))

// 群搜索：搜群号
const g2 = await req('/api/admin/groups?offset=0&limit=10&q=4382', 'GET', null, token)
console.log('群搜索 q=4382 => code', g2.code, 'total', g2.data && g2.data.total)

// 空搜索应返回全部
const u3 = await req('/api/admin/users?offset=0&limit=10', 'GET', null, token)
const g3 = await req('/api/admin/groups?offset=0&limit=10', 'GET', null, token)
console.log('\n空搜索 users total =', u3.data && u3.data.total, ' groups total =', g3.data && g3.data.total)

const ok = u1.code === 0 && u2.code === 0 && g1.code === 0 && g2.code === 0 && u3.code === 0
console.log(ok ? 'PASS：搜索接口正常' : 'FAIL：搜索接口异常')
process.exit(0)
