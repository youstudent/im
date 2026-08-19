/** 验证管理后台数据看板：柱状图是否渲染、是否显示趋势数据。 */
import { launchEdge, makeClient, evaluate, sleep } from './cdp_util.mjs'
import os from 'os'
import path from 'path'

const APP_URL = 'http://localhost:5174/'

const dir = path.join(os.tmpdir(), 'cdp_admin_' + Date.now())
const { child, targets } = await launchEdge({ port: 9255, userDataDir: dir, url: 'about:blank' })
const page = targets.find((t) => t.type === 'page')
const client = makeClient()
await client.connect(page.webSocketDebuggerUrl)
await client.send('Page.enable')
await client.send('Runtime.enable')
await client.send('Page.navigate', { url: APP_URL })
await sleep(3000)

// 登录表单
await evaluate(client, page, `(() => {
  const inputs = document.querySelectorAll('input');
  inputs[0].value = 'admin';
  inputs[0].dispatchEvent(new Event('input', { bubbles: true }));
  if (inputs[1]) { inputs[1].value = 'admin123'; inputs[1].dispatchEvent(new Event('input', { bubbles: true })); }
  const btn = document.querySelector('button');
  btn.click();
  return 'login';
})()`)
await sleep(2500)

// 查看 dashboard 请求返回
const dashResp = await evaluate(client, page, `(() => {
  // 重新触发 dashboard 加载，看返回
  return new Promise((resolve) => {
    fetch('/api/admin/dashboard', { headers: { 'Authorization': 'Bearer ' + (localStorage.getItem('wc_admin_token')||'') } })
      .then(r => r.json()).then(j => resolve({ status: 'ok', code: j.code, data: j.data && { users: j.data.users, user_trend: j.data.user_trend } }))
      .catch(e => resolve({ status: 'error', msg: String(e) }));
  });
})()`)
console.log('dashboard 请求返回 =', JSON.stringify(dashResp))

// 刷新页面重新加载看板（触发 onMounted → loadDashboard）
await client.send('Page.reload')
await sleep(3500)

// 调试：登录后的页面状态
const dbg = await evaluate(client, page, `(() => {
  const hasSidebar = !!document.querySelector('.sidebar, .nav-item');
  const loginVisible = !!document.querySelector('.login-card');
  const body = (document.body.textContent || '').replace(/\\s+/g,' ').slice(0, 200);
  return { hasSidebar, loginVisible, body };
})()`)
console.log('登录后状态 =', JSON.stringify(dbg))

// 检查看板状态
const dash = await evaluate(client, page, `(() => {
  const stats = [...document.querySelectorAll('.stat-num')].map(el => el.textContent.trim());
  const barCharts = document.querySelectorAll('.bar-chart').length;
  const bars = document.querySelectorAll('.bar').length;
  const chartCards = [...document.querySelectorAll('.chart-card h3')].map(el => el.textContent.trim());
  const barLabels = [...document.querySelectorAll('.bar-label')].map(el => el.textContent.trim());
  return { stats, barCharts, bars, chartCards, barLabels };
})()`)
console.log('看板状态 =', JSON.stringify(dash))

console.log('\n=== 结果 ===')
if (dash.stats && dash.stats.length === 4) {
  console.log('统计卡片（4 个）=', dash.stats.join(' / '))
}
if (dash.barCharts === 2 && dash.bars === 14) {
  console.log('PASS：渲染 2 个柱状图，共 14 根柱子（2×7天）')
} else {
  console.log('FAIL：柱状图数量不正确，chartCards =', JSON.stringify(dash.chartCards), 'bars =', dash.bars)
}

child.kill()
process.exit(0)
