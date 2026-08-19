const { contextBridge, ipcRenderer } = require('electron')

// 更新进度监听器引用（重复注册时先移除旧的）
let updaterProgressListener = null

// 通过 contextBridge 向渲染进程（window.electronAPI）安全暴露受限接口，
// 不直接暴露 Node 能力，符合 contextIsolation 最佳实践。
contextBridge.exposeInMainWorld('electronAPI', {
  getAppVersion: () => ipcRenderer.invoke('app:version'),
  ping: () => ipcRenderer.invoke('app:ping'),
  // 用系统默认浏览器打开外部链接（仅 http/https）
  openExternal: (url) => ipcRenderer.invoke('shell:open-external', url),
  // 自动更新：下载安装包并静默安装；进度经 updater:progress 事件回传
  updater: {
    // installDir：当前应用安装目录（渲染进程传 process.execPath），静默安装时覆盖安装到原位置
    // sha256：发布记录中的安装包摘要，主进程下载后校验，不一致则中止安装（防篡改）
    downloadAndInstall: (url, installDir, sha256) =>
      ipcRenderer.invoke('updater:download-and-install', { url, installDir, sha256 }),
    // 注册进度回调（重复注册会替换旧监听，避免泄漏）
    onProgress: (cb) => {
      if (updaterProgressListener) ipcRenderer.removeListener('updater:progress', updaterProgressListener)
      updaterProgressListener = (_e, p) => cb(p)
      ipcRenderer.on('updater:progress', updaterProgressListener)
    },
  },
  // 调用系统原生文件夹选择器，返回 { canceled, path }
  selectDirectory: () => ipcRenderer.invoke('dialog:select-directory'),
  // 新消息桌面通知（主进程原生 Notification，点击唤起窗口）
  notification: {
    show: (title, body) => ipcRenderer.invoke('notify:show', { title, body }),
  },
  // 安全令牌存储（safeStorage 系统级加密）
  secureStorage: {
    isEncryptionAvailable: () => ipcRenderer.invoke('storage:is-encryption-available'),
    // 加密存储：传入明文，返回 base64 密文；不可用时返回 null
    encrypt: (plain) => ipcRenderer.invoke('storage:set-secure', plain),
    // 解密读取：传入 base64 密文，返回明文；失败返回 null
    decrypt: (encrypted) => ipcRenderer.invoke('storage:get-secure', encrypted),
  },
})

// ---- 本地存储领域 API（SQLite，主进程持有连接） ----
// 统一返回 { ok, value, error }；渲染进程不传 owner_uid，
// 数据操作一律作用于当前会话账户的库（多账户隔离见 electron/store/db.js）。
contextBridge.exposeInMainWorld('store', {
  // 多账户会话：登录成功后 open，登出/被踢时 close
  session: {
    open: (uid) => ipcRenderer.invoke('store:session:open', { uid }),
    close: () => ipcRenderer.invoke('store:session:close'),
    current: () => ipcRenderer.invoke('store:session:current'),
  },
  conversations: {
    list: () => ipcRenderer.invoke('store:conversations:list'),
    upsert: (convs) => ipcRenderer.invoke('store:conversations:upsert', { convs }),
    bump: (convId, lastMsg, lastMsgTime) =>
      ipcRenderer.invoke('store:conversations:bump', { convId, lastMsg, lastMsgTime }),
    setUnread: (convId, unread) => ipcRenderer.invoke('store:conversations:set-unread', { convId, unread }),
    updateSyncSeq: (convId, seq) => ipcRenderer.invoke('store:conversations:update-sync-seq', { convId, seq }),
    setNoPersist: (convId, flag) => ipcRenderer.invoke('store:conversations:set-no-persist', { convId, flag }),
    remove: (convId) => ipcRenderer.invoke('store:conversations:remove', { convId }),
  },
  messages: {
    list: (convId, { beforeSeq, limit } = {}) =>
      ipcRenderer.invoke('store:messages:list', { convId, beforeSeq, limit }),
    search: (keyword, { type, limit, convId, offset } = {}) =>
      ipcRenderer.invoke('store:messages:search', { keyword, type, limit, convId, offset }),
    upsert: (msgs) => ipcRenderer.invoke('store:messages:upsert', { msgs }),
    appendPending: (msg) => ipcRenderer.invoke('store:messages:append-pending', { msg }),
    setSyncState: (localId, state, extra = {}) =>
      ipcRenderer.invoke('store:messages:set-sync-state', { localId, state, ...extra }),
    listPending: () => ipcRenderer.invoke('store:messages:list-pending'),
    claimPending: (convId, content) => ipcRenderer.invoke('store:messages:claim-pending', { convId, content }),
    removeByConv: (convId) => ipcRenderer.invoke('store:messages:remove-by-conv', { convId }),
  },
  kv: {
    get: (key) => ipcRenderer.invoke('store:kv:get', { key }),
    set: (key, value) => ipcRenderer.invoke('store:kv:set', { key, value }),
  },
  meta: {
    getPath: () => ipcRenderer.invoke('store:meta:path'),
  },
  // 设置页承接：占用统计 / 清理 / 保留期 / 导出 / 备份
  storage: {
    usage: () => ipcRenderer.invoke('store:storage:usage'),
    clearCache: () => ipcRenderer.invoke('store:storage:clear-cache'),
    purge: (days) => ipcRenderer.invoke('store:storage:purge', { days }),
    // 存储路径迁移：返回 { ok, value: 新路径, error }
    setPath: (p) => ipcRenderer.invoke('store:storage:set-path', { path: p }),
    // 清除本账户数据：返回 { ok, value: { uid }, error }
    clearAccount: () => ipcRenderer.invoke('store:storage:clear-account'),
  },
  export: {
    // 保存对话框：返回 { canceled, path }
    saveDialog: (format) => ipcRenderer.invoke('store:export:save-dialog', { format }),
    messages: (filePath, format) => ipcRenderer.invoke('store:export:messages', { filePath, format }),
  },
  backup: {
    create: (destDir) => ipcRenderer.invoke('store:backup:create', { destDir }),
  },
  // 文件缓存：解析缓存地址 / 系统程序打开（wcfile:// 协议）
  fileCache: {
    resolve: (url, key, name) => ipcRenderer.invoke('store:file:resolve', { url, key, name }),
    open: (url, key, name) => ipcRenderer.invoke('store:file:open', { url, key, name }),
  },
})
