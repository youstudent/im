/**
 * 本地存储：后台定时任务（随账户会话生命周期启停）。
 * 依据 docs/桌面端本地存储方案.md 第 8 节：
 *  - 「自动清理缓存」开关：定期清理 cache 目录 + VACUUM 压缩主库
 *  - 「消息保留时长」：定期删除超期消息（开关开启时随清理执行）
 *  - 「聊天记录备份」开关：定期在线备份到 backups/
 *
 * 策略：每小时检查一次；清理与备份各自按 24 小时间隔执行（避免频繁 VACUUM/备份）。
 * 开关/参数/上次执行时间均存当前账户库 kv（设置页已持久化同名键）。
 */
const { getDb, getSessionUid } = require('./db')
const { kvRepo } = require('./repos')
const storage = require('./storage')

// 与 SettingsWindow 保持一致的 kv 键
const KEY_AUTO_CLEAN = 'workchat:auto-clean'
const KEY_RETENTION = 'workchat:retention-days'
const KEY_BACKUP = 'workchat:backup-enabled'
// 定时任务自身的状态键
const KEY_LAST_CLEAN = 'workchat:maintenance:last-clean'
const KEY_LAST_BACKUP = 'workchat:backup:last-time'

const CHECK_INTERVAL_MS = 60 * 60 * 1000 // 每小时检查一次
const TASK_INTERVAL_SEC = 24 * 3600 // 清理/备份间隔：每天一次
const FIRST_DELAY_MS = 10 * 1000 // 会话打开后延迟首检，避免与启动抢资源

let checkTimer = null
let firstTimer = null

function nowSec() {
  return Math.floor(Date.now() / 1000)
}

// 读开关：kv 无值时用 UI 默认值（自动清理默认开、备份默认关）
function readFlag(key, defaultValue) {
  const v = kvRepo.get(key)
  if (v === null || v === undefined || v === '') return defaultValue
  return v === '1'
}

// 执行一轮维护：按开关与间隔决定清理/备份
function runMaintenance() {
  const db = getDb()
  if (!db || !getSessionUid()) return
  const now = nowSec()

  // ---- 自动清理（含按保留期删除超期消息） ----
  if (readFlag(KEY_AUTO_CLEAN, true)) {
    const lastClean = Number(kvRepo.get(KEY_LAST_CLEAN)) || 0
    if (now - lastClean >= TASK_INTERVAL_SEC) {
      const days = Number(kvRepo.get(KEY_RETENTION)) || 0
      try {
        if (days > 0) storage.purgeOldMessages(days)
        storage.clearCache()
        kvRepo.set(KEY_LAST_CLEAN, String(nowSec()))
        console.log('[store] 定时清理完成', days > 0 ? `(保留期 ${days} 天)` : '')
      } catch (e) {
        console.warn('[store] 定时清理失败:', e?.message || e)
      }
    }
  }

  // ---- 定期备份 ----
  if (readFlag(KEY_BACKUP, false)) {
    const lastBackup = Number(kvRepo.get(KEY_LAST_BACKUP)) || 0
    if (now - lastBackup >= TASK_INTERVAL_SEC) {
      storage
        .createBackup()
        .then((r) => {
          kvRepo.set(KEY_LAST_BACKUP, String(nowSec()))
          console.log('[store] 定时备份完成:', r.path)
        })
        .catch((e) => console.warn('[store] 定时备份失败:', e?.message || e))
    }
  }
}

// 随账户会话打开启动：延迟首检 + 每小时例行检查（幂等）
function start() {
  stop()
  firstTimer = setTimeout(() => {
    try {
      runMaintenance()
    } catch (e) {
      console.warn('[store] 定时任务首检失败:', e?.message || e)
    }
  }, FIRST_DELAY_MS)
  checkTimer = setInterval(runMaintenance, CHECK_INTERVAL_MS)
}

// 随账户会话关闭停止（登出/被踢/应用退出）
function stop() {
  if (firstTimer) {
    clearTimeout(firstTimer)
    firstTimer = null
  }
  if (checkTimer) {
    clearInterval(checkTimer)
    checkTimer = null
  }
}

module.exports = { start, stop, runMaintenance }
