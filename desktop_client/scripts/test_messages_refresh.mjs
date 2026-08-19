/**
 * 验证：消息页刷新浏览器不再请求 /friends 和 /groups。
 * 流程：登录 A → 进入通讯录页（填充缓存）→ 回消息页 → 注入拦截 → 刷新浏览器 → 统计 friends/groups 请求次数。
 * 期望：刷新后 friends/groups 请求均为 0（缓存已持久化到 localStorage）。
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
const a = await post('/api/v1/auth/register', { nickname: '刷新消息A', account: 'mra_' + suffix, password: 'test1234' })

// 启动浏览器
const dirA = path.join(os.tmpdir(), 'cdp_A_' + Date.now())
const { child, targets } = await launchEdge({ port: 9245, userDataDir: dirA, url: 'about:blank' })
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

// 进入通讯录页，填充 friends/groups 缓存到 localStorage
await evaluate(client, page, `(() => { const btns=[...document.querySelectorAll('.nav-btn')]; if(btns[1])btns[1].click(); return 'ok'; })()`)
await sleep(2500)
// 确认通讯录页加载并检查 localStorage 缓存
const cacheAfterContacts = await evaluate(client, page, `({
  friendsCache: localStorage.getItem('workchat:friends:cache'),
  groupsCache: localStorage.getItem('workchat:groups:cache')
})`)
console.log('进入通讯录后 localStorage 缓存 =', JSON.stringify(cacheAfterContacts))

// 切回消息页
await evaluate(client, page, `(() => { const btns=[...document.querySelectorAll('.nav-btn')]; if(btns[0])btns[0].click(); return 'ok'; })()`)
await sleep(1500)

// 注入 fetch 拦截（用 Page.addScriptToEvaluateOnNewDocument，刷新后新页面自动注入）
await client.send('Page.addScriptToEvaluateOnNewDocument', {
  source: `(() => {
    if (!window.__apiCalls) window.__apiCalls = { friends: 0, groups: 0 };
    const orig = window.fetch;
    window.fetch = function(...args) {
      const url = typeof args[0] === 'string' ? args[0] : (args[0] && args[0].url) || '';
      if (url.includes('/friends')) window.__apiCalls.friends++;
      if (url.includes('/groups')) window.__apiCalls.groups++;
      return orig.apply(this, args);
    };
  })();`,
})
console.log('已注入 fetch 拦截（onNewDocument）')

// 刷新浏览器（此时在消息页）
console.log('刷新浏览器（消息页）...')
await client.send('Page.reload')
await sleep(5000)

// 读取刷新后的请求统计（拦截器由 onNewDocument 自动注入）
const calls = await evaluate(client, page, `JSON.stringify(window.__apiCalls || { friends: -1, groups: -1 })`)
console.log('刷新后 friends/groups 请求 =', calls)

const parsed = JSON.parse(calls)
console.log('\n=== 结果 ===')
if (parsed.friends === 0 && parsed.groups === 0) {
  console.log('PASS：消息页刷新不再请求 /friends 和 /groups')
} else {
  console.log('FAIL：消息页刷新仍请求了 friends(' + parsed.friends + ') groups(' + parsed.groups + ')')
}

child.kill()
process.exit(0)
