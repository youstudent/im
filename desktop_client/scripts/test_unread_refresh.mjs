/**
 * 验证刷新后未读气泡不丢失：
 * A 发消息给 B → B 打开应用（有未读气泡）→ 刷新浏览器 → 未读气泡仍显示。
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
const a = await post('/api/v1/auth/register', { nickname: '刷新A', account: 'refa_' + suffix, password: 'test1234' })
const b = await post('/api/v1/auth/register', { nickname: '刷新B', account: 'refb_' + suffix, password: 'test1234' })
const aUID = a.user.uid
const bUID = b.user.uid

// A 发 5 条消息给 B（B 未读）
for (let i = 1; i <= 5; i++) {
  await post('/api/v1/conversations', { conv_id: '0', target_id: bUID, conv_type: 1, type: 1, msg_id: String(Date.now() + i), content: '刷新测试消息 ' + i }, a.access_token)
}

// 启动 B 浏览器
const dirB = path.join(os.tmpdir(), 'cdp_B_' + Date.now())
const { child, targets } = await launchEdge({ port: 9235, userDataDir: dirB, url: 'about:blank' })
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

// 首次加载：检查未读气泡
const getBadges = `[...document.querySelectorAll('.conv-item')].map(i=>({ text: i.textContent.replace(/\\s+/g,' ').trim(), badge: i.querySelector('.unread-badge')?.textContent?.trim()||null }))`
let badges = await evaluate(client, page, getBadges)
console.log('首次加载 B 会话 =', JSON.stringify(badges))

// 刷新浏览器
await client.send('Page.reload')
await sleep(4000)
badges = await evaluate(client, page, getBadges)
console.log('刷新后 B 会话 =', JSON.stringify(badges))

// 判断：刷新后与 A 的会话是否还有未读气泡
const aConv = badges.find((c) => c.text.includes(String(aUID)))
const afterRefresh = aConv ? aConv.badge : null
console.log('\n=== 结果 ===')
console.log('刷新后与 A 会话的未读气泡 =', JSON.stringify(afterRefresh))
if (afterRefresh && afterRefresh === '5') {
  console.log('PASS：刷新后未读气泡仍显示 5')
} else {
  console.log('FAIL：刷新后未读气泡丢失（期望 5，实际', JSON.stringify(afterRefresh), '）')
}

child.kill()
process.exit(0)
