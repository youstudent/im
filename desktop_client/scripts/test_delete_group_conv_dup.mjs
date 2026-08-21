/**
 * 验证：成员删除群会话后再次发言，conv_id 不分叉，其他成员会话列表不会重复建立群会话。
 * 背景 bug：发送者删除群会话视图后重发消息，服务端曾以新 conv_id 重建其视图（分叉），
 * 消息落入割裂会话，其他成员收到的推送帧 conv_id 无法映射到已有会话，本地重复建第二个群会话。
 * 前置：服务端 :8080 已运行；vite dev :5173 已启动。
 * 运行：node scripts/test_delete_group_conv_dup.mjs
 */
import { launchEdge, makeClient, evaluate, sleep } from './cdp_util.mjs'
import os from 'os'
import path from 'path'

const BASE = 'http://127.0.0.1:8080'
const APP_URL = 'http://localhost:5173/'
async function req(method, p, body, token) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + p, { method, headers: h, body: body ? JSON.stringify(body) : undefined })
  const j = await res.json()
  return { code: j.code, data: j.data, message: j.message }
}
const post = (p, body, token) => req('POST', p, body, token)
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
const a = (await post('/api/v1/auth/register', { nickname: '群主A', account: 'gdpa_' + suffix, password: 'test1234' })).data
const b = (await post('/api/v1/auth/register', { nickname: '成员B', account: 'gdpb_' + suffix, password: 'test1234' })).data
console.log('注册 a=%d b=%d', a.user.uid, b.user.uid)

// A 建群并拉 B 入群（建群系统消息会为全体成员建立会话视图）
const grpName = '防重群' + suffix
const grp = (await post('/api/v1/groups', { name: grpName, members: [b.user.uid] }, a.access_token)).data
ok(!!grp && !!grp.g_uid, '建群成功 g_uid=' + (grp && grp.g_uid))
const gUid = Number(grp.g_uid)

// A 发第一条群消息，记下群统一 conv_id
const m1 = await post('/api/v1/conversations', { conv_id: '0', target_id: gUid, conv_type: 2, type: 1, msg_id: String(Date.now() + 1), content: '法警' }, a.access_token)
ok(m1.code === 0, 'A 发首条群消息，conv_id=' + m1.data.conv_id)
const canonical = String(m1.data.conv_id)

// 启动浏览器登录 B
const dirB = path.join(os.tmpdir(), 'cdp_grpdup_' + Date.now())
const { child, targets } = await launchEdge({ port: 9271, userDataDir: dirB, url: 'about:blank' })
const page = targets.find((t) => t.type === 'page')
const client = makeClient()
await client.connect(page.webSocketDebuggerUrl)
await client.send('Page.enable')
await client.send('Runtime.enable')
await client.send('Page.navigate', { url: APP_URL })
await sleep(3000)
await evaluate(client, page, `(() => {
  const box = { v: ${JSON.stringify(b.access_token)}, enc: false };
  localStorage.setItem('workchat:token:access', JSON.stringify(box));
  localStorage.setItem('workchat:token:refresh', JSON.stringify(box));
  localStorage.setItem('workchat:token:remember', '0');
  localStorage.setItem('workchat:me', ${JSON.stringify(JSON.stringify(b.user))});
  return 'ok';
})()`)
await client.send('Page.reload')
await sleep(5000)

const convItems = () => evaluate(client, page, `[...document.querySelectorAll('.conv-item')].map((i) => i.textContent.replace(/\\s+/g, ' ').trim())`)
let list = await convItems()
ok(list.filter((t) => t.includes(grpName)).length === 1, 'B 会话列表恰好 1 个群会话：' + JSON.stringify(list))

// A 删除自己的群会话视图（模拟桌面端"删除会话"）
const del = await req('DELETE', `/api/v1/conversations/${canonical}`, null, a.access_token)
ok(del.code === 0, 'A 删除自己的群会话视图')

// A 删除后再次发言：服务端必须沿用群统一 conv_id（分叉即本 bug）
const m2 = await post('/api/v1/conversations', { conv_id: '0', target_id: gUid, conv_type: 2, type: 1, msg_id: String(Date.now() + 2), content: '又发送消息' }, a.access_token)
ok(m2.code === 0, 'A 删除后再次发言')
ok(String(m2.data.conv_id) === canonical, `重建未分叉 conv_id（got=${m2.data.conv_id} want=${canonical}）`)

// 服务端视角：B 的会话列表中该群仅一行
const bConvs = await get('/api/v1/conversations', b.access_token)
const rows = (bConvs || []).filter((c) => Number(c.target_id) === gUid)
ok(rows.length === 1, 'B 服务端会话列表该群仅 1 行（got=' + rows.length + '）')
ok(rows.length === 1 && String(rows[0].id) === canonical, 'B 的群会话行使用统一 conv_id')

// 客户端视角：等待 WS 推送后，B 的列表仍只有 1 个该群会话（不重复建立）且预览更新
await sleep(3000)
list = await convItems()
const dup = list.filter((t) => t.includes(grpName))
ok(dup.length === 1, 'B 客户端列表仍恰好 1 个群会话（无重复）：' + JSON.stringify(list))
ok(dup.some((t) => t.includes('又发送消息')), '群会话预览更新为新消息：' + JSON.stringify(dup))

child.kill()
console.log('--- 群会话防重复验证完成（exitCode=' + process.exitCode + '）---')
