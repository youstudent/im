/**
 * 验证：群成员退群后，群内其他成员的群成员信息（成员数/成员列表）实时更新。
 * 流程：A 建群邀请 B、C；B 在线打开群聊（成员数 3）；C 退群（API）；B 端成员数自动变 2。
 * 前置：服务端 :8080 已运行；vite dev :5173 已启动。
 * 运行：node scripts/test_group_member_left.mjs
 */
import { launchEdge, makeClient, evaluate, sleep } from './cdp_util.mjs'
import os from 'os'
import path from 'path'

const BASE = 'http://127.0.0.1:8080'
const APP_URL = 'http://localhost:5173/'
async function post(p, body, token) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + p, { method: 'POST', headers: h, body: JSON.stringify(body) })
  const j = await res.json()
  return { code: j.code, data: j.data, message: j.message }
}
async function del(p, token) {
  const res = await fetch(BASE + p, { method: 'DELETE', headers: { Authorization: 'Bearer ' + token } })
  const j = await res.json()
  return { code: j.code, data: j.data, message: j.message }
}
const ok = (cond, label) => {
  console.log((cond ? 'PASS' : 'FAIL') + ' | ' + label)
  if (!cond) process.exitCode = 1
}

const suffix = String(Date.now()).slice(-4)
const reg = async (nick, acc) => (await post('/api/v1/auth/register', { nickname: nick, account: acc + '_' + suffix, password: 'test1234' })).data
const a = await reg('退群A', 'gla')
const b = await reg('退群B', 'glb')
const c = await reg('退群C', 'glc')
console.log('注册 a=%d b=%d c=%d', a.user.uid, b.user.uid, c.user.uid)
await post('/api/v1/groups', { name: '退群测试' + suffix, members: [b.user.uid, c.user.uid] }, a.access_token)

// 启动浏览器登录 B
const dirB = path.join(os.tmpdir(), 'cdp_gl_' + Date.now())
const { child, targets } = await launchEdge({ port: 9290, userDataDir: dirB, url: 'about:blank' })
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

// B 打开群聊会话（成员数 3：A/B/C）
const clicked = await evaluate(client, page, `(() => {
  const items = [...document.querySelectorAll('.conv-item')];
  const it = items.find((i) => i.textContent.includes(${JSON.stringify('退群测试' + suffix)}));
  if (!it) return 'no-item';
  it.click();
  return 'ok';
})()`)
ok(clicked === 'ok', 'B 打开群聊会话')
await sleep(2500)

// 读取聊天头部成员数（"群名 (N)" 或群资料成员数）
const memberText = () => evaluate(client, page, `(() => {
  const head = document.querySelector('.contact-name');
  const panelCount = document.querySelector('.member-section .section-title, .group-pill');
  return JSON.stringify({ head: head ? head.textContent.replace(/\\s+/g, ' ').trim() : '', panel: panelCount ? panelCount.textContent.replace(/\\s+/g, ' ').trim() : '' });
})()`)
let m1 = JSON.parse(await memberText())
console.log('退群前 B 端成员信息 =', JSON.stringify(m1))
ok(/退群测试.*\\(3\\)/.test(m1.head) || (m1.panel && m1.panel.includes('3')), 'B 端显示 3 位成员：' + m1.head + ' / ' + m1.panel)

// C 退群（API）
const r = await del('/api/v1/groups/' + (await (async () => { const gs = await (await fetch(BASE + '/api/v1/groups', { headers: { Authorization: 'Bearer ' + a.access_token } })).json(); return gs.data.find((g) => g.name.includes('退群测试')).g_uid })()) + '/members/me', c.access_token)
ok(r.code === 0, 'C 退群成功')
await sleep(3000) // 等待 group.member_left 推送与 B 端重载

const m2 = JSON.parse(await memberText())
console.log('退群后 B 端成员信息 =', JSON.stringify(m2))
ok(/退群测试.*\\(2\\)/.test(m2.head) || (m2.panel && m2.panel.includes('2')), 'B 端成员数更新为 2：' + m2.head + ' / ' + m2.panel)

child.kill()
console.log('--- 退群成员同步验证完成（exitCode=' + process.exitCode + '）---')
