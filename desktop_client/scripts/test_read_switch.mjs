/**
 * 复现 Bug：B 在其他会话时，A 发消息，B 切到 A 会话，A 是否收到已读。
 * 场景：A 发消息给 B，B 先停留在 C 会话，再切到 A 会话。
 */
import { launchEdge, makeClient, evaluate, sleep } from './cdp_util.mjs'
import os from 'os'
import path from 'path'

const BASE = 'http://127.0.0.1:8080'
const APP_URL = 'http://localhost:5173/'

async function post(path, body, token) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + path, { method: 'POST', headers: h, body: JSON.stringify(body) })
  const j = await res.json()
  if (j.code !== 0) throw new Error(`API ${path}: ${j.code} ${j.message}`)
  return j.data
}

async function setupPage(port, userDir, user, token) {
  const { child, targets } = await launchEdge({ port, userDataDir: userDir, url: 'about:blank' })
  const page = targets.find((t) => t.type === 'page')
  const client = makeClient()
  await client.connect(page.webSocketDebuggerUrl)
  await client.send('Page.enable')
  await client.send('Runtime.enable')
  await client.send('Page.navigate', { url: APP_URL })
  await sleep(3000)
  await evaluate(client, page, `(() => {
    const box = { v: ${JSON.stringify(token)}, enc: false };
    localStorage.setItem('workchat:token:access', JSON.stringify(box));
    localStorage.setItem('workchat:token:refresh', JSON.stringify(box));
    localStorage.setItem('workchat:token:remember', '0');
    localStorage.setItem('workchat:me', ${JSON.stringify(JSON.stringify(user))});
    return 'ok';
  })()`)
  await client.send('Page.reload')
  await sleep(3500)
  return { child, client, page }
}

// 注册 A、B、C
const suffix = String(Date.now()).slice(-4)
const a = await post('/api/v1/auth/register', { nickname: '切换A', account: 'swa_' + suffix, password: 'test1234' })
const b = await post('/api/v1/auth/register', { nickname: '切换B', account: 'swb_' + suffix, password: 'test1234' })
const c = await post('/api/v1/auth/register', { nickname: '切换C', account: 'swc_' + suffix, password: 'test1234' })
const aUID = a.user.uid
const bUID = b.user.uid
const cUID = c.user.uid
console.log('A', aUID, 'B', bUID, 'C', cUID)

// C 发 20 条消息给 B，建立 B 的「其他会话」（让 C 会话消息区可滚动）
for (let i = 1; i <= 20; i++) {
  await post('/api/v1/conversations', { conv_id: '0', target_id: bUID, conv_type: 1, type: 1, msg_id: String(Date.now() + i), content: 'C 的消息 ' + i }, c.access_token)
}
// A 发 15 条消息给 B（超过前端 limit=10 的分页阈值）
for (let i = 1; i <= 15; i++) {
  await post('/api/v1/conversations', { conv_id: '0', target_id: bUID, conv_type: 1, type: 1, msg_id: String(Date.now() + 100 + i), content: 'A 发给 B 的消息 ' + i }, a.access_token)
}

// 启动 A、B 两个浏览器
const dirA = path.join(os.tmpdir(), 'cdp_A_' + Date.now())
const dirB = path.join(os.tmpdir(), 'cdp_B_' + Date.now())
const A = await setupPage(9230, dirA, a.user, a.access_token)
const B = await setupPage(9231, dirB, b.user, b.access_token)

// B 的会话列表
const bConvs = await evaluate(B.client, B.page, `[...document.querySelectorAll('.conv-item')].map(i=>({text:i.textContent.replace(/\\s+/g,' ').trim()}))`)
console.log('B 会话列表 =', JSON.stringify(bConvs))

// 在 B 页面注入 WS send 拦截，记录 B 发送的 read 帧
await evaluate(B.client, B.page, `(() => {
  window.__sentFrames = [];
  const orig = WebSocket.prototype.send;
  WebSocket.prototype.send = function(data) {
    try { const f = JSON.parse(data); if (f.type === 'read') window.__sentFrames.push({ conv_id: f.body.conv_id, seq: f.body.seq, at: Date.now() }); } catch {}
    return orig.apply(this, arguments);
  };
  return 'hooked';
})()`)

// B 先打开 C 的会话（停留），确保不在 A 会话
const openC = await evaluate(B.client, B.page, `(() => {
  const items = [...document.querySelectorAll('.conv-item')];
  const t = items.find(i => i.textContent.includes(${JSON.stringify(String(cUID))}));
  if (t) { t.click(); return true; } return false;
})()`)
console.log('B 打开 C 会话 =', openC)
await sleep(1800)
// 模拟 B 在 C 会话浏览历史：把消息区滚动到中间（不在底部）
const scrolled = await evaluate(B.client, B.page, `(() => {
  const el = document.querySelector('.messages');
  if (!el) return 'no-msg-area';
  const max = el.scrollHeight - el.clientHeight;
  el.scrollTop = max * 0.4;
  el.dispatchEvent(new Event('scroll'));
  return { scrollTop: el.scrollTop, max, nearBottom: (el.scrollHeight - el.scrollTop - el.clientHeight) <= 60 };
})()`)
console.log('B 在 C 会话滚动到中间 =', JSON.stringify(scrolled))

// B 停留在 C 会话，A 此时再发一条消息给 B（模拟 A 在 B 停留其他会话期间发消息）
await post('/api/v1/conversations', { conv_id: '0', target_id: bUID, conv_type: 1, type: 1, msg_id: String(Date.now() + 3), content: 'B在其他会话时A发的消息' }, a.access_token)
await sleep(1200)

// B 切换到 A 的会话
const openA = await evaluate(B.client, B.page, `(() => {
  const items = [...document.querySelectorAll('.conv-item')];
  const t = items.find(i => i.textContent.includes(${JSON.stringify(String(aUID))}));
  if (t) { t.click(); return true; } return false;
})()`)
console.log('B 切换到 A 会话 =', openA)
await sleep(1800)

// B 切到 A 会话后，看 B 的消息区内容
const bMsgs = await evaluate(B.client, B.page, `[...document.querySelectorAll('.message-row')].map(r=>({c:r.className,t:(r.querySelector('.bubble')?.textContent||'').trim()}))`)
console.log('B 切到 A 会话后消息 =', JSON.stringify(bMsgs))

// 等待 A 收到 read.sync 并更新 UI
await sleep(1500)
const aStatus = await evaluate(A.client, A.page, `(() => {
  const openAConv = (() => {
    const items = [...document.querySelectorAll('.conv-item')];
    const t = items.find(i => i.textContent.includes(${JSON.stringify(String(bUID))}));
    if (t) t.click();
    return !!t;
  })();
  return openAConv;
})()`)
console.log('A 已打开 B 会话 =', aStatus)
await sleep(1500)
const aMsgs = await evaluate(A.client, A.page, `[...document.querySelectorAll('.message-row.out')].map(r=>({t:(r.querySelector('.bubble')?.textContent||'').trim(), s:r.querySelector('.status-text-out')?.textContent?.trim()||''}))`)
console.log('A 界面自己发出消息状态 =', JSON.stringify(aMsgs))

// 查看 B 发出的 read 帧记录
const sentReads = await evaluate(B.client, B.page, `JSON.stringify(window.__sentFrames || [])`)
console.log('B 发出的 read 帧 =', sentReads)

// 判断：统计 A 界面所有 out 消息中未标已读的
const unreadCount = aMsgs.filter((m) => m.s !== '已读').length
console.log('A 界面 out 消息总数 =', aMsgs.length, ' 未标已读数 =', unreadCount)
console.log('\n=== 结果 ===')
if (unreadCount === 0 && aMsgs.length > 0) {
  console.log('PASS：B 从其他会话切到 A 会话后，A 全部消息已读')
} else {
  console.log('FAIL：A 有', unreadCount, '条消息未收到已读')
}

A.child.kill()
B.child.kill()
process.exit(0)
