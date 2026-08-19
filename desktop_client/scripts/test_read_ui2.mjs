/**
 * 已读未读 UI 测试（双浏览器版）：Edge1 登录 A，Edge2 登录 B。
 * 链路：A 打开会话发消息 → B 打开会话自动回已读 → A 界面消息应变「已读」。
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
  // 注入 token + me，刷新
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

// 注册 A、B
const suffix = String(Date.now()).slice(-4)
const a = await post('/api/v1/auth/register', { nickname: '双端A', account: 'dua_' + suffix, password: 'test1234' })
const b = await post('/api/v1/auth/register', { nickname: '双端B', account: 'dub_' + suffix, password: 'test1234' })
const aUID = a.user.uid
const bUID = b.user.uid
console.log('A uid', aUID, 'B uid', bUID)

// 建立 A、B 的会话（A 发一条）
await post('/api/v1/conversations', {
  conv_id: '0', target_id: bUID, conv_type: 1, type: 1,
  msg_id: String(Date.now() + 1), content: '建立会话',
}, a.access_token)

// 启动 A、B 两个浏览器
const dirA = path.join(os.tmpdir(), 'cdp_A_' + Date.now())
const dirB = path.join(os.tmpdir(), 'cdp_B_' + Date.now())
const A = await setupPage(9224, dirA, a.user, a.access_token)
const B = await setupPage(9225, dirB, b.user, b.access_token)

// 打开 A 的 B 会话
const convCountA = await evaluate(A.client, A.page, `document.querySelectorAll('.conv-item').length`)
console.log('A 会话数 =', convCountA)
await evaluate(A.client, A.page, `(() => {
  const items = [...document.querySelectorAll('.conv-item')];
  const t = items.find(i => i.textContent.includes(${JSON.stringify(String(bUID))}));
  if (t) t.click();
  return !!t;
})()`)
await sleep(1500)

// A 发一条消息
const sentText = '双端已读测试' + Date.now()
await evaluate(A.client, A.page, `(() => {
  const input = document.querySelector('textarea[placeholder="输入消息…"], input[placeholder="输入消息…"]');
  if (!input) return 'no-input';
  const setter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value') || Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value');
  setter.set.call(input, ${JSON.stringify(sentText)});
  input.dispatchEvent(new Event('input', { bubbles: true }));
  return 'ok';
})()`)
await sleep(400)
await evaluate(A.client, A.page, `(() => { const b=document.querySelector('.send-btn'); if(b&&!b.disabled){b.click();return true;} return false; })()`)
console.log('A 已发送消息:', sentText)
await sleep(1500)

// A 界面当前 out 消息状态
const statusA1 = await evaluate(A.client, A.page, `[...document.querySelectorAll('.message-row.out')].map(r=>({t:(r.querySelector('.bubble')?.textContent||'').trim(), s:r.querySelector('.status-text-out')?.textContent?.trim()||''}))`)
console.log('A 发送后消息状态 =', JSON.stringify(statusA1))

// 检查 B 是否收到消息（B 界面应出现 A 发来的 in 消息）
const convCountB = await evaluate(B.client, B.page, `document.querySelectorAll('.conv-item').length`)
console.log('B 会话数 =', convCountB)
await evaluate(B.client, B.page, `(() => {
  const items = [...document.querySelectorAll('.conv-item')];
  const t = items.find(i => i.textContent.includes(${JSON.stringify(String(aUID))}));
  if (t) t.click();
  return !!t;
})()`)
console.log('B 已打开与 A 的会话')
await sleep(1800)
const statusB = await evaluate(B.client, B.page, `[...document.querySelectorAll('.message-row')].map(r=>({c:r.className, t:(r.querySelector('.bubble')?.textContent||'').trim()}))`)
console.log('B 会话消息 =', JSON.stringify(statusB))

// B 打开会话已触发已读回执（switchConversation 里 isNearBottom 时 sendReadReceipt）
// 等待 A 收到 read.sync 并更新 UI
await sleep(1500)
const statusA2 = await evaluate(A.client, A.page, `[...document.querySelectorAll('.message-row.out')].map(r=>({t:(r.querySelector('.bubble')?.textContent||'').trim(), s:r.querySelector('.status-text-out')?.textContent?.trim()||''}))`)
console.log('B 已读后 A 消息状态 =', JSON.stringify(statusA2))

// 判断结果
const msg = statusA2.find((m) => m.t && m.t.includes('双端已读测试'))
const finalStatus = msg ? msg.s : '未找到消息'
console.log('\n=== 双端 UI 测试结果 ===')
console.log('A 发的消息最终状态 =', JSON.stringify(finalStatus))
if (finalStatus === '已读') {
  console.log('PASS：B 打开会话已读后，A 界面显示「已读」')
} else {
  console.log('FAIL：期望「已读」，实际', JSON.stringify(finalStatus))
}

A.child.kill()
B.child.kill()
process.exit(0)
