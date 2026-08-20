/**
 * 本地存储渲染进程封装：统一判空降级 + 解包 { ok, value, error }。
 *
 * window.store 由 preload 经 contextBridge 暴露（Electron 环境）；
 * 浏览器调试 / 无 Electron 环境时全部方法降级为 no-op / 空值，现有纯网络逻辑不受影响。
 */

function bridge() {
  return typeof window !== 'undefined' ? window.store || null : null
}

// 解包 IPC 结果：{ ok, value } → value；失败返回 fallback
function unwrap(res, fallback = null) {
  if (res && res.ok) return res.value
  return fallback
}

async function invoke(ns, method, ...args) {
  const s = bridge()
  const fn = s && s[ns] && s[ns][method]
  if (typeof fn !== 'function') return null
  try {
    return await fn(...args)
  } catch (e) {
    console.warn('[localdb]', ns + '.' + method, e?.message || e)
    return null
  }
}

export const localdb = {
  // 能力探测（浏览器环境为 false）
  available() {
    return !!bridge()
  },

  // ---- 多账户会话 ----
  session: {
    // 幂等：重复打开同一 uid 不重建连接；uid 来自登录响应的业务 uid
    async open(uid) {
      return unwrap(await invoke('session', 'open', uid), null)
    },
    async close() {
      return unwrap(await invoke('session', 'close'), false)
    },
    async current() {
      return unwrap(await invoke('session', 'current'), null)
    },
  },

  // ---- 会话列表 ----
  conversations: {
    async list() {
      return unwrap(await invoke('conversations', 'list'), []) || []
    },
    // convs: [{ id, type, target_id, last_msg, last_msg_time, unread, peer_read_seq, ... }]
    async upsert(convs) {
      return unwrap(await invoke('conversations', 'upsert', convs), 0)
    },
    async bump(convId, lastMsg, lastMsgTime, senderUid, senderName) {
      return unwrap(await invoke('conversations', 'bump', convId, lastMsg, lastMsgTime, senderUid, senderName), false)
    },
    async setUnread(convId, unread) {
      return unwrap(await invoke('conversations', 'setUnread', convId, unread), false)
    },
    async setMarkedUnread(convId, flag) {
      return unwrap(await invoke('conversations', 'setMarkedUnread', convId, flag), false)
    },
    async updateSyncSeq(convId, seq) {
      return unwrap(await invoke('conversations', 'updateSyncSeq', convId, seq), false)
    },
    // 敏感会话不落盘：flag=true 时同步清除该会话已落盘消息
    async setNoPersist(convId, flag) {
      return unwrap(await invoke('conversations', 'setNoPersist', convId, flag), false)
    },
    // 会话草稿（纯本地，不同步服务端）；空值清除
    async setDraft(convId, draft) {
      return unwrap(await invoke('conversations', 'setDraft', convId, draft), false)
    },
    // 会话置顶/免打扰（本地即时生效，服务端同步由调用方另行调 HTTP）
    async setSettings(convId, settings) {
      return unwrap(await invoke('conversations', 'setSettings', convId, settings), false)
    },
    // 删除会话行（退群清理）
    async remove(convId) {
      return unwrap(await invoke('conversations', 'remove', convId), false)
    },
  },

  // ---- 消息 ----
  messages: {
    async list(convId, { beforeSeq, limit } = {}) {
      return unwrap(await invoke('messages', 'list', convId, { beforeSeq, limit }), []) || []
    },
    // 消息搜索（查找聊天记录）：keyword 关键字 + type 过滤（2 图片/3 文件/4 链接）；
    // convId 非空时仅搜当前会话；offset/limit 滚动分页
    async search(keyword, { type, limit, convId, offset } = {}) {
      return unwrap(await invoke('messages', 'search', keyword, { type, limit, convId, offset }), []) || []
    },
    // 删除某会话全部消息（退群清理）
    async removeByConv(convId) {
      return unwrap(await invoke('messages', 'removeByConv', convId), 0)
    },
    // 批量删除消息（多选删除，仅本地视角）：ids = { serverIds: [], localIds: [] }
    async deleteMany(convId, ids) {
      return unwrap(await invoke('messages', 'deleteMany', convId, ids), 0)
    },
    // msgs: 服务端消息结构（需带 server_id/id），按 conv_id + server_id 去重
    async upsert(msgs) {
      return unwrap(await invoke('messages', 'upsert', msgs), 0)
    },
    // 离线发送入队，返回 local_id（失败返回 null）
    async appendPending(msg) {
      return unwrap(await invoke('messages', 'appendPending', msg), null)
    },
    // 回填同步状态：state = 'synced' | 'failed'；synced 时带 serverId/seq
    async setSyncState(localId, state, extra = {}) {
      return unwrap(await invoke('messages', 'setSyncState', localId, state, extra), false)
    },
    async listPending() {
      return unwrap(await invoke('messages', 'listPending'), []) || []
    },
    // 重发后回显对账：按 conv_id + 内容认领最早的 pending 行（未命中返回 null）
    async claimPending(convId, content) {
      return unwrap(await invoke('messages', 'claimPending', convId, content), null)
    },
    // 语音已播放标记：写入消息表 voice_played 字段（未读红点状态随消息持久化）
    async markVoicePlayed(serverId) {
      return unwrap(await invoke('messages', 'markVoicePlayed', serverId), false)
    },
  },

  // ---- 元数据 ----
  kv: {
    async get(key) {
      return unwrap(await invoke('kv', 'get', key), null)
    },
    async set(key, value) {
      return unwrap(await invoke('kv', 'set', key, value), false)
    },
  },

  meta: {
    async getPath() {
      return unwrap(await invoke('meta', 'getPath'), '')
    },
  },

  // ---- 设置页承接：占用统计 / 清理 / 保留期 ----
  storage: {
    // 返回 { dbSize, cacheSize, totalSize, root }（字节数）
    async usage() {
      return unwrap(await invoke('storage', 'usage'), null)
    },
    // 立即清理：返回 { freed }（释放字节数）
    async clearCache() {
      return unwrap(await invoke('storage', 'clearCache'), null)
    },
    // 按保留期清理超期消息：days<=0 表示永久保存，返回 { deleted }
    async purge(days) {
      return unwrap(await invoke('storage', 'purge', days), null)
    },
    // 存储路径迁移：返回原始结果 { ok, value: 新路径, error }，供 UI 展示错误
    async setPath(p) {
      const s = bridge()
      if (!s || typeof s.storage?.setPath !== 'function') return { ok: false, error: '本地存储不可用' }
      try {
        return await s.storage.setPath(p)
      } catch (e) {
        return { ok: false, error: e?.message || String(e) }
      }
    },
    // 清除本账户数据：返回原始结果 { ok, value: { uid }, error }
    async clearAccount() {
      const s = bridge()
      if (!s || typeof s.storage?.clearAccount !== 'function') return { ok: false, error: '本地存储不可用' }
      try {
        return await s.storage.clearAccount()
      } catch (e) {
        return { ok: false, error: e?.message || String(e) }
      }
    },
  },

  // ---- 导出聊天记录 ----
  export: {
    // 弹系统保存对话框：返回 { canceled, path }
    async saveDialog(format) {
      return unwrap(await invoke('export', 'saveDialog', format), { canceled: true, path: '' })
    },
    // 导出到指定文件：format = 'txt' | 'html'，返回 { path, conversations }
    async messages(filePath, format) {
      return unwrap(await invoke('export', 'messages', filePath, format), null)
    },
  },

  // ---- 备份 ----
  backup: {
    // 在线备份当前账户库；destDir 缺省存到 backups/，返回 { path }
    async create(destDir) {
      return unwrap(await invoke('backup', 'create', destDir), null)
    },
  },

  // ---- 文件缓存 ----
  fileCache: {
    // 解析媒体资源为缓存地址：命中返回 { hit:true, cacheUrl }（wcfile://）；
    // 未命中会后台下载入缓存，完成后重新调用可命中
    async resolve(url, key, name) {
      return unwrap(await invoke('fileCache', 'resolve', url, key, name), null)
    },
    // 用系统程序打开文件（自动先入缓存），返回 { ok, localPath }
    async open(url, key, name) {
      return unwrap(await invoke('fileCache', 'open', url, key, name), null)
    },
  },
}
