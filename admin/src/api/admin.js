import { http } from './http'

export const adminApi = {
  login(username, password) {
    return http.post('/login', { username, password })
  },
  dashboard() {
    return http.get('/dashboard')
  },
  listUsers(offset, limit, keyword = '', status = 0) {
    const q = keyword ? `&q=${encodeURIComponent(keyword)}` : ''
    const st = status ? `&status=${status}` : ''
    return http.get(`/users?offset=${offset}&limit=${limit}${q}${st}`)
  },
  disableUser(uid) {
    return http.delete(`/users/${uid}/disable`)
  },
  enableUser(uid) {
    return http.delete(`/users/${uid}/enable`)
  },
  listGroups(offset, limit, keyword = '') {
    const q = keyword ? `&q=${encodeURIComponent(keyword)}` : ''
    return http.get(`/groups?offset=${offset}&limit=${limit}${q}`)
  },
  groupMessages(gUid, beforeSeq = 0, limit = 50) {
    return http.get(`/groups/${gUid}/messages?before_seq=${beforeSeq}&limit=${limit}`)
  },
  deleteGroup(gUid) {
    return http.delete(`/groups/${gUid}`)
  },
  // 版本发布：发布新版本 / 版本列表（客户端检查更新用）
  publishVersion(data) {
    return http.post('/version', data)
  },
  listVersions(offset = 0, limit = 20) {
    return http.get(`/versions?offset=${offset}&limit=${limit}`)
  },
  // 安装包上传预签名（前端直传 OSS）：返回 { object_key, upload_url, download_url, expire_in }
  presignFile(data) {
    return http.post('/files/presign', data)
  },
}
