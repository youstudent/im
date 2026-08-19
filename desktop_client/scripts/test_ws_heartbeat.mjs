/** 最小 WS 测试：验证 Node 全局 WebSocket 能连服务端并收到 heartbeat pong。 */
const WS_URL = 'ws://127.0.0.1:8080/ws'
const BASE = 'http://127.0.0.1:8080'

const acc = 'hb_' + Date.now()
const res = await fetch(BASE + '/api/v1/auth/register', {
  method: 'POST', headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ nickname: '心跳测试', account: acc, password: 'test1234' }),
})
const j = await res.json()
console.log('register code=', j.code, ' uid=', j.data && j.data.user.uid)
const token = j.data.access_token

const ws = new WebSocket(WS_URL)
ws.onopen = () => {
  console.log('[WS] open')
  ws.send(JSON.stringify({ ver: 1, type: 'auth', seq: 1, body: { token } }))
}
ws.onmessage = (e) => {
  console.log('[WS] <=', e.data.slice(0, 150))
}
ws.onerror = (e) => console.log('[WS] error', e.message || '')
ws.onclose = (e) => console.log('[WS] close code=', e.code, 'reason=', e.reason)

setTimeout(() => {
  console.log('[WS] sending heartbeat')
  ws.send(JSON.stringify({ ver: 1, type: 'heartbeat', seq: 2, body: {} }))
}, 2000)

setTimeout(() => { ws.close(); process.exit(0) }, 5000)
