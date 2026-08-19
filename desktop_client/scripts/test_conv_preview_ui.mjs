/** 验证前端会话列表：最后一条消息为图片时显示 [图片]，而非 URL。 */
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
const a = await post('/api/v1/auth/register', { nickname: '预览UI_A', account: 'pua_' + suffix, password: 'test1234' })
const b = await post('/api/v1/auth/register', { nickname: '预览UI_B', account: 'pub_' + suffix, password: 'test1234' })
const aUID = a.user.uid
const bUID = b.user.uid

// A 发一条文本，再发一条图片消息（content 为 URL）
await post('/api/v1/conversations', { conv_id: '0', target_id: bUID, conv_type: 1, type: 1, msg_id: String(Date.now() + 1), content: '一条文本' }, a.access_token)
await post('/api/v1/conversations', { conv_id: '0', target_id: bUID, conv_type: 1, type: 2, msg_id: String(Date.now() + 2), content: 'http://oss.example.com/img/x.png' }, a.access_token)

const dirA = path.join(os.tmpdir(), 'cdp_A_' + Date.now())
const { child, targets } = await launchEdge({ port: 9260, userDataDir: dirA, url: 'about:blank' })
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

// 检查会话列表项文本：最后一条消息应为 [图片]
const items = await evaluate(client, page, `[...document.querySelectorAll('.conv-item')].map(i=>i.textContent.replace(/\\s+/g,' ').trim())`)
console.log('会话列表 =', JSON.stringify(items))

const target = items.find((t) => t.includes(String(bUID)))
console.log('\n=== 结果 ===')
if (target) {
  const hasUrl = target.includes('http://')
  const hasPic = target.includes('[图片]')
  console.log('会话项含 URL =', hasUrl, ' 含 [图片] =', hasPic)
  if (hasPic && !hasUrl) console.log('PASS：会话列表显示 [图片]，不显示图片 URL')
  else console.log('FAIL：会话列表预览不正确')
} else {
  console.log('FAIL：未找到目标会话')
}

child.kill()
process.exit(0)
