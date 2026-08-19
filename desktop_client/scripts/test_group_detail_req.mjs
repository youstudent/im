/**
 * 验证消息页会话列表加载时，是否提前请求了 /groups/:gid（群详情）。
 * 期望：仅打开群聊会话时才请求群详情，会话列表加载本身不应请求。
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
async function get(path, token) {
  const res = await fetch(BASE + path, { headers: { Authorization: 'Bearer ' + token } })
  const j = await res.json()
  if (j.code !== 0) throw new Error(path + ': ' + j.message)
  return j.data
}

const suffix = String(Date.now()).slice(-4)
const a = await post('/api/v1/auth/register', { nickname: '群详情A', account: 'gda_' + suffix, password: 'test1234' })
const b = await post('/api/v1/auth/register', { nickname: '群详情B', account: 'gdb_' + suffix, password: 'test1234' })

// A 创建群，邀请 B 加入
const grp = await post('/api/v1/groups', { name: '测试群' + suffix, members: [b.user.uid] }, a.access_token)
const gUid = grp.g_uid
console.log('创建群 g_uid =', gUid)
// B 给 A 发一条单聊消息，建立单聊会话（用于切换测试）
await post('/api/v1/conversations', { conv_id: '0', target_id: a.user.uid, conv_type: 1, type: 1, msg_id: String(Date.now() + 1), content: '单聊测试' }, b.access_token)

// 启动浏览器登录 A
const dirA = path.join(os.tmpdir(), 'cdp_A_' + Date.now())
const { child, targets } = await launchEdge({ port: 9250, userDataDir: dirA, url: 'about:blank' })
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

// 注入 fetch 拦截（记录 /groups/:gid 群详情请求），刷新后新页面自动生效
await client.send('Page.addScriptToEvaluateOnNewDocument', {
  source: `(() => {
    if (!window.__gidReq) window.__gidReq = [];
    const orig = window.fetch;
    window.fetch = function(...args) {
      const url = typeof args[0] === 'string' ? args[0] : (args[0] && args[0].url) || '';
      if (/\\/groups\\/\\d+$/.test(url)) window.__gidReq.push({ url, at: Date.now() });
      return orig.apply(this, args);
    };
  })();`,
})
// 刷新页面，让拦截器在消息页加载时生效
await client.send('Page.reload')
await sleep(4000)

// 当前在消息页（会话列表）。检查会话列表内容与群详情请求
const convList = await evaluate(client, page, `[...document.querySelectorAll('.conv-item')].map(i=>i.textContent.replace(/\\s+/g,' ').trim())`)
console.log('消息页会话列表 =', JSON.stringify(convList))
const gidReqs1 = await evaluate(client, page, `JSON.stringify(window.__gidReq || [])`)
console.log('会话列表加载后群详情请求 =', gidReqs1)

// 打开群聊会话
await evaluate(client, page, `(() => {
  const items = [...document.querySelectorAll('.conv-item')];
  const t = items.find(i => i.textContent.includes(${JSON.stringify(String(gUid))}) || i.textContent.includes('测试群'));
  if (t) t.click(); return !!t;
})()`)
console.log('打开群聊会话（首次）')
await sleep(2000)
const gidReqs2 = await evaluate(client, page, `JSON.stringify(window.__gidReq || [])`)
console.log('首次打开群聊会话后群详情请求 =', gidReqs2)

// 切到单聊会话，再切回群聊会话（验证重复打开是否复用缓存）
await evaluate(client, page, `(() => {
  const items = [...document.querySelectorAll('.conv-item')];
  const t = items.find(i => i.textContent.includes(${JSON.stringify(String(a.user.uid))}));
  if (t) t.click(); return !!t;
})()`)
console.log('切到单聊会话')
await sleep(1000)
// 重新打开群聊会话（应复用缓存，不请求）
await evaluate(client, page, `(() => {
  const items = [...document.querySelectorAll('.conv-item')];
  const t = items.find(i => i.textContent.includes(${JSON.stringify(String(gUid))}) || i.textContent.includes('测试群'));
  if (t) t.click(); return !!t;
})()`)
console.log('再次打开群聊会话（第二次）')
await sleep(2000)
const gidReqs3 = await evaluate(client, page, `JSON.stringify(window.__gidReq || [])`)
console.log('再次打开群聊会话后群详情请求 =', gidReqs3)

const parsed1 = JSON.parse(gidReqs1)
const parsed2 = JSON.parse(gidReqs2)
const parsed3 = JSON.parse(gidReqs3)
console.log('\n=== 结果 ===')
console.log('会话列表加载时群详情请求次数 =', parsed1.length, '（期望 0）')
console.log('首次打开群聊后群详情请求次数 =', parsed2.length, '（应 = 1）')
console.log('再次打开群聊后群详情请求次数 =', parsed3.length, '（期望仍 = 1，复用缓存）')
if (parsed1.length === 0 && parsed2.length === 1 && parsed3.length === 1) {
  console.log('PASS：群详情接口仅在首次打开群聊会话时请求，重复打开复用缓存')
} else {
  console.log('FAIL：群详情请求行为不符合预期')
}

child.kill()
process.exit(0)
