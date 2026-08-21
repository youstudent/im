/**
 * 验证：删除会话后，对方好友发消息来，本地自动重建会话（含未读）。
 * 前置：服务端 :8080 已运行；vite dev :5173 已启动。
 * 运行：node scripts/test_delete_conv_recreate.mjs
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
const a = (await post('/api/v1/auth/register', { nickname: '重建A', account: 'rca_' + suffix, password: 'test1234' })).data
const b = (await post('/api/v1/auth/register', { nickname: '重建B', account: 'rcb_' + suffix, password: 'test1234' })).data
console.log('注册 a=%d b=%d', a.user.uid, b.user.uid)

// A 申请加 B，B 同意（建立好友关系 + 双方会话视图）
await post('/api/v1/friends/requests', { to_uid: b.user.uid, message: 'hi' }, a.access_token)
const reqs = await get('/api/v1/friends/requests', b.access_token)
const req = reqs[0]
ok(!!req, 'B 收到好友申请')
await post(`/api/v1/friends/requests/${req.id}/handle`, { accept: true }, b.access_token)
// A 给 B 发一条消息（建立会话）
const m1 = await post('/api/v1/conversations', { conv_id: '0', target_id: b.user.uid, conv_type: 1, type: 1, msg_id: String(Date.now() + 1), content: '第一条消息' }, a.access_token)
ok(m1.code === 0, 'A 发首条消息建立会话，conv_id=' + m1.data.conv_id)

// 启动浏览器登录 B
const dirB = path.join(os.tmpdir(), 'cdp_recreate_' + Date.now())
const { child, targets } = await launchEdge({ port: 9270, userDataDir: dirB, url: 'about:blank' })
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

// 会话列表应出现 A 的会话（附诊断：登录态/会话项数量）
const diag = await evaluate(client, page, `(() => {
  const me = localStorage.getItem('workchat:me');
  const tok = localStorage.getItem('workchat:token:access');
  const items = document.querySelectorAll('.conv-item').length;
  const empty = document.querySelector('.conv-empty, .conv-list-empty, .empty');
  return JSON.stringify({ me: me ? JSON.parse(me).nickname : null, hasTok: !!tok, items, emptyText: empty ? empty.textContent.trim().slice(0, 30) : '' });
})()`)
console.log('DIAG 登录后状态:', diag)
let list = await evaluate(client, page, `[...document.querySelectorAll('.conv-item')].map((i) => i.textContent.replace(/\s+/g, ' ').trim())`)
ok(list.some((t) => t.includes('重建A')), 'B 会话列表出现 A：' + JSON.stringify(list))

// B 右键删除该会话 → 确认弹框 → 确认
const delStep = await evaluate(client, page, `(async () => {
  const item = [...document.querySelectorAll('.conv-item')].find((i) => i.textContent.includes('重建A'));
  if (!item) return 'no-item';
  item.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, clientX: 300, clientY: 300 }));
  await new Promise((r) => setTimeout(r, 300));
  const delBtn = [...document.querySelectorAll('.conv-menu-item')].find((b) => b.textContent.includes('删除会话'));
  if (!delBtn) return 'no-menu';
  delBtn.click();
  await new Promise((r) => setTimeout(r, 300));
  const confirm = document.querySelector('.confirm-modal .confirm-btn.danger');
  if (!confirm) return 'no-confirm-modal';
  confirm.click();
  await new Promise((r) => setTimeout(r, 600));
  return 'ok';
})()`)
ok(delStep === 'ok', '删除会话流程完成（' + delStep + '）')
list = await evaluate(client, page, `[...document.querySelectorAll('.conv-item')].map((i) => i.textContent.replace(/\\s+/g, ' ').trim())`)
ok(!list.some((t) => t.includes('重建A')), '删除后会话从列表消失：' + JSON.stringify(list))

// A 再给 B 发消息（B 已删除会话，服务端重建 B 的视图行，推送帧带 conv_type/target_id）
const m2 = await post('/api/v1/conversations', { conv_id: m1.data.conv_id, target_id: b.user.uid, conv_type: 1, type: 1, msg_id: String(Date.now() + 2), content: '删除后的新消息' }, a.access_token)
ok(m2.code === 0, 'A 发送新消息')
await sleep(3000) // 等待 WS 推送与客户端重建

list = await evaluate(client, page, `[...document.querySelectorAll('.conv-item')].map((i) => i.textContent.replace(/\\s+/g, ' ').trim())`)
ok(list.some((t) => t.includes('重建A')), '收到新消息后会话自动重建：' + JSON.stringify(list))
ok(list.some((t) => t.includes('删除后的新消息')), '会话列表展示新消息预览：' + JSON.stringify(list))

child.kill()
console.log('--- 重建验证完成（exitCode=' + process.exitCode + '）---')
