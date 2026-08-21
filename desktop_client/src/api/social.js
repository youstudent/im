/**
 * 社交接口：好友 / 群组 / 通知。
 * friends / groups 带共享缓存：首次调用请求后端并缓存，后续读取缓存，避免登录后立即请求。
 * 通讯录页首次进入时填充缓存，其他页面（如消息页）直接复用，不重复请求。
 */
import { http } from './http'

// 共享缓存（跨组件）：friendList / groupList。
// 缓存同时持久化到 localStorage，避免刷新浏览器后内存缓存丢失，
// 导致消息页 buildContactMap 在缓存为空时强制请求 /friends、/groups（造成不必要的接口请求）。
// 缓存键账户化（workchat:{uid}:friends:cache）：多账户切换天然隔离，
// 不再依赖登出时的 clearAccountCache 兜底（异常退出未走清理时下一账户会看到旧缓存）。
function meUid() {
  try {
    const me = JSON.parse(localStorage.getItem('workchat:me') || 'null')
    return me && me.uid ? String(me.uid) : ''
  } catch {
    return ''
  }
}
function friendCacheKey() {
  const uid = meUid()
  return uid ? `workchat:${uid}:friends:cache` : ''
}
function groupCacheKey() {
  const uid = meUid()
  return uid ? `workchat:${uid}:groups:cache` : ''
}

// 旧版无账户键一次性迁移：归属给当前登录账户后删除旧键；
// 未登录时无法确定归属，保留待下次登录时迁移（handleLoggedIn 会再次调用）。
const LEGACY_FRIEND_KEY = 'workchat:friends:cache'
const LEGACY_GROUP_KEY = 'workchat:groups:cache'
function migrateLegacyCache(legacyKey, accountKey) {
  if (!accountKey) return
  try {
    const legacy = localStorage.getItem(legacyKey)
    if (legacy == null) return
    localStorage.removeItem(legacyKey)
    if (legacy && localStorage.getItem(accountKey) == null) localStorage.setItem(accountKey, legacy)
  } catch {}
}
function migrateLegacyCaches() {
  migrateLegacyCache(LEGACY_FRIEND_KEY, friendCacheKey())
  migrateLegacyCache(LEGACY_GROUP_KEY, groupCacheKey())
}

// 从 localStorage 读取缓存（首次进入应用时恢复，避免刷新后缓存丢失）
function readCache(key) {
  if (!key) return null
  try {
    const raw = localStorage.getItem(key)
    if (raw) return JSON.parse(raw)
  } catch {}
  return null
}
function writeCache(key, value) {
  if (!key) return
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {}
}
function clearStorageCache() {
  try {
    // 仅能删除当前可确定账户的键：登出流程中 me 已先行清除（键为空），
    // 此时退出账户的持久化缓存保留，供同账户下次登录复用；旧键无条件清理
    const fk = friendCacheKey()
    const gk = groupCacheKey()
    if (fk) localStorage.removeItem(fk)
    if (gk) localStorage.removeItem(gk)
    localStorage.removeItem(LEGACY_FRIEND_KEY)
    localStorage.removeItem(LEGACY_GROUP_KEY)
  } catch {}
}

// 初始化内存缓存：先迁移旧键，再从当前账户的持久化数据恢复
migrateLegacyCaches()
// 内存缓存归属账户：异常路径（崩溃/被踢）若漏了缓存清理，账户不匹配时按无缓存处理，防止串账户展示
let cacheOwner = meUid()
let friendListCache = readCache(friendCacheKey())
let groupListCache = readCache(groupCacheKey())

// 切换登录账户后重载：迁移旧键 → 以新账户键恢复持久化缓存到内存（秒开）
function reloadFromStorage() {
  migrateLegacyCaches()
  cacheOwner = meUid()
  friendListCache = readCache(friendCacheKey())
  groupListCache = readCache(groupCacheKey())
}

function ownerOk() {
  return cacheOwner === meUid()
}

function clearCache() {
  friendListCache = null
  groupListCache = null
  cacheOwner = meUid()
  clearStorageCache()
}

export const friendApi = {
  // force=true：向后端请求并刷新缓存；force=false：仅读缓存，缓存为空则返回空数组（不发起请求）
  async list(force = false) {
    if (force) {
      const data = await http.get('/friends')
      cacheOwner = meUid()
      friendListCache = data || []
      writeCache(friendCacheKey(), friendListCache)
      return friendListCache
    }
    return ownerOk() ? friendListCache || [] : []
  },
  // 读缓存（不请求），供消息页等复用
  getCachedFriends() {
    return ownerOk() ? friendListCache || [] : []
  },
  // 缓存是否已初始化（含空列表），用于判断是否需要真正请求后端
  isFriendCacheLoaded() {
    return ownerOk() && friendListCache !== null
  },
  // 仅失效好友缓存（好友关系变化时用，不影响群缓存）
  clearFriendCache() {
    friendListCache = null
    try {
      const k = friendCacheKey()
      if (k) localStorage.removeItem(k)
      localStorage.removeItem(LEGACY_FRIEND_KEY)
    } catch {}
  },
  // 仅失效群缓存（新群创建/被邀请入群时用，不影响好友缓存）
  clearGroupCache() {
    groupListCache = null
    try {
      const k = groupCacheKey()
      if (k) localStorage.removeItem(k)
      localStorage.removeItem(LEGACY_GROUP_KEY)
    } catch {}
  },
  // 账户级缓存清理：登出/被踢/清除账户数据时调用，避免下一账户看到旧账户的好友/群缓存
  clearAccountCache() {
    clearCache()
  },
  // 切换登录账户后重载缓存：迁移旧键并把新账户的持久化缓存载入内存（登录成功时调用）
  reloadAccountCache() {
    reloadFromStorage()
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
    if (ownerOk() && friendListCache) {
      friendListCache = friendListCache.map((f) => (Number(f.uid) === Number(uid) ? { ...f, remark } : f))
      writeCache(friendCacheKey(), friendListCache)
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
      cacheOwner = meUid()
      groupListCache = data || []
      writeCache(groupCacheKey(), groupListCache)
      return groupListCache
    }
    return ownerOk() ? groupListCache || [] : []
  },
  getCachedGroups() {
    return ownerOk() ? groupListCache || [] : []
  },
  // 缓存是否已初始化（含空列表），用于判断是否需要真正请求后端
  isGroupCacheLoaded() {
    return ownerOk() && groupListCache !== null
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
  // 移除群成员（群主/管理员；管理员仅可移除普通成员，后端鉴权）
  async kick(gUid, uid) {
    const r = await http.post(`/groups/${gUid}/members/${uid}/kick`)
    clearCache()
    return r
  },
  // 设为/取消管理员（仅群主）：role 1 管理员 / 2 普通成员
  async setRole(gUid, uid, role) {
    const r = await http.put(`/groups/${gUid}/members/${uid}/role`, { role })
    clearCache()
    return r
  },
  // 转让群主（仅现任群主）：newOwnerUid 新群主 uid
  async transferOwner(gUid, newOwnerUid) {
    const r = await http.put(`/groups/${gUid}/owner`, { new_owner_uid: Number(newOwnerUid) })
    clearCache()
    return r
  },
  // 设置我的群内昵称（任何成员）；空字符串清除回落用户昵称
  async setMyNickname(gUid, nickname) {
    const r = await http.put(`/groups/${gUid}/members/me/nickname`, { nickname })
    clearCache()
    return r
  },
  // 更新群设置开关（仅群主/管理员，后端鉴权）：settings = { invite_confirm?: 0|1, mute_all?: 0|1 }，未传字段保持不变
  async updateSettings(gUid, settings) {
    const r = await http.put(`/groups/${gUid}/settings`, settings)
    clearCache()
    return r
  },
  // 处理入群确认（G7，仅群主/管理员）：inviteeUid 被邀请人，accept 是否同意入群
  async decideInvite(gUid, inviteeUid, accept) {
    const r = await http.post(`/groups/${gUid}/invites/decide`, { invitee_uid: Number(inviteeUid), accept })
    clearCache()
    return r
  },
  // 设置/解除成员禁言（G8，仅群主/管理员）：until unix 毫秒，0 解除
  async muteMember(gUid, uid, until) {
    const r = await http.put(`/groups/${gUid}/members/${uid}/mute`, { until: Number(until) || 0 })
    clearCache()
    return r
  },
  // 更新我"保存到通讯录"开关（G10，任何成员）：saved 0 关闭 / 1 开启
  async setSaved(gUid, saved) {
    const r = await http.put(`/groups/${gUid}/saved`, { saved: saved ? 1 : 0 })
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
