/**
 * 综合功能冒烟脚本：覆盖《群聊单聊功能完善方案》功能总表中未被专项脚本覆盖的核心项。
 *   L1/L6/L13 会话置顶（含排序）、L2 消息免打扰
 *   S1 引用回复（extra.quote 协议与历史回显）、S2 合并转发（type=7）
 *   S5 撤回（status=1 + 会话预览回退）
 *   G1 @提及（mention_uids → 通知中心）、G3 移除成员、G4 管理员、G5 转让群主、G6 群昵称、G11 公告
 * 前置：服务端 :8080 已运行。运行：node scripts/test_full_features.mjs
 */
const BASE = 'http://127.0.0.1:8080'
async function post(p, body, token) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + p, { method: 'POST', headers: h, body: JSON.stringify(body) })
  const j = await res.json()
  return { code: j.code, data: j.data, message: j.message }
}
async function put(p, body, token) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + p, { method: 'PUT', headers: h, body: JSON.stringify(body) })
  const j = await res.json()
  return { code: j.code, data: j.data, message: j.message }
}
async function get(p, token) {
  const res = await fetch(BASE + p, { headers: { Authorization: 'Bearer ' + token } })
  const j = await res.json()
  if (j.code !== 0) throw new Error(p + ': ' + j.message)
  return j.data
}
const ok = (cond, label) => {
  console.log((cond ? 'PASS' : 'FAIL') + ' | ' + label)
  if (!cond) process.exitCode = 1
}

const suffix = String(Date.now()).slice(-4)
const reg = async (nick, acc) => (await post('/api/v1/auth/register', { nickname: nick, account: acc + '_' + suffix, password: 'test1234' })).data
const a = await reg('全测群主', 'ffa')
const b = await reg('全测成员', 'ffb')
const c = await reg('全测外部', 'ffc')
console.log('注册 a=%d b=%d c=%d', a.user.uid, b.user.uid, c.user.uid)

// ===== L1/L6/L13 会话置顶 + L2 免打扰（单聊） =====
let r = await post('/api/v1/conversations', { conv_id: '0', target_id: b.user.uid, conv_type: 1, type: 1, msg_id: String(Date.now() + 1), content: '置顶测试' }, a.access_token)
ok(r.code === 0, 'A→B 发消息建立会话')
let convs = await get('/api/v1/conversations', a.access_token)
let convA = convs.find((x) => String(x.target_id) === String(b.user.uid))
ok(!!convA, 'A 会话列表含 B 会话')
r = await put(`/api/v1/conversations/${convA.id}/settings`, { pinned: 1 }, a.access_token)
ok(r.code === 0, 'L1 置顶接口调用成功')
convs = await get('/api/v1/conversations', a.access_token)
ok(convs.find((x) => String(x.target_id) === String(b.user.uid)).pinned === true, 'L1/L6 列表回显 pinned=true')
r = await put(`/api/v1/conversations/${convA.id}/settings`, { muted: 1 }, a.access_token)
ok(r.code === 0, 'L2 免打扰接口调用成功')
convs = await get('/api/v1/conversations', a.access_token)
ok(convs.find((x) => String(x.target_id) === String(b.user.uid)).muted === true, 'L2 列表回显 muted=true')
r = await put(`/api/v1/conversations/${convA.id}/settings`, { pinned: 0, muted: 0 }, a.access_token)
ok(r.code === 0, 'L1/L2 取消置顶与免打扰')

// ===== 建群（A 群主 + B 成员 + C 成员） =====
const g = await post('/api/v1/groups', { name: '全测群' + suffix, members: [b.user.uid, c.user.uid] }, a.access_token)
ok(g.code === 0, '建群成功')
const gUid = g.data.g_uid

// ===== G1 @提及 → 通知中心 =====
r = await post('/api/v1/conversations', { conv_id: '0', target_id: gUid, conv_type: 2, type: 1, msg_id: String(Date.now() + 2), content: '@B 你好', extra: JSON.stringify({ mention_uids: [String(b.user.uid)] }) }, a.access_token)
ok(r.code === 0, 'G1 群发 @B 消息成功')
const notifs = await get('/api/v1/notifications', b.access_token)
ok(notifs.some((n) => n.type === 'mention' && String(n.summary).includes('全测群')), 'G1 被 @ 成员收到 mention 通知')

// ===== G4 管理员（仅群主可设；普通成员无权） =====
r = await put(`/api/v1/groups/${gUid}/members/${b.user.uid}/role`, { role: 1 }, a.access_token)
ok(r.code === 0, 'G4 群主设 B 为管理员')
r = await put(`/api/v1/groups/${gUid}/members/${c.user.uid}/role`, { role: 1 }, c.access_token)
ok(r.code !== 0, 'G4 普通成员 C 无权设管理员（拦截）')
let gInfo = await get(`/api/v1/groups/${gUid}`, a.access_token)
ok(gInfo.member_roles[String(b.user.uid)] === 1, 'G4 群详情回显 B 为管理员')

// ===== G3 移除成员（微信规则：管理员仅可移除普通成员；群主可移除任何人） =====
// 先把 C 也设为管理员，验证"管理员不能移除管理员"
let r3 = await put(`/api/v1/groups/${gUid}/members/${c.user.uid}/role`, { role: 1 }, a.access_token)
ok(r3.code === 0, 'G3 前置：A 将 C 也设为管理员')
r3 = await post(`/api/v1/groups/${gUid}/members/${c.user.uid}/kick`, null, b.access_token)
ok(r3.code !== 0, 'G3 管理员 B 不能移除管理员 C（拦截）')
// A 撤销 C 管理员 → C 变普通成员 → 管理员 B 可移除 C
r3 = await put(`/api/v1/groups/${gUid}/members/${c.user.uid}/role`, { role: 2 }, a.access_token)
ok(r3.code === 0, 'G3 前置：A 撤销 C 管理员')
r3 = await post(`/api/v1/groups/${gUid}/members/${c.user.uid}/kick`, null, b.access_token)
ok(r3.code === 0, 'G3 管理员 B 移除普通成员 C 成功')
gInfo = await get(`/api/v1/groups/${gUid}`, a.access_token)
ok(!gInfo.members.includes(c.user.uid), 'G3 C 已不在群成员列表')

// ===== S5 撤回（status=1 + 会话预览回退） =====
r = await post('/api/v1/conversations', { conv_id: '0', target_id: b.user.uid, conv_type: 1, type: 1, msg_id: String(Date.now() + 3), content: '待撤回消息' }, a.access_token)
const mRecall = r.data
r = await post(`/api/v1/conversations/${mRecall.conv_id}/recall`, { msg_id: mRecall.id }, a.access_token)
ok(r.code === 0 && r.data.status === 1, 'S5 撤回成功 status=1')
convs = await get('/api/v1/conversations', a.access_token)
const convAfterRecall = convs.find((x) => String(x.target_id) === String(b.user.uid))
ok(['你撤回了一条消息', '对方撤回了一条消息'].includes(convAfterRecall.last_msg), 'S5 撤回后会话预览回退：' + convAfterRecall.last_msg)

// ===== S1 引用回复（extra.quote 协议 + 历史回显） =====
r = await post('/api/v1/conversations', { conv_id: '0', target_id: b.user.uid, conv_type: 1, type: 1, msg_id: String(Date.now() + 4), content: '引用来源消息' }, a.access_token)
const srcMsg = r.data
const quote = { msg_id: String(srcMsg.id), seq: srcMsg.seq, sender_uid: String(a.user.uid), sender_name: '全测群主', type: 1, content: '引用来源消息' }
r = await post('/api/v1/conversations', { conv_id: '0', target_id: b.user.uid, conv_type: 1, type: 1, msg_id: String(Date.now() + 5), content: '这是引用回复', extra: JSON.stringify({ quote }) }, a.access_token)
ok(r.code === 0, 'S1 发送带 quote 的引用消息成功')
const hist = await get(`/api/v1/conversations/${mRecall.conv_id}/messages?limit=10`, a.access_token)
const quoted = hist.find((m) => String(m.id) === String(r.data.id))
ok(!!quoted && !!quoted.extra && JSON.parse(quoted.extra).quote, 'S1 历史回显 extra.quote（协议闭环）')

// ===== S2 合并转发（type=7，content 为 JSON 摘要） =====
const mergeContent = JSON.stringify({ count: 2, items: [{ sender_name: '全测群主', type: 1, content: 'a' }, { sender_name: '全测成员', type: 1, content: 'b' }] })
r = await post('/api/v1/conversations', { conv_id: '0', target_id: b.user.uid, conv_type: 1, type: 7, msg_id: String(Date.now() + 6), content: mergeContent }, a.access_token)
ok(r.code === 0, 'S2 合并转发发送成功')
convs = await get('/api/v1/conversations', a.access_token)
ok(convs.find((x) => String(x.target_id) === String(b.user.uid)).last_msg === '[合并转发]', 'S2 会话预览为 [合并转发] 占位')

// ===== G5 转让群主（权限 + 角色变化） =====
r = await put(`/api/v1/groups/${gUid}/owner`, { new_owner_uid: c.user.uid }, b.access_token)
ok(r.code !== 0, 'G5 非群主 B 无权转让（拦截）')
r = await put(`/api/v1/groups/${gUid}/owner`, { new_owner_uid: b.user.uid }, a.access_token)
ok(r.code === 0, 'G5 群主 A 转让给 B 成功')
gInfo = await get(`/api/v1/groups/${gUid}`, b.access_token)
ok(gInfo.owner_uid === b.user.uid && gInfo.member_roles[String(b.user.uid)] === 0, 'G5 新群主 B 角色=0')
ok(gInfo.member_roles[String(a.user.uid)] === 2, 'G5 原群主 A 变普通成员')

// ===== G6 群昵称（任何成员可设；群详情回显） =====
r = await put(`/api/v1/groups/${gUid}/members/me/nickname`, { nickname: '群里的小测' }, a.access_token)
ok(r.code === 0, 'G6 A 设置群昵称成功')
gInfo = await get(`/api/v1/groups/${gUid}`, a.access_token)
ok(gInfo.my_nickname === '群里的小测' && gInfo.member_nicknames[String(a.user.uid)] === '群里的小测', 'G6 群详情回显群昵称')

// ===== G11 群公告更新（新群主 B 更新公告） =====
r = await put(`/api/v1/groups/${gUid}`, { name: '全测群' + suffix, announcement: '这是新公告' }, b.access_token)
ok(r.code === 0, 'G11 群主 B 更新公告成功')
gInfo = await get(`/api/v1/groups/${gUid}`, b.access_token)
ok(gInfo.announcement === '这是新公告', 'G11 群详情回显公告')

console.log('--- 综合功能冒烟完成（exitCode=' + process.exitCode + '）---')
