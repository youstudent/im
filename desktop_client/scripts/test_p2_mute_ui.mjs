/**
 * 验证 G8 全员禁言时，非管理员成员的输入区按钮置灰：
 *   - textarea 禁用、placeholder 提示
 *   - 表情/图片/文件/语音/发送按钮 disabled
 * 前置：服务端 :8080 已运行；vite dev :5173 已启动。
 * 运行：node scripts/test_p2_mute_ui.mjs
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
  if (j.code !== 0) throw new Error(p + ': ' + j.message)
  return j.data
}
async function put(p, body, token) {
  const h = { 'Content-Type': 'application/json' }
  if (token) h['Authorization'] = 'Bearer ' + token
  const res = await fetch(BASE + p, { method: 'PUT', headers: h, body: JSON.stringify(body) })
  const j = await res.json()
  if (j.code !== 0) throw new Error(p + ': ' + j.message)
  return j.data
}
const ok = (cond, label) => {
  console.log((cond ? 'PASS' : 'FAIL') + ' | ' + label)
  if (!cond) process.exitCode = 1
}

const suffix = String(Date.now()).slice(-4)
const a = await post('/api/v1/auth/register', { nickname: '置灰群主', account: 'mua_' + suffix, password: 'test1234' })
const b = await post('/api/v1/auth/register', { nickname: '置灰成员', account: 'mub_' + suffix, password: 'test1234' })
const grp = await post('/api/v1/groups', { name: '置灰测试群' + suffix, members: [b.user.uid] }, a.access_token)
const gUid = grp.g_uid
console.log('建群 g_uid =', gUid)

// 启动浏览器登录 B
const dirB = path.join(os.tmpdir(), 'cdp_muteB_' + Date.now())
const { child, targets } = await launchEdge({ port: 9260, userDataDir: dirB, url: 'about:blank' })
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
await sleep(4000)

// 在会话列表中点击群会话（按文本匹配群名）
const clicked = await evaluate(client, page, `(() => {
  const items = [...document.querySelectorAll('.conv-item')];
  const it = items.find((i) => i.textContent.includes(${JSON.stringify('置灰测试群' + suffix)}));
  if (!it) return 'no-item';
  it.click();
  return 'ok';
})()`)
ok(clicked === 'ok', '打开群聊会话')
await sleep(2500)

// 初始状态：非禁言，输入可用（注入文本使发送按钮脱离"空消息禁用"，等待 Vue 响应式更新）
await evaluate(client, page, `(() => {
  const ta = document.querySelector('.input-field');
  ta.value = '测试消息';
  ta.dispatchEvent(new Event('input', { bubbles: true }));
  return 'ok';
})()`)
await sleep(400)
let state = await evaluate(client, page, `(() => {
  const ta = document.querySelector('.input-field');
  const tools = [...document.querySelectorAll('.tool-btn')];
  const send = document.querySelector('.send-btn');
  return JSON.stringify({
    taDisabled: ta ? ta.disabled : null,
    toolDisabled: tools.every((t) => t.disabled),
    sendDisabled: send ? send.disabled : null,
  });
})()`)
let s = JSON.parse(state)
ok(s.taDisabled === false && s.toolDisabled === false && s.sendDisabled === false, '初始输入可用（ta=' + s.taDisabled + ' tools=' + s.toolDisabled + ' send=' + s.sendDisabled + '）')

// A 开启全员禁言
await put(`/api/v1/groups/${gUid}/settings`, { mute_all: 1 }, a.access_token)
await sleep(2500) // 等待 WS group.updated 推送与响应式更新

state = await evaluate(client, page, `(() => {
  const ta = document.querySelector('.input-field');
  const tools = [...document.querySelectorAll('.tool-btn')];
  const send = document.querySelector('.send-btn');
  return JSON.stringify({
    taDisabled: ta ? ta.disabled : null,
    toolDisabled: tools.every((t) => t.disabled),
    sendDisabled: send ? send.disabled : null,
    placeholder: ta ? ta.placeholder : '',
  });
})()`)
s = JSON.parse(state)
ok(s.taDisabled === true, '全员禁言后输入框禁用，placeholder=' + s.placeholder)
ok(s.toolDisabled === true, '全员禁言后表情/图片/文件/语音按钮全部置灰（4 个 tool-btn）')
ok(s.sendDisabled === true, '全员禁言后发送按钮置灰')

// A 解除全员禁言
await put(`/api/v1/groups/${gUid}/settings`, { mute_all: 0 }, a.access_token)
await sleep(2500)

await evaluate(client, page, `(() => {
  const ta = document.querySelector('.input-field');
  ta.value = '测试消息';
  ta.dispatchEvent(new Event('input', { bubbles: true }));
  return 'ok';
})()`)
await sleep(400)
state = await evaluate(client, page, `(() => {
  const ta = document.querySelector('.input-field');
  const tools = [...document.querySelectorAll('.tool-btn')];
  const send = document.querySelector('.send-btn');
  return JSON.stringify({
    taDisabled: ta ? ta.disabled : null,
    toolDisabled: tools.every((t) => t.disabled),
    sendDisabled: send ? send.disabled : null,
  });
})()`)
s = JSON.parse(state)
ok(s.taDisabled === false && s.toolDisabled === false && s.sendDisabled === false, '解除禁言后恢复可用（ta=' + s.taDisabled + ' tools=' + s.toolDisabled + ' send=' + s.sendDisabled + '）')

child.kill()
console.log('--- 置灰验证完成（exitCode=' + process.exitCode + '）---')
