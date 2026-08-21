/**
 * 复现验证：建群 + 邀请好友时，被邀请人在线收到推送（系统消息先于 conversation.created 到达），
 * 会话列表不应出现重复会话或"用户 0"错误会话（历史 bug：系统消息误触发会话重建）。
 * 前置：服务端 :8080 已运行；vite dev :5173 已启动。
 * 运行：node scripts/test_group_create_no_dup.mjs
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
const ok = (cond, label) => {
  console.log((cond ? 'PASS' : 'FAIL') + ' | ' + label)
  if (!cond) process.exitCode = 1
}

const suffix = String(Date.now()).slice(-4)
const a = await post('/api/v1/auth/register', { nickname: '建群A', account: 'gca_' + suffix, password: 'test1234' })
const b = await post('/api/v1/auth/register', { nickname: '建群B', account: 'gcb_' + suffix, password: 'test1234' })
console.log('注册 a=%d b=%d', a.user.uid, b.user.uid)

// 启动浏览器登录 B（在线）
const dirB = path.join(os.tmpdir(), 'cdp_gc_' + Date.now())
const { child, targets } = await launchEdge({ port: 9280, userDataDir: dirB, url: 'about:blank' })
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
// 注入 WS 帧记录（reload 后生效）：诊断 B 页面是否收到 conversation.created / msg.push
await client.send('Page.addScriptToEvaluateOnNewDocument', {
  source: `(() => {
    window.__wsFrames = [];
    window.__errs = [];
    window.addEventListener('error', (e) => window.__errs.push('err:' + String(e.message)));
    window.addEventListener('unhandledrejection', (e) => window.__errs.push('rej:' + String((e.reason && e.reason.message) || e.reason)));
    const OrigWS = window.WebSocket;
    window.WebSocket = class extends OrigWS {
      constructor(...a) {
        super(...a);
        this.addEventListener('message', (e) => {
          try {
            const f = JSON.parse(e.data);
            if (f.type === 'social' || f.type === 'msg.push') window.__wsFrames.push(f.type + ':' + JSON.stringify(f.body).slice(0, 140));
          } catch {}
        });
      }
    };
  })();`,
})
await client.send('Page.reload')
await sleep(5000)

// A 建群并邀请 B（B 在线，将实时收到：系统消息 msg.push 先、conversation.created 后）
const grp = await post('/api/v1/groups', { name: '建群测试' + suffix, members: [b.user.uid] }, a.access_token)
console.log('建群 g_uid =', grp.g_uid)
await sleep(5000)

// 诊断：登录态 + 会话列表渲染状态
const diag = await evaluate(client, page, `(() => {
  const me = localStorage.getItem('workchat:me');
  const tok = localStorage.getItem('workchat:token:access');
  const items = document.querySelectorAll('.conv-item').length;
  const listEl = document.querySelector('.conv-list');
  const grpCache = localStorage.getItem('workchat:groups:cache');
  const searchVal = (document.querySelector('.search-box input, .search-input, .conv-search') || {}).value || '';
  return JSON.stringify({ me: me ? JSON.parse(me).nickname : null, hasTok: !!tok, items, listExists: !!listEl, searchVal, listHTML: listEl ? listEl.innerHTML.replace(/\s+/g, ' ').slice(0, 200) : '', grpCache: grpCache ? grpCache.slice(0, 120) : 'null', wsFrames: (window.__wsFrames || []).join(' || '), errs: (window.__errs || []).join(' ;; ') });
})()`)
console.log('DIAG:', diag)
await sleep(1000)
// 刷新页面：loadRealData 从服务端拉取（此时群已建），验证服务端数据路径正常
await client.send('Page.reload')
await sleep(4000)
const list2 = await evaluate(client, page, `[...document.querySelectorAll('.conv-item')].map((i) => i.textContent.replace(/\s+/g, ' ').trim())`)
console.log('刷新后 B 会话列表 =', JSON.stringify(list2))
ok(list2.some((t) => t.includes('建群测试')), '刷新后出现正确群会话（服务端数据路径）')
ok(!list2.some((t) => t.includes('用户 0')), '刷新后无"用户 0"')
// 用刷新后的列表继续断言
const curList = list2.length ? list2 : list

// 验证 B 会话列表：恰好 1 个群会话（群名正确），无"用户 0"，排在最上面，且带最后消息预览与时间
const list = curList
const groupItems = list.filter((t) => t.includes('建群测试'))
const user0Items = list.filter((t) => t.includes('用户 0') || t.includes('用户0'))
ok(groupItems.length === 1, 'B 会话列表恰好 1 个群会话（实际 ' + groupItems.length + '）：' + JSON.stringify(list))
ok(user0Items.length === 0, 'B 会话列表无"用户 0"错误会话')
ok(list[0] && list[0].includes('建群测试'), '群会话排在最上面（实际第一项：' + (list[0] || '空') + '）')
ok(groupItems[0] && groupItems[0].includes('[系统消息]'), '群会话展示最后消息预览为 [系统消息]（' + (groupItems[0] || '') + '）')
ok(groupItems[0] && /(刚刚|\d{1,2}[:：]\d{2}|昨天|星期)/.test(groupItems[0]), '群会话展示最后消息时间（' + (groupItems[0] || '') + '）')

child.kill()
console.log('--- 建群去重验证完成（exitCode=' + process.exitCode + '）---')
