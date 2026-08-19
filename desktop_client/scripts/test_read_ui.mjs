/**
 * 已读未读 UI 测试：用 Edge headless 登录用户 A，打开与 B 的会话并查看已读状态。
 * 核心链路：A 发消息 → Node 模拟 B 收到并回已读回执 → A 界面消息气泡应变「已读」。
 */
import { launchEdge, makeClient, evaluate, sleep } from './cdp_util.mjs'
import fs from 'fs'
import os from 'os'
import path from 'path'

const BASE = 'http://127.0.0.1:8080'
const WS_URL = 'ws://127.0.0.1:8080/ws'
const APP_URL = 'http://localhost:5173/'

async function post(path, body, token) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + path, { method: 'POST', headers: h, body: JSON.stringify(body) })
  const j = await res.json()
  if (j.code !== 0) throw new Error(`API ${path}: ${j.code} ${j.message}`)
  return j.data
}

const suffix = String(Date.now()).slice(-5)
const accA = 'uia_' + suffix
const accB = 'uib_' + suffix
const a = await post('/api/v1/auth/register', { nickname: 'UI测试A', account: accA, password: 'test1234' })
const b = await post('/api/v1/auth/register', { nickname: 'UI测试B', account: accB, password: 'test1234' })
const aUID = a.user.uid
const bUID = b.user.uid
console.log('A uid', aUID, 'B uid', bUID)

// 先用 HTTP 建立 A 与 B 的会话（A 发一条消息），让 A 的会话列表出现 B
const msgId1 = Date.now() + 1
await post('/api/v1/conversations', {
  conv_id: '0', target_id: bUID, conv_type: 1, type: 1,
  msg_id: String(msgId1), content: '第一条建立会话',
}, a.access_token)
await sleep(300)

// 启动 Edge headless 登录 A
const userDataDir = path.join(os.tmpdir(), 'cdp_uiA_' + Date.now())
const { child, targets } = await launchEdge({ port: 9223, userDataDir, url: 'about:blank' })
const page = targets.find((t) => t.type === 'page')
const client = makeClient()
await client.connect(page.webSocketDebuggerUrl)
await client.send('Page.enable')
await client.send('Runtime.enable')
// 打开应用
await client.send('Page.navigate', { url: APP_URL })
await sleep(3000)
const pageReady = await evaluate(client, page, `({ ready: document.readyState, title: document.title, bodyLen: (document.body && document.body.innerHTML.length)||0, hasApp: !!document.querySelector('.app') })`)
console.log('页面加载 =', JSON.stringify(pageReady))

// 注入 A 的 token + me，刷新进入主界面
await evaluate(client, page, `(() => {
  const box = { v: ${JSON.stringify(a.access_token)}, enc: false };
  localStorage.setItem('workchat:token:access', JSON.stringify(box));
  localStorage.setItem('workchat:token:refresh', JSON.stringify(box));
  localStorage.setItem('workchat:token:remember', '0');
  localStorage.setItem('workchat:me', ${JSON.stringify(JSON.stringify(a.user))});
  return 'injected';
})()`)
await client.send('Page.reload')
await sleep(3000)

// 检查是否进入主界面（出现了会话列表容器 / conv-item）
let convCount = await evaluate(client, page, `document.querySelectorAll('.conv-item').length`)
console.log('A 会话数 =', convCount)
// 调试：检查页面状态
const dbg = await evaluate(client, page, `(() => {
  const ls = {};
  for (let i = 0; i < localStorage.length; i++) { const k = localStorage.key(i); ls[k] = localStorage.getItem(k); }
  return { url: location.href, hasWindowFrame: !!document.querySelector('.window-frame'), hasNavRail: !!document.querySelector('.nav-rail, .nav'), loginWin: !!document.querySelector('.login-view'), lsKeys: Object.keys(ls), me: (ls['workchat:me']||'').slice(0,80) };
})()`)
console.log('页面调试 =', JSON.stringify(dbg))
if (convCount === 0) {
  console.log('未进入主界面或没有会话，检查登录态…')
}

// 打印会话列表项文本，定位 B 会话
const convItems = await evaluate(client, page, `(() => {
  return [...document.querySelectorAll('.conv-item')].map((i, idx) => ({ idx, text: i.textContent.replace(/\\s+/g, ' ').trim(), cls: i.className }));
})()`)
console.log('会话列表项 =', JSON.stringify(convItems))

// 点击与 B 的会话（按 B 的 uid 匹配）
const clicked = await evaluate(client, page, `(() => {
  const items = [...document.querySelectorAll('.conv-item')];
  const target = items.find(i => i.textContent.includes(${JSON.stringify(String(bUID))}));
  if (target) { target.click(); return true; }
  return false;
})()`)
console.log('点击 B 会话 =', clicked)
await sleep(1500)
const afterOpen = await evaluate(client, page, `({ hasMsgArea: !!document.querySelector('.messages'), inputCnt: document.querySelectorAll('textarea, input').length, inputs: [...document.querySelectorAll('textarea, input')].map(i=>i.placeholder||'').slice(0,5) })`)
console.log('打开会话后 =', JSON.stringify(afterOpen))

// ---- 先建立 B 的 WS 连接（确保 A 发消息时 B 在线可接收）----
console.log('\n--- 建立 B 的 WS 连接 ---')
const ws = new WebSocket(WS_URL)
await new Promise((res, rej) => { ws.onopen = res; ws.onerror = rej })
ws.send(JSON.stringify({ ver: 1, type: 'auth', seq: 1, body: { token: b.access_token } }))
await sleep(800)
// 发心跳验证鉴权成功（服务端应回 pong）
ws.send(JSON.stringify({ ver: 1, type: 'heartbeat', seq: 2, body: {} }))
await sleep(500)

// 检查 A 页面 WS 连接状态（决定 A 走 WS 还是 HTTP 发送）
const aWsStatus = await evaluate(client, page, `(() => {
  const el = document.querySelector('.ws-status, .conn-status, .status-dot');
  const txt = el ? el.className + '|' + el.textContent : '';
  return txt;
})()`)
console.log('A 页面连接状态元素 =', JSON.stringify(aWsStatus))

// 在输入框输入消息并发送
const sentText = 'UI已读测试消息' + Date.now()
const inputOk = await evaluate(client, page, `(() => {
  const input = document.querySelector('textarea[placeholder="输入消息…"], input[placeholder="输入消息…"]');
  if (!input) return 'no-input';
  const setter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value') ||
                 Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value');
  setter.set.call(input, ${JSON.stringify(sentText)});
  input.dispatchEvent(new Event('input', { bubbles: true }));
  return 'typed';
})()`)
console.log('输入消息 =', inputOk)
await sleep(500)
const sent = await evaluate(client, page, `(() => {
  const btn = document.querySelector('.send-btn');
  if (btn && !btn.disabled) { btn.click(); return 'clicked'; }
  return 'btn-disabled';
})()`)
console.log('点击发送 =', sent)
await sleep(1200)

// 查看发送后消息区中自己发出消息的状态文案
const statusAfterSend = await evaluate(client, page, `(() => {
  const rows = [...document.querySelectorAll('.message-row')];
  const outRows = rows.filter(r => r.classList.contains('out'));
  return outRows.map(r => ({
    text: r.querySelector('.bubble')?.textContent?.trim(),
    status: r.querySelector('.status-text-out')?.textContent?.trim() || ''
  }));
})()`)
console.log('发送后 out 消息状态 =', JSON.stringify(statusAfterSend))

// ---- Node 模拟 B：等 A 发的新消息，回已读回执 ----
console.log('\n--- Node 模拟 B 收到并回已读 ---')
let gotPush = false
let convIdB = ''
let seqB = 0
ws.onmessage = (e) => {
  const f = JSON.parse(e.data)
  console.log('[B WS] <=', f.type, JSON.stringify(f.body || {}).slice(0, 120))
  if (f.type === 'msg.push') {
    gotPush = true
    convIdB = f.body.conv_id
    seqB = f.body.seq
    // 模拟 B 已读：发送已读回执
    ws.send(JSON.stringify({ ver: 1, type: 'read', seq: 2, body: { conv_id: convIdB, seq: seqB } }))
    console.log('B 收到消息 seq=', seqB, ' conv_id=', convIdB, ' → 回已读回执')
  }
}
// 等 B 收到 A 发的消息（UI 里 A 刚发的）
let waited = 0
while (!gotPush && waited < 10000) { await sleep(200); waited += 200 }
if (!gotPush) console.log('B 未在 10s 内收到消息（可能 A 未成功发送）')

// 等 A 界面收到 read.sync 并更新
await sleep(1500)

// 查看 A 界面中自己发出消息的最新状态
const statusAfterRead = await evaluate(client, page, `(() => {
  const rows = [...document.querySelectorAll('.message-row')];
  const outRows = rows.filter(r => r.classList.contains('out'));
  return outRows.map(r => ({
    text: r.querySelector('.bubble')?.textContent?.trim(),
    status: r.querySelector('.status-text-out')?.textContent?.trim() || ''
  }));
})()`)
console.log('收到已读后 out 消息状态 =', JSON.stringify(statusAfterRead))

// 判断结果
const msg = statusAfterRead.find((s) => s.text && s.text.includes('UI已读测试'))
const finalStatus = msg ? msg.status : '未找到消息'
console.log('\n=== UI 测试结果 ===')
console.log('目标消息状态 =', JSON.stringify(finalStatus))
if (finalStatus === '已读') {
  console.log('PASS：A 发消息被 B 已读后，界面显示「已读」')
} else {
  console.log('FAIL：已读状态未正确显示，期望「已读」，实际', JSON.stringify(finalStatus))
}

ws.close()
child.kill()
process.exit(0)
