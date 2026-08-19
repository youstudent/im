/**
 * 验证敏感词过滤不产生重复消息：
 * A 发送含敏感词消息（如"你傻逼"），A 页面应只显示一条（过滤后"你***"），而非两条。
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
  if (j.code !== 0) throw new Error(path + ': ' + j.message)
  return j.data
}

const suffix = String(Date.now()).slice(-4)
const a = await post('/api/v1/auth/register', { nickname: '敏感A', account: 'sena_' + suffix, password: 'test1234' })
const b = await post('/api/v1/auth/register', { nickname: '敏感B', account: 'senb_' + suffix, password: 'test1234' })
const aUID = a.user.uid
const bUID = b.user.uid

// A 发一条普通消息建立会话
await post('/api/v1/conversations', { conv_id: '0', target_id: bUID, conv_type: 1, type: 1, msg_id: String(Date.now() + 1), content: '建立会话' }, a.access_token)

// 启动 A 浏览器
const dirA = path.join(os.tmpdir(), 'cdp_A_' + Date.now())
const { child, targets } = await launchEdge({ port: 9238, userDataDir: dirA, url: 'about:blank' })
const page = targets.find((t) => t.type === 'page')
const client = makeClient()
await client.connect(page.webSocketDebuggerUrl)
await client.send('Page.enable')
await client.send('Runtime.enable')
await client.send('Page.navigate', { url: APP_URL })
await sleep(3000)
await evaluate(client, page, `(() => {
  const box = { v: ${JSON.stringify(a.access_token)}, enc: false };
  localStorage.setItem('workchat:token:access', JSON.stringify(box));
  localStorage.setItem('workchat:token:refresh', JSON.stringify(box));
  localStorage.setItem('workchat:token:remember', '0');
  localStorage.setItem('workchat:me', ${JSON.stringify(JSON.stringify(a.user))});
  return 'ok';
})()`)
await client.send('Page.reload')
await sleep(4000)

// 打开与 B 的会话
await evaluate(client, page, `(() => {
  const items = [...document.querySelectorAll('.conv-item')];
  const t = items.find(i => i.textContent.includes(${JSON.stringify(String(bUID))}));
  if (t) t.click(); return !!t;
})()`)
await sleep(1500)

// 发送含敏感词的消息
const rawText = '你傻逼吗'
await evaluate(client, page, `(() => {
  const input = document.querySelector('textarea[placeholder="输入消息…"], input[placeholder="输入消息…"]');
  if (!input) return 'no-input';
  const setter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value') || Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value');
  setter.set.call(input, ${JSON.stringify(rawText)});
  input.dispatchEvent(new Event('input', { bubbles: true }));
  return 'ok';
})()`)
await sleep(400)
await evaluate(client, page, `(() => { const b=document.querySelector('.send-btn'); if(b&&!b.disabled){b.click();return true;} return false; })()`)
console.log('A 发送含敏感词消息:', rawText)
await sleep(2000)

// 检查 A 界面所有 out 消息（含气泡内容 + 状态）
const outMsgs = await evaluate(client, page, `[...document.querySelectorAll('.message-row.out')].map(r=>({ t:(r.querySelector('.bubble')?.textContent||'').trim(), s:r.querySelector('.status-text-out')?.textContent?.trim()||'' }))`)
console.log('A 界面 out 消息 =', JSON.stringify(outMsgs))

// 判断：含敏感词的消息应只有一条，且内容被过滤为 "你***"
const sensitiveLike = outMsgs.filter((m) => m.t.includes('傻逼') || m.t.includes('***'))
const filteredMsgs = outMsgs.filter((m) => m.t.includes('你'))
console.log('\n=== 结果 ===')
console.log('含 "你" 的消息条数 =', filteredMsgs.length, '（期望 1）')
console.log('显示内容 =', JSON.stringify(filteredMsgs.map((m) => m.t)))

let ok = true
if (filteredMsgs.length !== 1) {
  console.log('FAIL：出现', filteredMsgs.length, '条消息（应为 1 条，重复追加了）')
  ok = false
} else if (filteredMsgs[0].t.includes('傻逼')) {
  console.log('FAIL：消息内容未被过滤，仍显示原始敏感词')
  ok = false
} else if (!filteredMsgs[0].t.includes('***')) {
  console.log('FAIL：消息内容未正确替换为 ***')
  ok = false
} else {
  console.log('PASS：敏感词消息只显示一条，且内容已过滤为', JSON.stringify(filteredMsgs[0].t))
}

child.kill()
process.exit(ok ? 0 : 1)
