/**
 * 本地存储：ipcMain.handle 注册（命名空间 store:*）。
 * 入参做基础类型校验；渲染进程不传 owner_uid，数据操作一律作用于当前会话账户的库。
 */
const { ipcMain } = require('electron')
const { BrowserWindow, dialog, shell } = require('electron')
const store = require('./db')
const { conversationRepo, messageRepo, kvRepo } = require('./repos')
const storage = require('./storage')
const filecache = require('./filecache')

// 统一包装：异常转 { ok:false, error }，避免渲染进程收到未处理 reject
function safe(fn) {
  return async (_event, ...args) => {
    try {
      return { ok: true, value: await fn(...args) }
    } catch (e) {
      console.warn('[store] ipc 异常:', e?.message || e)
      return { ok: false, error: e?.message || String(e) }
    }
  }
}

function register() {
  // ---- 会话（多账户切换） ----
  ipcMain.handle('store:session:open', safe((arg) => store.openSession(arg?.uid)))
  ipcMain.handle('store:session:close', safe(() => (store.closeSession(), true)))
  ipcMain.handle('store:session:current', safe(() => store.getSessionUid()))

  // ---- conversations ----
  ipcMain.handle('store:conversations:list', safe(() => conversationRepo.listByOwner()))
  ipcMain.handle('store:conversations:upsert', safe((arg) => conversationRepo.upsertMany(arg?.convs)))
  ipcMain.handle('store:conversations:bump', safe((arg) => {
    conversationRepo.bumpLastMessage(arg?.convId, arg?.lastMsg, arg?.lastMsgTime)
    return true
  }))
  ipcMain.handle('store:conversations:set-unread', safe((arg) => {
    conversationRepo.setUnread(arg?.convId, arg?.unread)
    return true
  }))
  ipcMain.handle('store:conversations:update-sync-seq', safe((arg) => {
    conversationRepo.updateSyncSeq(arg?.convId, arg?.seq)
    return true
  }))
  // 敏感会话不落盘：置标记；开启时清除该会话已落盘消息
  ipcMain.handle('store:conversations:set-no-persist', safe((arg) =>
    conversationRepo.setNoPersist(arg?.convId, arg?.flag)
  ))
  // 删除会话行（退群清理）
  ipcMain.handle('store:conversations:remove', safe((arg) => conversationRepo.remove(arg?.convId)))

  // ---- messages ----
  ipcMain.handle('store:messages:list', safe((arg) =>
    messageRepo.listByConv(arg?.convId, { beforeSeq: arg?.beforeSeq, limit: arg?.limit })
  ))
  ipcMain.handle('store:messages:search', safe((arg) =>
    messageRepo.search(arg?.keyword, { type: arg?.type, limit: arg?.limit, convId: arg?.convId })
  ))
  ipcMain.handle('store:messages:upsert', safe((arg) => messageRepo.upsertMany(arg?.msgs)))
  ipcMain.handle('store:messages:append-pending', safe((arg) => messageRepo.appendPending(arg?.msg)))
  ipcMain.handle('store:messages:set-sync-state', safe((arg) =>
    messageRepo.setSyncState(arg?.localId, arg?.state || 'synced', {
      serverId: arg?.serverId,
      seq: arg?.seq,
      convId: arg?.convId,
      createdAt: arg?.createdAt,
    })
  ))
  ipcMain.handle('store:messages:list-pending', safe(() => messageRepo.listPending()))
  ipcMain.handle('store:messages:claim-pending', safe((arg) => messageRepo.claimPending(arg?.convId, arg?.content)))
  // 删除某会话全部消息（退群清理）
  ipcMain.handle('store:messages:remove-by-conv', safe((arg) => messageRepo.deleteByConv(arg?.convId)))

  // ---- kv / 元数据 ----
  ipcMain.handle('store:kv:get', safe((arg) => kvRepo.get(arg?.key)))
  ipcMain.handle('store:kv:set', safe((arg) => kvRepo.set(arg?.key, arg?.value)))
  ipcMain.handle('store:meta:path', safe(() => store.getStorageRoot()))

  // ---- 设置页承接：占用统计 / 清理 / 保留期 / 导出 / 备份 ----
  ipcMain.handle('store:storage:usage', safe(() => storage.getUsage()))
  ipcMain.handle('store:storage:clear-cache', safe(() => storage.clearCache()))
  ipcMain.handle('store:storage:purge', safe((arg) => storage.purgeOldMessages(arg?.days)))
  // 存储路径迁移：移动整个 storage 目录并切换配置（失败自动回滚）
  ipcMain.handle('store:storage:set-path', safe(async (arg) => store.setStorageRoot(arg?.path)))
  // 清除本账户数据：删除 accounts/{uid}/ 并重建空库
  ipcMain.handle('store:storage:clear-account', safe(() => storage.clearAccountData()))
  // 导出保存对话框：返回 { canceled, path }
  ipcMain.handle('store:export:save-dialog', async (event, arg) => {
    try {
      const win = BrowserWindow.fromWebContents(event.sender)
      const isHtml = arg?.format === 'html'
      const result = await dialog.showSaveDialog(win, {
        title: '导出聊天记录',
        defaultPath: `workchat-聊天记录.${isHtml ? 'html' : 'txt'}`,
        filters: isHtml ? [{ name: 'HTML 文件', extensions: ['html'] }] : [{ name: '文本文件', extensions: ['txt'] }],
      })
      return { ok: true, value: { canceled: !!result.canceled, path: result.filePath || '' } }
    } catch (e) {
      return { ok: false, error: e?.message || String(e) }
    }
  })
  ipcMain.handle('store:export:messages', safe((arg) => storage.exportMessages(arg?.filePath, arg?.format)))
  ipcMain.handle('store:backup:create', safe((arg) => storage.createBackup(arg?.destDir)))

  // ---- 文件缓存：解析缓存地址 / 用系统程序打开 ----
  ipcMain.handle('store:file:resolve', safe((arg) => filecache.resolveMedia({ url: arg?.url, key: arg?.key, name: arg?.name })))
  ipcMain.handle('store:file:open', safe(async (arg) => {
    const r = await filecache.openLocal({ url: arg?.url, key: arg?.key, name: arg?.name })
    if (!r.ok) throw new Error('文件未就绪（下载失败或地址无效）')
    // openPath 成功返回空串，失败返回错误原因（如系统无关联应用）；
    // 不检查会把错误吞掉，渲染进程无法兜底（历史 bug：.webm 无关联时只弹系统错误框）
    const err = await shell.openPath(r.localPath)
    if (err) throw new Error('系统无法打开该文件：' + err)
    return r
  }))
}

module.exports = { register }
