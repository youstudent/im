/**
 * 第三期（P2）群聊/单聊功能冒烟脚本（HTTP API 链路验证）：
 *   G7 入群确认（开关 → 邀请转通知 → 同意入群）
 *   G8 群禁言（全员禁言拦截普通成员、豁免群主）
 *   G2 @所有人（仅群主/管理员可用）
 *   G10 保存到通讯录（saved 开关与群列表回显）
 * 前置：服务端已启动（:8080）且已应用 0014_group_p2.sql 迁移。
 * 运行：node scripts/test_p2_group.mjs
 */
const BASE = 'http://127.0.0.1:8080'
async function post(path, body, token) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + path, { method: 'POST', headers: h, body: JSON.stringify(body) })
  const j = await res.json()
  return { status: res.status, code: j.code, data: j.data, message: j.message }
}
async function put(path, body, token) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + path, { method: 'PUT', headers: h, body: JSON.stringify(body) })
  const j = await res.json()
  return { status: res.status, code: j.code, data: j.data, message: j.message }
}
async function get(path, token) {
  const res = await fetch(BASE + path, { headers: { Authorization: 'Bearer ' + token } })
  const j = await res.json()
  if (j.code !== 0) throw new Error(path + ': ' + j.message)
  return j.data
}
const ok = (cond, label) => {
  console.log((cond ? 'PASS' : 'FAIL') + ' | ' + label)
  if (!cond) process.exitCode = 1
}

const suffix = String(Date.now()).slice(-4)
const a = (await post('/api/v1/auth/register', { nickname: 'P2群主', account: 'p2a_' + suffix, password: 'test1234' })).data
const b = (await post('/api/v1/auth/register', { nickname: 'P2成员', account: 'p2b_' + suffix, password: 'test1234' })).data
const c = (await post('/api/v1/auth/register', { nickname: 'P2外部', account: 'p2c_' + suffix, password: 'test1234' })).data
console.log('注册完成 a=%d b=%d c=%d', a.user.uid, b.user.uid, c.user.uid)

// ---- 建群 ----
const grp = await post('/api/v1/groups', { name: 'P2测试群' + suffix, members: [b.user.uid] }, a.access_token)
ok(grp.code === 0, '建群成功')
const gUid = grp.data.g_uid

// ---- G7 入群确认 ----
let r = await put(`/api/v1/groups/${gUid}/settings`, { invite_confirm: 1 }, a.access_token)
ok(r.code === 0, 'G7 开启入群确认')
// 普通成员 B 邀请 C：不直接入群
r = await post(`/api/v1/groups/${gUid}/members`, { members: [c.user.uid] }, b.access_token)
ok(r.code !== 0 && String(r.message).includes('入群确认'), 'G7 开启后邀请被延迟: ' + r.message)
let ginfo = await get(`/api/v1/groups/${gUid}`, a.access_token)
ok(!ginfo.members.includes(c.user.uid), 'G7 确认前 C 未入群')
// 群主同意入群
r = await post(`/api/v1/groups/${gUid}/invites/decide`, { invitee_uid: c.user.uid, accept: true }, a.access_token)
ok(r.code === 0, 'G7 群主同意入群')
ginfo = await get(`/api/v1/groups/${gUid}`, a.access_token)
ok(ginfo.members.includes(c.user.uid), 'G7 同意后 C 已入群')
// 普通成员无权决定
r = await post(`/api/v1/groups/${gUid}/invites/decide`, { invitee_uid: c.user.uid, accept: true }, b.access_token)
ok(r.code !== 0, 'G7 普通成员无权处理入群申请')
// 关闭开关
r = await put(`/api/v1/groups/${gUid}/settings`, { invite_confirm: 0 }, a.access_token)
ok(r.code === 0, 'G7 关闭入群确认')

// ---- G8 群禁言 ----
r = await put(`/api/v1/groups/${gUid}/settings`, { mute_all: 1 }, a.access_token)
ok(r.code === 0, 'G8 开启全员禁言')
// 普通成员 B 发消息被拒
r = await post('/api/v1/conversations', { conv_id: '0', target_id: gUid, conv_type: 2, type: 1, msg_id: String(Date.now() + 1), content: '禁言下发送' }, b.access_token)
ok(r.code !== 0 && String(r.message).includes('禁言'), 'G8 普通成员被全员禁言拦截: ' + r.message)
// 群主 A 可发
r = await post('/api/v1/conversations', { conv_id: '0', target_id: gUid, conv_type: 2, type: 1, msg_id: String(Date.now() + 2), content: '群主消息' }, a.access_token)
ok(r.code === 0, 'G8 群主不受全员禁言影响')
// 关闭禁言
r = await put(`/api/v1/groups/${gUid}/settings`, { mute_all: 0 }, a.access_token)
ok(r.code === 0, 'G8 解除全员禁言')
// 个人禁言 B
r = await put(`/api/v1/groups/${gUid}/members/${b.user.uid}/mute`, { until: Date.now() + 3600000 }, a.access_token)
ok(r.code === 0, 'G8 个人禁言 B')
r = await post('/api/v1/conversations', { conv_id: '0', target_id: gUid, conv_type: 2, type: 1, msg_id: String(Date.now() + 3), content: '个人禁言下发送' }, b.access_token)
ok(r.code !== 0 && String(r.message).includes('禁言'), 'G8 个人禁言拦截 B: ' + r.message)
r = await put(`/api/v1/groups/${gUid}/members/${b.user.uid}/mute`, { until: 0 }, a.access_token)
ok(r.code === 0, 'G8 解除个人禁言')

// ---- G2 @所有人 ----
// 普通成员 B @所有人 被拒
r = await post('/api/v1/conversations', { conv_id: '0', target_id: gUid, conv_type: 2, type: 1, msg_id: String(Date.now() + 4), content: '@所有人 越权', extra: JSON.stringify({ mention_uids: ['all'] }) }, b.access_token)
ok(r.code !== 0 && String(r.message).includes('群主或管理员'), 'G2 普通成员 @所有人 被拒: ' + r.message)
// 群主 A @所有人 成功（通知中心有 mention 条目）
r = await post('/api/v1/conversations', { conv_id: '0', target_id: gUid, conv_type: 2, type: 1, msg_id: String(Date.now() + 5), content: '@所有人 全体通知', extra: JSON.stringify({ mention_uids: ['all'] }) }, a.access_token)
ok(r.code === 0, 'G2 群主 @所有人 成功')
const notifs = await get('/api/v1/notifications', b.access_token)
ok(notifs.some((n) => n.type === 'mention' && String(n.summary).includes('全体通知')), 'G2 被 @ 成员收到 mention 通知')

// ---- G10 保存到通讯录 ----
r = await put(`/api/v1/groups/${gUid}/saved`, { saved: 0 }, b.access_token)
ok(r.code === 0, 'G10 B 关闭保存到通讯录')
let groups = await get('/api/v1/groups', b.access_token)
const bg = groups.find((g) => Number(g.g_uid) === Number(gUid))
ok(bg && bg.saved === 0, 'G10 群列表回显 saved=0')
r = await put(`/api/v1/groups/${gUid}/saved`, { saved: 1 }, b.access_token)
ok(r.code === 0, 'G10 B 重新开启保存到通讯录')
groups = await get('/api/v1/groups', b.access_token)
ok(groups.find((g) => Number(g.g_uid) === Number(gUid)).saved === 1, 'G10 群列表回显 saved=1')

console.log('--- 冒烟完成（exitCode=' + process.exitCode + '）---')
