// 一次性工具：清理 Redis 注册/登录频控计数（本地开发调试用）
const net = require('node:net')
const keys = ['auth:reg:limit:127.0.0.1', 'auth:login:limit:ip:127.0.0.1']
const s = net.connect(6379, '127.0.0.1')
let buf = ''
s.on('connect', () => {
  let payload = ''
  for (const k of keys) {
    payload += '*2\r\n$3\r\nDEL\r\n$' + Buffer.byteLength(k) + '\r\n' + k + '\r\n'
  }
  s.write(payload)
})
s.on('data', (d) => {
  buf += d.toString()
  if (buf.includes('\r\n') || buf.length >= 2) {
    console.log('resp:', JSON.stringify(buf))
    s.end()
  }
})
s.on('error', (e) => {
  console.log('err:', e.message)
  process.exit(1)
})
setTimeout(() => process.exit(0), 3000)
