/** 验证会话列表最后一条消息：非文本类型显示类型占位（[图片][文件][视频]），而非资源 URL。 */
const BASE = 'http://127.0.0.1:8080'
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
const a = await post('/api/v1/auth/register', { nickname: '预览A', account: 'pva_' + suffix, password: 'test1234' })
const b = await post('/api/v1/auth/register', { nickname: '预览B', account: 'pvb_' + suffix, password: 'test1234' })
const aUID = a.user.uid
const bUID = b.user.uid

// 1) A 发一条文本消息
await post('/api/v1/conversations', { conv_id: '0', target_id: bUID, conv_type: 1, type: 1, msg_id: String(Date.now() + 1), content: '普通文本消息' }, a.access_token)
let convs = await get('/api/v1/conversations', a.access_token)
let conv = convs.find((c) => String(c.target_id) === String(bUID))
console.log('① 文本消息 last_msg =', JSON.stringify(conv.last_msg), '（期望: 普通文本消息）')

// 2) A 发一条图片消息（content 为图片 URL）
await post('/api/v1/conversations', { conv_id: '0', target_id: bUID, conv_type: 1, type: 2, msg_id: String(Date.now() + 2), content: 'http://oss.aliyun.com/bucket/image1.jpg', extra: '{"url":"http://oss.aliyun.com/bucket/image1.jpg"}' }, a.access_token)
convs = await get('/api/v1/conversations', a.access_token)
conv = convs.find((c) => String(c.target_id) === String(bUID))
console.log('② 图片消息 last_msg =', JSON.stringify(conv.last_msg), '（期望: [图片]）')

// 3) A 发一条文件消息
await post('/api/v1/conversations', { conv_id: '0', target_id: bUID, conv_type: 1, type: 3, msg_id: String(Date.now() + 3), content: 'http://oss.aliyun.com/bucket/report.pdf', extra: '{"name":"report.pdf","url":"http://oss.aliyun.com/bucket/report.pdf"}' }, a.access_token)
convs = await get('/api/v1/conversations', a.access_token)
conv = convs.find((c) => String(c.target_id) === String(bUID))
console.log('③ 文件消息 last_msg =', JSON.stringify(conv.last_msg), '（期望: [文件]）')

// 4) A 发一条视频消息
await post('/api/v1/conversations', { conv_id: '0', target_id: bUID, conv_type: 1, type: 5, msg_id: String(Date.now() + 4), content: 'http://oss.aliyun.com/bucket/video.mp4', extra: '{"url":"http://oss.aliyun.com/bucket/video.mp4"}' }, a.access_token)
convs = await get('/api/v1/conversations', a.access_token)
conv = convs.find((c) => String(c.target_id) === String(bUID))
console.log('④ 视频消息 last_msg =', JSON.stringify(conv.last_msg), '（期望: [视频]）')

// B 侧会话列表也应显示类型占位
const bConvs = await get('/api/v1/conversations', b.access_token)
const bConv = bConvs.find((c) => String(c.target_id) === String(aUID))
console.log('⑤ B 会话 last_msg =', JSON.stringify(bConv.last_msg), '（期望: [视频]）')

const results = [conv.last_msg, bConv.last_msg]
console.log('\n=== 结果 ===')
const ok = results.every((m) => m === '[视频]') && conv.last_msg === '[视频]'
if (ok) console.log('PASS：非文本消息在会话列表显示类型占位，不再显示资源 URL')
else console.log('FAIL：类型占位不正确')

process.exit(0)
