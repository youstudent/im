/** 验证管理后台用户/群组管理搜索 UI。 */
import { launchEdge, makeClient, evaluate, sleep } from './cdp_util.mjs'
import os from 'os'
import path from 'path'

const APP_URL = 'http://localhost:5174/'

const dir = path.join(os.tmpdir(), 'cdp_admin_' + Date.now())
const { child, targets } = await launchEdge({ port: 9258, userDataDir: dir, url: 'about:blank' })
const page = targets.find((t) => t.type === 'page')
const client = makeClient()
await client.connect(page.webSocketDebuggerUrl)
await client.send('Page.enable')
await client.send('Runtime.enable')
await client.send('Page.navigate', { url: APP_URL })
await sleep(3000)

// 登录
await evaluate(client, page, `(() => {
  const inputs = document.querySelectorAll('input');
  if (!inputs[0]) return 'no-input';
  inputs[0].value = 'admin';
  inputs[0].dispatchEvent(new Event('input', { bubbles: true }));
  if (inputs[1]) { inputs[1].value = 'admin123'; inputs[1].dispatchEvent(new Event('input', { bubbles: true })); }
  const btn = document.querySelector('button');
  if (btn) btn.click();
  return 'login';
})()`)
await sleep(2000)

// 进入用户管理
await evaluate(client, page, `(() => {
  const items = [...document.querySelectorAll('.nav-item')];
  const t = items.find(i => (i.textContent||'').includes('用户管理'));
  if (t) t.click(); return !!t;
})()`)
await sleep(1500)

// 检查用户管理搜索框是否存在
const userToolbar = await evaluate(client, page, `(() => {
  const input = document.querySelector('.search-input');
  return { hasInput: !!input, placeholder: input && input.placeholder };
})()`)
console.log('用户管理搜索框 =', JSON.stringify(userToolbar))

// 输入关键词搜索
await evaluate(client, page, `(() => {
  const input = document.querySelector('.search-input');
  if (!input) return 'no-input';
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value');
  setter.set.call(input, 'sena');
  input.dispatchEvent(new Event('input', { bubbles: true }));
  return 'typed';
})()`)
await sleep(200)
await evaluate(client, page, `(() => { const btns=[...document.querySelectorAll('.search-box button')]; const b=btns.find(x=>x.textContent.trim()==='搜索'); if(b){b.click();return true;} return false; })()`)
await sleep(1500)
const userResult = await evaluate(client, page, `(() => {
  const rows = [...document.querySelectorAll('tbody tr')].map(r => r.textContent.replace(/\\s+/g,' ').trim()).filter(t => t && !t.includes('暂无数据'));
  const count = document.querySelector('.count')?.textContent?.trim() || '';
  return { count, rows: rows.slice(0, 5) };
})()`)
console.log('用户搜索 sena 结果 =', JSON.stringify(userResult))

// 进入群组管理，测试搜索
await evaluate(client, page, `(() => {
  const items = [...document.querySelectorAll('.nav-item')];
  const t = items.find(i => (i.textContent||'').includes('群组管理'));
  if (t) t.click(); return !!t;
})()`)
await sleep(1200)
await evaluate(client, page, `(() => {
  const input = document.querySelector('.search-input');
  if (!input) return 'no-input';
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value');
  setter.set.call(input, '测试群');
  input.dispatchEvent(new Event('input', { bubbles: true }));
  return 'typed';
})()`)
await sleep(200)
await evaluate(client, page, `(() => { const btns=[...document.querySelectorAll('.search-box button')]; const b=btns.find(x=>x.textContent.trim()==='搜索'); if(b){b.click();return true;} return false; })()`)
await sleep(1500)
const groupResult = await evaluate(client, page, `(() => {
  const rows = [...document.querySelectorAll('tbody tr')].map(r => r.textContent.replace(/\\s+/g,' ').trim()).filter(t => t && !t.includes('暂无数据'));
  const count = document.querySelector('.count')?.textContent?.trim() || '';
  return { count, rows: rows.slice(0, 5) };
})()`)
console.log('群搜索 测试群 结果 =', JSON.stringify(groupResult))

// 判断
const userRows = userResult.rows
const groupRows = groupResult.rows
const ok = userToolbar.hasInput && userRows.length > 0 && userRows.every(r => r.includes('sena') || r.includes('sena')) && groupRows.length > 0
console.log('\n=== 结果 ===')
console.log('用户管理搜索框存在:', userToolbar.hasInput)
console.log('用户搜索结果条数:', userRows.length, '（应>0 且都是匹配项）')
console.log('群搜索结果条数:', groupRows.length, '（应>0）')
if (ok) console.log('PASS：用户/群组管理搜索功能正常')
else console.log('FAIL：搜索功能异常')

child.kill()
process.exit(0)
