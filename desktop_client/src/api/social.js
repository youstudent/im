/**
 * 社交接口：好友 / 群组 / 通知。
 * friends / groups 带共享缓存：首次调用请求后端并缓存，后续读取缓存，避免登录后立即请求。
 * 通讯录页首次进入时填充缓存，其他页面（如消息页）直接复用，不重复请求。
 */
import { http } from './http'

// 共享缓存（跨组件）：friendList / groupList。
// 缓存同时持久化到 localStorage，避免刷新浏览器后内存缓存丢失，
// 导致消息页 buildContactMap 在缓存为空时强制请求 /friends、/groups（造成不必要的接口请求）。
const FRIEND_CACHE_KEY = 'workchat:friends:cache'
const GROUP_CACHE_KEY = 'workchat:groups:cache'

// 从 localStorage 读取缓存（首次进入应用时恢复，避免刷新后缓存丢失）
function readCache(key) {
  try {
    const raw = localStorage.getItem(key)
    if (raw) return JSON.parse(raw)
  } catch {}
  return null
}
function writeCache(key, value) {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {}
}
function clearStorageCache() {
  try {
    localStorage.removeItem(FRIEND_CACHE_KEY)
    localStorage.removeItem(GROUP_CACHE_KEY)
  } catch {}
}

// 初始化内存缓存：优先用 localStorage 持久化的数据，避免刷新后丢失
let friendListCache = readCache(FRIEND_CACHE_KEY)
let groupListCache = readCache(GROUP_CACHE_KEY)

function clearCache() {
  friendListCache = null
  groupListCache = null
  clearStorageCache()
}

export const friendApi = {
  // force=true：向后端请求并刷新缓存；force=false：仅读缓存，缓存为空则返回空数组（不发起请求）
  async list(force = false) {
    if (force) {
      const data = await http.get('/friends')
      friendListCache = data || []
      writeCache(FRIEND_CACHE_KEY, friendListCache)
      return friendListCache
    }
    return friendListCache || []
  },
  // 读缓存（不请求），供消息页等复用
  getCachedFriends() {
    return friendListCache || []
  },
  // 缓存是否已初始化（含空列表），用于判断是否需要真正请求后端
  isFriendCacheLoaded() {
    return friendListCache !== null
  },
  // 仅失效好友缓存（好友关系变化时用，不影响群缓存）
  clearFriendCache() {
    friendListCache = null
    try {
      localStorage.removeItem(FRIEND_CACHE_KEY)
    } catch {}
  },
  // 账户级缓存清理：登出/被踢时调用，避免下一账户看到旧账户的好友/群缓存
  clearAccountCache() {
    clearCache()
  },
  async listRequests() {
    return http.get('/friends/requests')
  },
  search(account) {
    return http.get(`/users/search?account=${encodeURIComponent(account)}`)
  },
  async sendRequest(toUid, message) {
    const r = await http.post('/friends/requests', { to_uid: toUid, message })
    return r
  },
  async handleRequest(reqId, accept) {
    const r = await http.post(`/friends/requests/${reqId}/handle`, { accept })
    clearCache()
    return r
  },
  async delete(uid) {
    const r = await http.delete(`/friends/${uid}`)
    clearCache()
    return r
  },
  // 设置好友备注（空字符串清除）；成功后就地更新缓存与持久化，避免全量重拉
  async setRemark(uid, remark) {
    const r = await http.put(`/friends/${uid}/remark`, { remark })
    if (friendListCache) {
      friendListCache = friendListCache.map((f) => (Number(f.uid) === Number(uid) ? { ...f, remark } : f))
      writeCache(FRIEND_CACHE_KEY, friendListCache)
    }
    return r
  },
}

export const groupApi = {
  // 创建群聊：name 群名，members 成员 uid 数组，avatar 群头像 URL（可选）
  async create(name, members, avatar) {
    const r = await http.post('/groups', { name, members, avatar })
    clearCache()
    return r
  },
  // force=true：向后端请求并刷新缓存；force=false：仅读缓存，缓存为空则返回空数组（不发起请求）
  async list(force = false) {
    if (force) {
      const data = await http.get('/groups')
      groupListCache = data || []
      writeCache(GROUP_CACHE_KEY, groupListCache)
      return groupListCache
    }
    return groupListCache || []
  },
  getCachedGroups() {
    return groupListCache || []
  },
  // 缓存是否已初始化（含空列表），用于判断是否需要真正请求后端
  isGroupCacheLoaded() {
    return groupListCache !== null
  },
  get(gUid) {
    return http.get(`/groups/${gUid}`)
  },
  // 修改群名/群公告（仅群主或管理员，后端鉴权）；成功后清缓存使下次拉取最新
  async update(gUid, name, announcement) {
    const r = await http.put(`/groups/${gUid}`, { name, announcement })
    clearCache()
    return r
  },
  async invite(gUid, members) {
    const r = await http.post(`/groups/${gUid}/members`, { members })
    clearCache()
    return r
  },
  async leave(gUid) {
    const r = await http.delete(`/groups/${gUid}/members/me`)
    clearCache()
    return r
  },
}

export const notifyApi = {
  list() {
    return http.get('/notifications')
  },
  markRead(id) {
    return http.post(`/notifications/read?id=${id}`)
  },
  markAllRead() {
    return http.post('/notifications/read?all=1')
  },
  unread() {
    return http.get('/notifications/unread')
  },
  clear() {
    return http.delete('/notifications')
  },
}
