/**
 * 消息 / 会话接口。
 */
import { http } from './http'

export const messageApi = {
  // 会话列表；changedSince（unix 秒）非空时仅返回该时间后有变化的会话（差量刷新，减压服务端）
  listConversations({ changedSince } = {}) {
    const q = new URLSearchParams()
    if (changedSince) q.set('changed_since', changedSince)
    const qs = q.toString()
    return http.get(`/conversations${qs ? '?' + qs : ''}`)
  },
  // 拉取某会话历史消息：
  // - afterSeq：增量拉取 seq > afterSeq 的新消息（升序，本地缓存补齐）
  // - beforeSeq：向前翻页；都不传时取最新 limit 条
  getHistory(convId, { beforeSeq, afterSeq, limit } = {}) {
    const q = new URLSearchParams()
    if (afterSeq) q.set('after_seq', afterSeq)
    else if (beforeSeq) q.set('before_seq', beforeSeq)
    if (limit) q.set('limit', limit)
    const qs = q.toString()
    return http.get(`/conversations/${convId}/messages${qs ? '?' + qs : ''}`)
  },
  // 发送消息（HTTP 兜底，高频走 WS）
  // data: { conv_id, target_id, conv_type, type, msg_id, content, extra }
  send(data) {
    return http.post('/conversations', data)
  },
  // 撤回消息（2 分钟内）
  // data: { msg_id }  msg_id 为雪花 ID，传字符串
  recall(convId, msgId) {
    return http.post(`/conversations/${convId}/recall`, { msg_id: String(msgId) })
  },
  // 更新会话设置（置顶/免打扰）：settings = { pinned?: 0|1, muted?: 0|1 }，未传字段保持不变
  updateSettings(convId, settings) {
    return http.put(`/conversations/${convId}/settings`, settings)
  },
  // 删除会话（仅删本人会话视图行，保留消息；再次收发消息自动重建）
  deleteConversation(convId) {
    return http.delete(`/conversations/${convId}`)
  },
  // 搜索消息（keyword / type）
  search({ keyword, type, limit } = {}) {
    const q = new URLSearchParams()
    if (keyword) q.set('keyword', keyword)
    if (type) q.set('type', type)
    if (limit) q.set('limit', limit)
    const qs = q.toString()
    return http.get(`/conversations/search${qs ? '?' + qs : ''}`)
  },
}
