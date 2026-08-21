<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { localdb } from '../api/localdb'
import { friendApi } from '../api/social'
import { notifySettings, playNotifySound, showDesktopNotification } from '../api/notify'
import { getAppVersion, checkLatestVersion, openDownloadUrl, autoUpdateAvailable, onInstallProgress, downloadAndInstall } from '../api/version'

const emit = defineEmits(['close', 'logout'])

defineProps({
  activeTab: {
    type: String,
    default: 'general', // general | notifications | privacy | storage | about
  },
})

// 当前选中的分类
const activeCategory = ref('general')

// 关闭弹窗
function closeModal() {
  emit('close')
}

// 切换分类
function selectCategory(key) {
  activeCategory.value = key
}

// 账号信息：展示当前登录用户（登录时写入 localStorage['workchat:me']）
function loadAccount() {
  let me = null
  if (typeof window !== 'undefined') {
    try {
      me = JSON.parse(localStorage.getItem('workchat:me') || 'null')
    } catch {
      me = null
    }
  }
  const uid = me && me.uid ? String(me.uid) : ''
  return {
    avatar: (me && me.nickname ? me.nickname[0] : '?').toUpperCase(),
    avatarColor: '#8b5cf6',
    name: (me && me.nickname) || '未登录',
    userId: uid, // WorkChat ID
    account: (me && me.account) || '',
    signature: (me && me.signature) || '',
  }
}
const account = ref(loadAccount())

// 退出登录（二次确认弹框）
const showLogoutConfirm = ref(false)

// 点击"退出登录"：弹出确认框，不直接退出
function handleLogout() {
  showLogoutConfirm.value = true
}

// 确认退出：关闭弹框并通知父组件切换到登录页
function confirmLogout() {
  showLogoutConfirm.value = false
  emit('logout')
}

// 取消退出
function cancelLogout() {
  showLogoutConfirm.value = false
}

// 分类列表
const categories = [
  { key: 'general', label: '通用' },
  { key: 'notifications', label: '通知' },
  { key: 'privacy', label: '隐私' },
  { key: 'storage', label: '数据存储' },
  { key: 'about', label: '关于' },
]

// 主题键与本地存储键
const THEME_KEY = 'workchat:theme'
const FONT_SIZE_KEY = 'workchat:font-size'

/**
 * 读取已保存的主题：
 * 1. 优先取 localStorage
 * 2. 否则回退到系统偏好 prefers-color-scheme
 * 3. 最终缺省 light
 */
function getInitialTheme() {
  if (typeof window === 'undefined') return 'light'
  try {
    const saved = window.localStorage.getItem(THEME_KEY)
    if (saved === 'light' || saved === 'dark') return saved
    if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
      return 'dark'
    }
  } catch (e) {
    /* 忽略 localStorage 访问异常（如隐私模式） */
  }
  return 'light'
}

/**
 * 读取已保存的字体大小：small / medium / large，缺省 medium
 */
function getInitialFontSize() {
  if (typeof window === 'undefined') return 'medium'
  try {
    const saved = window.localStorage.getItem(FONT_SIZE_KEY)
    if (saved === 'small' || saved === 'medium' || saved === 'large') return saved
  } catch (e) {
    /* 忽略 */
  }
  return 'medium'
}

// 通用设置状态
const theme = ref(getInitialTheme()) // light | dark

/**
 * 应用主题到 <html>：通过 data-theme 属性让 im-tokens.css 的
 * :root[data-theme='dark'] 选择器覆盖所有设计变量。
 */
function applyTheme(value) {
  if (typeof document === 'undefined') return
  if (value === 'dark') {
    document.documentElement.setAttribute('data-theme', 'dark')
  } else {
    document.documentElement.removeAttribute('data-theme')
  }
  try {
    window.localStorage.setItem(THEME_KEY, value)
  } catch (e) {
    /* 忽略 */
  }
}

// 主题变化时立即应用到 document + 持久化
watch(
  theme,
  (val) => {
    applyTheme(val)
  },
  { immediate: true }
)

const language = ref('简体中文')
const fontSize = ref(getInitialFontSize()) // small | medium | large

/**
 * 应用字体大小到 <html>：通过 data-font-size 属性让 im-tokens.css 的
 * :root[data-font-size] 选择器调整 --im-font-size-base，
 * 从而影响所有用 var(--im-font-size-base) 或 rem 单位的文字。
 */
function applyFontSize(value) {
  if (typeof document === 'undefined') return
  if (value === 'small' || value === 'large') {
    document.documentElement.setAttribute('data-font-size', value)
  } else {
    // medium 视为默认，移除属性即可
    document.documentElement.removeAttribute('data-font-size')
  }
  try {
    window.localStorage.setItem(FONT_SIZE_KEY, value)
  } catch (e) {
    /* 忽略 */
  }
}

// 字体大小变化时立即应用到 document + 持久化
watch(
  fontSize,
  (val) => {
    applyFontSize(val)
  },
  { immediate: true }
)

const autoStart = ref(true)
// “使用 Enter 发送”开关：持久化到 localStorage，主窗口回车时实时读取（关闭后 Enter 换行、Ctrl+Enter 发送）
const SEND_ENTER_KEY = 'workchat:send-with-enter'
const sendWithEnter = ref(true)
try {
  sendWithEnter.value = localStorage.getItem(SEND_ENTER_KEY) !== '0'
} catch {}
function toggleSendWithEnter() {
  sendWithEnter.value = !sendWithEnter.value
  try {
    localStorage.setItem(SEND_ENTER_KEY, sendWithEnter.value ? '1' : '0')
  } catch {}
}

// 通知设置状态（持久化到 localStorage，消息提醒链路实时读取）
const desktopNotify = ref(notifySettings.desktopEnabled())
const notifySound = ref(notifySettings.soundEnabled())
const dndMode = ref(false)

// 切换桌面通知开关
function toggleDesktopNotify() {
  desktopNotify.value = !desktopNotify.value
  notifySettings.setDesktop(desktopNotify.value)
}

// 切换提示音开关：开启时试听一声
function toggleNotifySound() {
  notifySound.value = !notifySound.value
  notifySettings.setSound(notifySound.value)
  if (notifySound.value) playNotifySound()
}

// 测试通知：预览实际效果（提示音 + 桌面通知）
function testNotification() {
  if (notifySound.value) playNotifySound()
  if (desktopNotify.value) showDesktopNotification('WorkChat', '这是一条测试通知')
  if (!notifySound.value && !desktopNotify.value) showToast('桌面通知与提示音均已关闭', 'info')
}
const mentionAlert = ref(true)
const messagePreview = ref(true)

// 隐私设置状态
const onlineVisibility = ref('所有人')
const readReceipt = ref(true)
const lastSeen = ref('仅好友')
const findByPhone = ref(true)
const blocklistCount = ref(3)

// ===== 数据存储（接入本地库，替换静态假数据） =====
const RETENTION_KEY = 'workchat:retention-days'
const AUTO_CLEAN_KEY = 'workchat:auto-clean'
const BACKUP_KEY = 'workchat:backup-enabled'

// 消息保留时长（天；0 = 永久保存）
const retentionOptions = [
  { days: 0, label: '永久保存' },
  { days: 30, label: '30 天' },
  { days: 90, label: '90 天' },
  { days: 180, label: '180 天' },
  { days: 365, label: '1 年' },
]
const retentionDays = ref(0)
const showRetentionMenu = ref(false)
const retentionLabel = computed(
  () => (retentionOptions.find((o) => o.days === retentionDays.value) || retentionOptions[0]).label
)

const autoClean = ref(true)
const backupEnabled = ref(false)

// 缓存占用：实时统计（DB 主库 + cache 目录）
const cacheSize = ref('--')
const cleaning = ref(false)
const exporting = ref(false)
const backingUp = ref(false)
const showExportMenu = ref(false)

// 本地存储路径：优先取主进程真实路径，浏览器环境兜底默认值
const STORAGE_PATH_KEY = 'workchat.storagePath'
const storagePath = ref('C:\\Users\\Admin\\AppData\\Local\\WorkChat')

if (typeof window !== 'undefined') {
  try {
    const saved = window.localStorage.getItem(STORAGE_PATH_KEY)
    if (saved) storagePath.value = saved
  } catch (e) {
    /* 忽略 */
  }
}

// 字节数格式化
function formatBytes(n) {
  const bytes = Number(n) || 0
  if (bytes < 1024) return bytes + ' B'
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = bytes / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return v.toFixed(v >= 100 ? 0 : v >= 10 ? 1 : 2) + ' ' + units[i]
}

// ===== 通用确认弹框（替代原生 confirm，Promise 化） =====
const confirmState = ref(null) // { title, desc[], confirmText, danger, resolve }
function openConfirm({ title, desc, confirmText = '确认', danger = false }) {
  return new Promise((resolve) => {
    confirmState.value = {
      title,
      desc: Array.isArray(desc) ? desc : [desc],
      confirmText,
      danger,
      resolve,
    }
  })
}
function settleConfirm(ok) {
  if (confirmState.value) {
    confirmState.value.resolve(ok)
    confirmState.value = null
  }
}

// ===== 轻量结果提示 toast（替代原生 alert） =====
const toastState = ref(null) // { text, type: success | error | info }
let toastTimer = null
function showToast(text, type = 'info') {
  toastState.value = { text, type }
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(() => {
    toastState.value = null
  }, 3200)
}

// 时间戳格式化（展示上次备份时间）
function formatTime(unixSec) {
  if (!unixSec) return ''
  const d = new Date(unixSec * 1000)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// 上次备份时间（定时/手动备份后更新）
const lastBackupText = ref('')
async function loadLastBackupTime() {
  const v = await localdb.kv.get('workchat:backup:last-time')
  lastBackupText.value = v ? `上次备份：${formatTime(Number(v))}` : ''
}

// 读取占用统计并刷新展示
async function loadUsage() {
  const u = await localdb.storage.usage()
  cacheSize.value = u ? formatBytes(u.totalSize) : '不可用'
}

// 立即清理：清空 cache 目录 + VACUUM 压缩主库，并按保留期清理超期消息
async function doClearCache() {
  if (cleaning.value) return
  cleaning.value = true
  try {
    const res = await localdb.storage.clearCache()
    let purgeRes = null
    if (retentionDays.value > 0) purgeRes = await localdb.storage.purge(retentionDays.value)
    // 同步定时任务时间戳，避免后台当天重复执行
    localdb.kv.set('workchat:maintenance:last-clean', String(Math.floor(Date.now() / 1000)))
    const freed = res ? formatBytes(res.freed) : '0 B'
    const deleted = purgeRes ? purgeRes.deleted : 0
    showToast(`清理完成：释放 ${freed}${deleted ? `，已删除 ${deleted} 条超期消息` : ''}`, 'success')
    await loadUsage()
  } finally {
    cleaning.value = false
  }
}

// 选择保留时长：持久化并立即应用一次清理（非永久时）
async function chooseRetention(days) {
  retentionDays.value = days
  showRetentionMenu.value = false
  try {
    localStorage.setItem(RETENTION_KEY, String(days))
  } catch {}
  localdb.kv.set(RETENTION_KEY, String(days))
  if (days > 0) {
    const r = await localdb.storage.purge(days)
    if (r && r.deleted > 0) {
      showToast(`已清理 ${r.deleted} 条超过 ${days} 天的消息`, 'success')
      await loadUsage()
    } else {
      showToast(`已设置：仅保留最近 ${days} 天的消息`, 'info')
    }
  }
}

// 自动清理开关（持久化）
function toggleAutoClean() {
  autoClean.value = !autoClean.value
  try {
    localStorage.setItem(AUTO_CLEAN_KEY, autoClean.value ? '1' : '0')
  } catch {}
  localdb.kv.set(AUTO_CLEAN_KEY, autoClean.value ? '1' : '0')
}

// 备份开关：开启时立即执行一次备份
async function toggleBackup() {
  backupEnabled.value = !backupEnabled.value
  try {
    localStorage.setItem(BACKUP_KEY, backupEnabled.value ? '1' : '0')
  } catch {}
  localdb.kv.set(BACKUP_KEY, backupEnabled.value ? '1' : '0')
  if (backupEnabled.value) {
    await doBackup(true)
  } else {
    showToast('已关闭自动备份', 'info')
  }
}

// 立即备份：在线备份当前账户库到 backups/（silent=true 时来自开关，成功仅 toast）
async function doBackup(silent = false) {
  if (backingUp.value) return
  backingUp.value = true
  try {
    const res = await localdb.backup.create()
    if (res && res.path) {
      // 更新时间戳：供展示，也避免定时任务当天重复备份
      localdb.kv.set('workchat:backup:last-time', String(Math.floor(Date.now() / 1000)))
      await loadLastBackupTime()
      showToast(`备份完成：${res.path}`, 'success')
    } else {
      showToast(silent ? '自动备份失败，请稍后重试' : '备份失败：本地库会话未打开', 'error')
    }
  } finally {
    backingUp.value = false
  }
}

// 导出聊天记录：选格式 → 系统保存对话框 → 写文件
async function doExport(format) {
  showExportMenu.value = false
  if (exporting.value) return
  const dlg = await localdb.export.saveDialog(format)
  if (!dlg || dlg.canceled || !dlg.path) return
  exporting.value = true
  try {
    const res = await localdb.export.messages(dlg.path, format)
    if (res) showToast(`已导出 ${res.conversations} 个会话：${res.path}`, 'success')
    else showToast('导出失败：本地库会话未打开', 'error')
  } finally {
    exporting.value = false
  }
}

// 打开更改路径：系统文件夹选择器 → 确认 → 迁移全部本地数据（失败自动回滚）
const migrating = ref(false)
async function changeStoragePath() {
  if (migrating.value) return
  if (typeof window === 'undefined' || !window.electronAPI?.selectDirectory) {
    alert('仅桌面客户端支持更改存储位置')
    return
  }
  let res
  try {
    res = await window.electronAPI.selectDirectory()
  } catch {
    return
  }
  if (!res || res.canceled || !res.path) return
  const target = res.path.trim()
  if (target === storagePath.value) return
  const ok = await openConfirm({
    title: '更改存储位置',
    desc: [
      '以下数据将全部迁移到新目录：',
      '聊天记录 · 文件缓存 · 备份文件',
      `目标目录：${target}`,
      '迁移期间请勿关闭应用。',
    ],
    confirmText: '开始迁移',
  })
  if (!ok) return
  migrating.value = true
  try {
    const r = await localdb.storage.setPath(target)
    if (r && r.ok && r.value) {
      storagePath.value = r.value
      try {
        localStorage.setItem(STORAGE_PATH_KEY, r.value)
      } catch {}
      showToast('迁移完成，数据已保存到新位置', 'success')
      await loadUsage()
    } else {
      showToast((r && r.error) || '迁移失败，数据仍保留在原位置', 'error')
    }
  } finally {
    migrating.value = false
  }
}

// 清除本账户数据：双重确认 → 删除本账户本地库（含离线队列）→ 重建空库，
// 并清理账户级联系人缓存；服务端数据不受影响，后续可重新同步。
const clearingAccount = ref(false)
async function clearAccountData() {
  if (clearingAccount.value) return
  const ok = await openConfirm({
    title: '清除本账户数据？',
    desc: [
      '将删除该账户在本设备的全部本地数据：',
      '聊天记录 · 未发送的消息',
      '服务端数据不受影响，清除后将重新同步。',
      '此操作不可恢复。',
    ],
    confirmText: '确认清除',
    danger: true,
  })
  if (!ok) return
  clearingAccount.value = true
  try {
    const r = await localdb.storage.clearAccount()
    if (r && r.ok) {
      friendApi.clearAccountCache()
      showToast('清除完成，聊天记录将从服务端重新同步', 'success')
      await loadUsage()
    } else {
      showToast((r && r.error) || '清除失败', 'error')
    }
  } finally {
    clearingAccount.value = false
  }
}

// 设置页打开时初始化：真实存储路径 + 占用统计 + 恢复开关
onMounted(async () => {
  try {
    const r = localStorage.getItem(RETENTION_KEY)
    if (r != null) retentionDays.value = Number(r) || 0
    const ac = localStorage.getItem(AUTO_CLEAN_KEY)
    if (ac != null) autoClean.value = ac === '1'
    const be = localStorage.getItem(BACKUP_KEY)
    if (be != null) backupEnabled.value = be === '1'
  } catch {}
  const p = await localdb.meta.getPath()
  if (p) {
    storagePath.value = p
    try {
      localStorage.setItem(STORAGE_PATH_KEY, p)
    } catch {}
  }
  await loadUsage()
  await loadLastBackupTime()
  // 加载真实应用版本号
  getAppVersion().then((v) => {
    appVersion.value = v
  })
  // 注册更新下载进度回调
  if (canAutoInstall) {
    onInstallProgress((p) => {
      installProgress.value = p.percent || 0
    })
  }
})

// 关于：当前版本实时取 app.getVersion()（Electron），浏览器兜底 1.0.0
const appVersion = ref('1.0.0')

// 检查更新：manual=true 为用户手动点击（无新版时给提示，且无视 TTL 强制请求）；false 为静默检查（24h TTL 内复用缓存）
const checkingUpdate = ref(false)
const updateInfo = ref(null) // 新版本信息：{ version, download_url, release_notes }
async function checkUpdate(manual = true) {
  if (checkingUpdate.value) return
  checkingUpdate.value = true
  try {
    const r = await checkLatestVersion(appVersion.value, { force: manual })
    if (r.hasNew && r.latest) {
      updateInfo.value = r.latest
    } else if (manual) {
      showToast(r.offline ? '无法连接更新服务，请稍后重试' : '当前已是最新版本', r.offline ? 'error' : 'success')
    }
  } finally {
    checkingUpdate.value = false
  }
}
function closeUpdateModal() {
  updateInfo.value = null
}
// 下载并安装：支持自动安装时走静默安装（应用自动退出）；否则兜底浏览器下载
const installingUpdate = ref(false)
const installProgress = ref(0)
const canAutoInstall = autoUpdateAvailable()
async function downloadUpdate() {
  const url = updateInfo.value && updateInfo.value.download_url
  if (!url || installingUpdate.value) return
  if (!canAutoInstall) {
    openDownloadUrl(url)
    updateInfo.value = null
    return
  }
  installingUpdate.value = true
  installProgress.value = 0
  const r = await downloadAndInstall(url, updateInfo.value && updateInfo.value.sha256)
  if (!r || !r.ok) {
    installingUpdate.value = false
    // 摘要校验失败（可能被篡改/投毒）：不降级到浏览器下载，只提示风险
    if (r && String(r.error || '').includes('摘要')) {
      alert('安装包校验失败，已中止更新。更新源可能存在风险，请通过官方渠道核实。')
      updateInfo.value = null
      return
    }
    openDownloadUrl(url)
    updateInfo.value = null
  }
  // 成功时应用即将退出，无需关闭弹框
}
</script>

<template>
  <div class="modal-overlay" @click.self="closeModal">
    <div class="settings-window">
      <!-- 标题栏：模仿 macOS 风格窗口控制 -->
      <header class="title-bar">
        <div class="title-left">
          <div class="app-icon" aria-label="应用图标">
            <span>W</span>
          </div>
        </div>
        <div class="title-center">WorkChat</div>
        <div class="title-right">
          <button class="window-btn close" aria-label="关闭" @click="closeModal">
            <svg viewBox="0 0 12 12" width="16" height="16">
              <line x1="3" y1="3" x2="9" y2="9" stroke="#1f2329" stroke-width="1.2" stroke-linecap="round" />
              <line x1="9" y1="3" x2="3" y2="9" stroke="#1f2329" stroke-width="1.2" stroke-linecap="round" />
            </svg>
          </button>
        </div>
      </header>

      <div class="window-body">
        <!-- 左侧侧栏：设置分类 -->
        <aside class="settings-sidebar">
          <div class="sidebar-header">
            <div class="sidebar-avatar">W</div>
            <span class="sidebar-title">设置</span>
          </div>

          <div
            v-for="c in categories"
            :key="c.key"
            class="sidebar-item"
            :class="{ active: activeCategory === c.key }"
            :aria-current="activeCategory === c.key ? 'page' : undefined"
            @click="selectCategory(c.key)"
          >
            <!-- 图标 -->
            <span class="sidebar-icon">
              <!-- 通用：调谐器 -->
              <svg v-if="c.key === 'general'" viewBox="0 0 24 24" width="20" height="20">
                <line x1="4" y1="7" x2="14" y2="7" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
                <line x1="18" y1="7" x2="20" y2="7" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
                <circle cx="16" cy="7" r="2.5" fill="none" stroke="currentColor" stroke-width="1.6" />
                <line x1="4" y1="17" x2="6" y2="17" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
                <line x1="10" y1="17" x2="20" y2="17" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
                <circle cx="8" cy="17" r="2.5" fill="none" stroke="currentColor" stroke-width="1.6" />
              </svg>
              <!-- 通知：铃铛 -->
              <svg v-else-if="c.key === 'notifications'" viewBox="0 0 24 24" width="20" height="20">
                <path d="M6 16V11a6 6 0 0 1 12 0v5l1.5 2H4.5L6 16z"
                  fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
                <path d="M10 21h4" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
              </svg>
              <!-- 隐私：盾牌 -->
              <svg v-else-if="c.key === 'privacy'" viewBox="0 0 24 24" width="20" height="20">
                <path d="M12 3l7 3v6c0 4.5-3 8-7 9-4-1-7-4.5-7-9V6l7-3z"
                  fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
                <path d="M9 12l2 2 4-4" fill="none" stroke="currentColor" stroke-width="1.6"
                  stroke-linecap="round" stroke-linejoin="round" />
              </svg>
              <!-- 数据存储：硬盘 -->
              <svg v-else-if="c.key === 'storage'" viewBox="0 0 24 24" width="20" height="20">
                <ellipse cx="12" cy="5" rx="8" ry="2.5" fill="none" stroke="currentColor" stroke-width="1.6" />
                <path d="M4 5v6c0 1.4 3.6 2.5 8 2.5s8-1.1 8-2.5V5" fill="none" stroke="currentColor" stroke-width="1.6" />
                <path d="M4 11v8c0 1.4 3.6 2.5 8 2.5s8-1.1 8-2.5v-8" fill="none" stroke="currentColor" stroke-width="1.6" />
              </svg>
              <!-- 关于：信息圆圈 -->
              <svg v-else viewBox="0 0 24 24" width="20" height="20">
                <circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="1.6" />
                <line x1="12" y1="11" x2="12" y2="17" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
                <circle cx="12" cy="8" r="1.2" fill="currentColor" />
              </svg>
            </span>
            <span class="sidebar-label">{{ c.label }}</span>
          </div>
        </aside>

        <!-- 列分隔线 -->
        <div class="divider-col"></div>

        <!-- 右侧内容区：根据 activeCategory 切换 -->
        <main class="content">
          <!-- ===== 通用 ===== -->
          <template v-if="activeCategory === 'general'">
            <header class="content-header">
              <h2 class="content-title">通用</h2>
              <p class="content-desc">管理外观、语言与基础交互偏好</p>
            </header>

            <!-- ===== 账号分组（退出登录） ===== -->
            <div class="account-card">
              <div class="account-label">账号</div>
              <div class="account-row">
                <div class="account-info">
                  <div class="account-avatar" :style="{ background: account.avatarColor }">
                    <span>{{ account.avatar }}</span>
                  </div>
                  <div class="account-text">
                    <div class="account-name">{{ account.name }}</div>
                    <div class="account-id">WorkChat ID：{{ account.userId }}</div>
                    <div v-if="account.signature" class="account-signature">{{ account.signature }}</div>
                  </div>
                </div>
                <button class="btn-logout" type="button" @click="handleLogout">退出登录</button>
              </div>
            </div>

            <div class="settings-group">
              <!-- 主题 -->
              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">主题</div>
                  <div class="label-sub">选择浅色或深色外观</div>
                </div>
                <div class="seg-group">
                  <button class="seg-btn" :class="{ active: theme === 'light' }" @click="theme = 'light'">浅色</button>
                  <button class="seg-btn" :class="{ active: theme === 'dark' }" @click="theme = 'dark'">深色</button>
                </div>
              </div>
              <div class="divider-row"></div>

              <!-- 语言 -->
              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">语言</div>
                  <div class="label-sub">界面显示语言</div>
                </div>
                <button class="select-btn">
                  <span>{{ language }}</span>
                  <svg viewBox="0 0 16 16" width="16" height="16">
                    <path d="M3 6l5 5 5-5" fill="none" stroke="#646a73" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
                  </svg>
                </button>
              </div>
              <div class="divider-row"></div>

              <!-- 字体大小 -->
              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">字体大小</div>
                  <div class="label-sub">消息与界面文字大小</div>
                </div>
                <div class="seg-group three">
                  <button class="seg-btn" :class="{ active: fontSize === 'small' }" @click="fontSize = 'small'">小</button>
                  <button class="seg-btn" :class="{ active: fontSize === 'medium' }" @click="fontSize = 'medium'">中</button>
                  <button class="seg-btn" :class="{ active: fontSize === 'large' }" @click="fontSize = 'large'">大</button>
                </div>
              </div>
              <div class="divider-row"></div>

              <!-- 开机自启动 -->
              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">开机自启动</div>
                  <div class="label-sub">登录系统后自动运行 WorkChat</div>
                </div>
                <button class="toggle" :class="{ on: autoStart }" @click="autoStart = !autoStart">
                  <span class="toggle-dot"></span>
                </button>
              </div>
              <div class="divider-row"></div>

              <!-- 发送消息 -->
              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">发送消息</div>
                  <div class="label-sub">使用 Enter 发送，Shift+Enter 换行</div>
                </div>
                <button class="toggle" :class="{ on: sendWithEnter }" @click="toggleSendWithEnter()">
                  <span class="toggle-dot"></span>
                </button>
              </div>
            </div>
          </template>

          <!-- ===== 通知 ===== -->
          <template v-else-if="activeCategory === 'notifications'">
            <header class="content-header">
              <h2 class="content-title">通知</h2>
              <p class="content-desc">管理新消息的提醒方式与免打扰</p>
            </header>

            <div class="settings-group">
              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">新消息桌面通知</div>
                  <div class="label-sub">收到消息时弹出桌面提醒，点击可直达会话</div>
                </div>
                <button class="toggle" :class="{ on: desktopNotify }" @click="toggleDesktopNotify">
                  <span class="toggle-dot"></span>
                </button>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">提示音</div>
                  <div class="label-sub">新消息播放提示音</div>
                </div>
                <button class="toggle" :class="{ on: notifySound }" @click="toggleNotifySound">
                  <span class="toggle-dot"></span>
                </button>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">测试通知</div>
                  <div class="label-sub">预览当前的提醒效果</div>
                </div>
                <button class="btn-outline-blue" type="button" @click="testNotification">发送测试</button>
              </div>

              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">勿扰模式</div>
                  <div class="label-sub">指定时间段内静音所有提醒</div>
                </div>
                <button class="toggle" :class="{ on: dndMode }" @click="dndMode = !dndMode">
                  <span class="toggle-dot"></span>
                </button>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">@我 时强提醒</div>
                  <div class="label-sub">被提及或私信时始终提醒</div>
                </div>
                <button class="toggle" :class="{ on: mentionAlert }" @click="mentionAlert = !mentionAlert">
                  <span class="toggle-dot"></span>
                </button>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">消息预览</div>
                  <div class="label-sub">通知中显示消息内容</div>
                </div>
                <button class="toggle" :class="{ on: messagePreview }" @click="messagePreview = !messagePreview">
                  <span class="toggle-dot"></span>
                </button>
              </div>
            </div>
          </template>

          <!-- ===== 隐私 ===== -->
          <template v-else-if="activeCategory === 'privacy'">
            <header class="content-header">
              <h2 class="content-title">隐私</h2>
              <p class="content-desc">管理在线状态、已读回执与黑名单</p>
            </header>

            <div class="settings-group">
              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">在线状态可见性</div>
                  <div class="label-sub">控制谁可以看到你的在线状态</div>
                </div>
                <button class="select-btn">
                  <span>{{ onlineVisibility }}</span>
                  <svg viewBox="0 0 16 16" width="16" height="16">
                    <path d="M3 6l5 5 5-5" fill="none" stroke="#646a73" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
                  </svg>
                </button>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">已读回执</div>
                  <div class="label-sub">让对方看到消息已读状态</div>
                </div>
                <button class="toggle" :class="{ on: readReceipt }" @click="readReceipt = !readReceipt">
                  <span class="toggle-dot"></span>
                </button>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">最后上线时间</div>
                  <div class="label-sub">显示你最近一次活跃的时间</div>
                </div>
                <button class="select-btn">
                  <span>{{ lastSeen }}</span>
                  <svg viewBox="0 0 16 16" width="16" height="16">
                    <path d="M3 6l5 5 5-5" fill="none" stroke="#646a73" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
                  </svg>
                </button>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">允许通过手机号找到我</div>
                  <div class="label-sub">其他用户可通过手机号搜索到你</div>
                </div>
                <button class="toggle" :class="{ on: findByPhone }" @click="findByPhone = !findByPhone">
                  <span class="toggle-dot"></span>
                </button>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">黑名单</div>
                  <div class="label-sub">已屏蔽的联系人将无法联系你</div>
                </div>
                <button class="btn-outline-blue">管理 ({{ blocklistCount }})</button>
              </div>
            </div>
          </template>

          <!-- ===== 数据存储 ===== -->
          <template v-else-if="activeCategory === 'storage'">
            <header class="content-header">
              <h2 class="content-title">数据存储</h2>
              <p class="content-desc">管理本地消息存储、缓存与备份</p>
            </header>

            <div class="settings-group">
              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">本地存储位置</div>
                  <div class="label-sub">消息与文件的存储目录</div>
                </div>
                <div class="setting-control">
                  <span class="path-text" :title="storagePath">{{ storagePath }}</span>
                  <button class="btn-outline-blue" type="button" :disabled="migrating" @click="changeStoragePath">
                    {{ migrating ? '迁移中…' : '更改' }}
                  </button>
                </div>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">消息保留时长</div>
                  <div class="label-sub">超过该时长的记录将自动清理</div>
                </div>
                <div class="picker-wrap">
                  <button class="select-btn" type="button" @click="showRetentionMenu = !showRetentionMenu">
                    <span>{{ retentionLabel }}</span>
                    <svg viewBox="0 0 16 16" width="16" height="16">
                      <path d="M3 6l5 5 5-5" fill="none" stroke="#646a73" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
                    </svg>
                  </button>
                  <div v-if="showRetentionMenu" class="mini-menu">
                    <button
                      v-for="o in retentionOptions"
                      :key="o.days"
                      type="button"
                      class="mini-menu-item"
                      :class="{ active: o.days === retentionDays }"
                      @click="chooseRetention(o.days)"
                    >
                      {{ o.label }}
                    </button>
                  </div>
                </div>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">自动清理缓存</div>
                  <div class="label-sub">定期释放磁盘空间</div>
                </div>
                <button class="toggle" :class="{ on: autoClean }" @click="toggleAutoClean">
                  <span class="toggle-dot"></span>
                </button>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">缓存占用</div>
                  <div class="label-sub">图片、视频等临时文件</div>
                </div>
                <div class="setting-control">
                  <span class="size-text">{{ cacheSize }}</span>
                  <button class="btn-outline-blue" type="button" :disabled="cleaning" @click="doClearCache">
                    {{ cleaning ? '清理中…' : '立即清理' }}
                  </button>
                </div>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">导出聊天记录</div>
                  <div class="label-sub">导出为 HTML / TXT 文件</div>
                </div>
                <div class="picker-wrap">
                  <button class="btn-primary" type="button" :disabled="exporting" @click="showExportMenu = !showExportMenu">
                    {{ exporting ? '导出中…' : '导出' }}
                  </button>
                  <div v-if="showExportMenu" class="mini-menu">
                    <button type="button" class="mini-menu-item" @click="doExport('html')">导出为 HTML</button>
                    <button type="button" class="mini-menu-item" @click="doExport('txt')">导出为 TXT</button>
                  </div>
                </div>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">聊天记录备份</div>
                  <div class="label-sub">
                    开启后每天自动备份到 backups 目录{{ lastBackupText ? '，' + lastBackupText : '' }}
                  </div>
                </div>
                <div class="setting-control">
                  <button class="btn-outline-blue" type="button" :disabled="backingUp" @click="doBackup">
                    {{ backingUp ? '备份中…' : '立即备份' }}
                  </button>
                  <button class="toggle" :class="{ on: backupEnabled }" @click="toggleBackup">
                    <span class="toggle-dot"></span>
                  </button>
                </div>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">清除本账户数据</div>
                  <div class="label-sub">删除本账户本地聊天记录与离线队列，可重新同步</div>
                </div>
                <button class="btn-danger" type="button" :disabled="clearingAccount" @click="clearAccountData">
                  {{ clearingAccount ? '清除中…' : '清除' }}
                </button>
              </div>
            </div>
          </template>

          <!-- ===== 关于 ===== -->
          <template v-else-if="activeCategory === 'about'">
            <header class="content-header">
              <h2 class="content-title">关于</h2>
              <p class="content-desc">版本信息与法律条款</p>
            </header>

            <div class="settings-group">
              <div class="setting-row">
                <div class="setting-label">
                  <div class="label-title">当前版本</div>
                  <div class="label-sub">正在运行的客户端版本</div>
                </div>
                <div class="setting-control">
                  <span class="version-text">WorkChat {{ appVersion }}</span>
                  <button class="btn-primary-blue" type="button" :disabled="checkingUpdate" @click="checkUpdate(true)">
                    {{ checkingUpdate ? '检查中…' : '检查更新' }}
                  </button>
                </div>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row clickable">
                <div class="setting-label">
                  <div class="label-title">用户协议</div>
                  <div class="label-sub">软件使用与服务条款</div>
                </div>
                <span class="link-text">查看 ›</span>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row clickable">
                <div class="setting-label">
                  <div class="label-title">隐私政策</div>
                  <div class="label-sub">我们如何收集与使用数据</div>
                </div>
                <span class="link-text">查看 ›</span>
              </div>
              <div class="divider-row"></div>

              <div class="setting-row clickable">
                <div class="setting-label">
                  <div class="label-title">开源许可证</div>
                  <div class="label-sub">第三方开源组件声明</div>
                </div>
                <span class="link-text">查看 ›</span>
              </div>
            </div>

            <!-- 关于页底部品牌区 -->
            <div class="about-brand">
              <div class="brand-logo">W</div>
              <div class="brand-name">WorkChat</div>
              <div class="brand-desc">让团队沟通更简单</div>
              <div class="brand-copy">© 2026 WorkChat · 保留所有权利</div>
            </div>
          </template>
        </main>
      </div>
    </div>

    <!-- 退出登录二次确认弹框 -->
    <Teleport to="body">
      <div
        v-if="showLogoutConfirm"
        class="logout-confirm-mask"
        role="dialog"
        aria-modal="true"
        aria-labelledby="logout-confirm-title"
        @click.self="cancelLogout"
      >
        <div class="logout-confirm-dialog">
          <div class="logout-confirm-icon" aria-hidden="true">
            <svg viewBox="0 0 24 24" width="22" height="22">
              <path
                d="M10 17l5-5-5-5M15 12H3M14 3h5a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-5"
                fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"
              />
            </svg>
          </div>

          <h2 id="logout-confirm-title" class="logout-confirm-title">退出登录？</h2>
          <p class="logout-confirm-desc">
            退出后需要重新验证账号，当前未同步的本地设置将保留。
          </p>

          <div class="logout-confirm-actions">
            <button class="logout-btn-cancel" type="button" @click="cancelLogout">取消</button>
            <button class="logout-btn-danger" type="button" @click="confirmLogout">确认退出</button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 通用确认弹框（数据存储操作二次确认） -->
    <Teleport to="body">
      <div
        v-if="confirmState"
        class="wc-confirm-mask"
        role="dialog"
        aria-modal="true"
        aria-labelledby="wc-confirm-title"
        @click.self="settleConfirm(false)"
      >
        <div class="wc-confirm-dialog">
          <div class="wc-confirm-icon" :class="{ danger: confirmState.danger }" aria-hidden="true">
            <!-- 危险操作：警示三角 -->
            <svg v-if="confirmState.danger" viewBox="0 0 24 24" width="26" height="26">
              <path d="M12 3.5 21.5 20h-19L12 3.5z" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" />
              <path d="M12 9.5v4.5" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
              <circle cx="12" cy="17" r="1" fill="currentColor" />
            </svg>
            <!-- 普通操作：文件夹 -->
            <svg v-else viewBox="0 0 24 24" width="26" height="26">
              <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" />
            </svg>
          </div>
          <h2 id="wc-confirm-title" class="wc-confirm-title">{{ confirmState.title }}</h2>
          <div class="wc-confirm-desc">
            <p v-for="(line, i) in confirmState.desc" :key="i" :class="{ strong: i === confirmState.desc.length - 1 && confirmState.danger }">
              {{ line }}
            </p>
          </div>
          <div class="wc-confirm-actions">
            <button class="wc-confirm-btn cancel" type="button" @click="settleConfirm(false)">取消</button>
            <button
              class="wc-confirm-btn"
              :class="confirmState.danger ? 'danger' : 'primary'"
              type="button"
              @click="settleConfirm(true)"
            >
              {{ confirmState.confirmText }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 轻量结果提示 toast -->
    <Teleport to="body">
      <div v-if="toastState" class="wc-toast" :class="toastState.type" role="status">
        <span class="wc-toast-dot" aria-hidden="true"></span>
        <span class="wc-toast-text">{{ toastState.text }}</span>
      </div>
    </Teleport>

    <!-- 发现新版本弹框 -->
    <Teleport to="body">
      <div v-if="updateInfo" class="wc-confirm-mask" role="dialog" aria-modal="true" @click.self="closeUpdateModal">
        <div class="wc-confirm-dialog">
          <div class="wc-confirm-icon" aria-hidden="true">
            <svg viewBox="0 0 24 24" width="26" height="26">
              <path d="M12 16V5" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
              <path d="m7 10 5-5 5 5" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
              <path d="M4 17v2a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-2" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
            </svg>
          </div>
          <h2 class="wc-confirm-title">发现新版本 v{{ updateInfo.version }}</h2>
          <div class="wc-confirm-desc">
            <p v-if="updateInfo.release_notes" class="update-notes">{{ updateInfo.release_notes }}</p>
            <p v-else>新版本已发布，建议下载更新。</p>
            <p class="update-cur">当前版本：v{{ appVersion }}</p>
          </div>
          <div class="wc-confirm-actions">
            <button class="wc-confirm-btn cancel" type="button" @click="closeUpdateModal">稍后</button>
            <button class="wc-confirm-btn primary" type="button" :disabled="installingUpdate" @click="downloadUpdate">
              <template v-if="installingUpdate">下载中 {{ installProgress }}%</template>
              <template v-else>{{ canAutoInstall ? '下载并更新' : '前往下载' }}</template>
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
/* ===== 全屏遮罩 ===== */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: var(--im-overlay);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  padding: 32px;
}

/* ===== 设置窗口容器：宽度 700px，固定高度保证切换分类时尺寸统一 ===== */
.settings-window {
  width: 100%;
  max-width: 700px;
  height: min(620px, calc(100vh - 80px));
  background: var(--im-surface);
  border-radius: 16px;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.2);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* ===== 标题栏（38px） ===== */
.title-bar {
  height: 38px;
  background: var(--im-surface-2);
  display: flex;
  align-items: center;
  padding: 0 12px;
  flex-shrink: 0;
  position: relative;
}

.title-left {
  display: flex;
  align-items: center;
  width: 18px;
}

.app-icon {
  width: 18px;
  height: 18px;
  background: var(--im-primary);
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 11px;
  font-weight: 700;
}

.title-center {
  position: absolute;
  left: 50%;
  transform: translateX(-50%);
  font-size: 13px;
  font-weight: 500;
  color: var(--im-text-title);
}

.title-right {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 8px;
}

.window-btn {
  width: 32px;
  height: 32px;
  background: transparent;
  border: none;
  padding: 0;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
  transition: background 0.15s;
}

.window-btn:hover {
  background: rgba(0, 0, 0, 0.05);
}

/* 关闭按钮 hover：红色小圆背景，比按钮更紧凑，避免过大色块 */
.window-btn.close {
  position: relative;
}

.window-btn.close:hover::before {
  content: '';
  position: absolute;
  inset: 4px;
  border-radius: 50%;
  background: var(--im-danger);
}

.window-btn.close:hover svg {
  position: relative;
  z-index: 1;
}

.window-btn.close:hover svg line {
  stroke: #fff;
}

/* ===== 主体区：左侧栏 + 内容区 ===== */
.window-body {
  flex: 1;
  display: flex;
  min-height: 0;
}

/* ===== 左侧设置分类侧栏：适配 600px 窗口，适当收窄 ===== */
.settings-sidebar {
  width: 168px;
  background: var(--im-surface-2);
  padding: 16px 12px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex-shrink: 0;
}

.sidebar-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 0 16px;
  flex-shrink: 0;
}

.sidebar-avatar {
  width: 40px;
  height: 40px;
  border-radius: 20px;
  background: var(--im-primary);
  color: #fff;
  font-size: 16px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.sidebar-title {
  font-size: calc(var(--im-font-size-base) + 2px);
  font-weight: 500;
  color: var(--im-text-title);
}

.sidebar-item {
  height: 40px;
  padding: 0 12px;
  display: flex;
  align-items: center;
  gap: 12px;
  border-radius: 8px;
  cursor: pointer;
  color: var(--im-text-title);
  font-size: 14px;
  transition: background 0.15s ease;
  -webkit-user-select: none;
  user-select: none;
}

.sidebar-item:hover {
  background: rgba(0, 0, 0, 0.04);
}

.sidebar-item.active {
  background: rgba(37, 99, 235, 0.1);
}

.sidebar-item.active .sidebar-icon {
  color: var(--im-primary);
}

.sidebar-item.active .sidebar-label {
  font-weight: 500;
}

.sidebar-icon {
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--im-text-secondary);
  flex-shrink: 0;
}

.sidebar-label {
  font-size: var(--im-font-size-base);
  color: var(--im-text-title);
  line-height: 1.43;
}

/* ===== 列分隔线 ===== */
.divider-col {
  width: 1px;
  background: var(--im-border);
  flex-shrink: 0;
}

/* ===== 内容区 ===== */
.content {
  flex: 1;
  min-width: 0;
  background: var(--im-surface);
  padding: 24px 28px;
  display: flex;
  flex-direction: column;
  overflow-y: auto;
}

.content::-webkit-scrollbar {
  width: 6px;
}

.content::-webkit-scrollbar-thumb {
  background: transparent;
  border-radius: 3px;
}

.content:hover::-webkit-scrollbar-thumb {
  background: rgba(0, 0, 0, 0.15);
}

.content-header {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding-bottom: 20px;
}

.content-title {
  margin: 0;
  font-size: calc(var(--im-font-size-base) + 6px);
  font-weight: 500;
  color: var(--im-text-title);
  line-height: 1.45;
}

.content-desc {
  margin: 0;
  font-size: calc(var(--im-font-size-base) - 1px);
  color: var(--im-text-secondary);
  line-height: 1.46;
}

/* ===== 设置项组 ===== */
.settings-group {
  padding-top: 8px;
  display: flex;
  flex-direction: column;
}

.setting-row {
  height: 64px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.setting-row.clickable {
  cursor: pointer;
  padding: 0 -8px;
  margin: 0 -8px;
  border-radius: 8px;
  padding-left: 8px;
  padding-right: 8px;
  transition: background 0.15s ease;
}

.setting-row.clickable:hover {
  background: var(--im-surface-2);
}

.setting-label {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  flex: 1;
}

.label-title {
  font-size: var(--im-font-size-base);
  font-weight: 500;
  color: var(--im-text-title);
  line-height: 1.43;
}

.label-sub {
  font-size: calc(var(--im-font-size-base) - 2px);
  color: var(--im-text-secondary);
  line-height: 1.4;
}

.setting-control {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-shrink: 0;
  min-width: 0;
  max-width: 58%;
}

/* ===== 分隔行 ===== */
.divider-row {
  height: 1px;
  background: var(--im-border);
}

/* ===== 分段控件（浅色/深色、小/中/大） ===== */
.seg-group {
  height: 36px;
  padding: 3px;
  background: var(--im-segment-bg);
  border-radius: 8px;
  display: flex;
  align-items: center;
  gap: 0;
}

.seg-group.three {
  width: 174px;
}

.seg-group:not(.three) {
  width: 150px;
}

.seg-btn {
  flex: 1;
  height: 30px;
  padding: 0 16px;
  background: transparent;
  border: none;
  border-radius: 6px;
  font-size: 13px;
  color: var(--im-text-title);
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s ease, font-weight 0.15s;
}

.seg-btn.active {
  background: var(--im-segment-active);
  font-weight: 500;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.05);
}

.seg-btn:not(.active) {
  font-weight: 400;
}

/* ===== 下拉选择按钮（语言/消息保留时长/可见性/最后上线） ===== */
.select-btn {
  height: 36px;
  padding: 0 12px 0 14px;
  background: var(--im-select-bg);
  border: none;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 150px;
  font-size: 13px;
  color: var(--im-text-title);
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s;
}

.select-btn:hover {
  background: var(--im-select-bg-hover);
}

/* ===== 开关 Toggle ===== */
.toggle {
  width: 44px;
  height: 24px;
  background: var(--im-toggle-off);
  border: none;
  border-radius: 12px;
  padding: 0 2px;
  display: flex;
  align-items: center;
  justify-content: flex-start;
  cursor: pointer;
  transition: background 0.2s ease;
  flex-shrink: 0;
}

.toggle.on {
  background: var(--im-primary);
  justify-content: flex-end;
}

.toggle-dot {
  width: 20px;
  height: 20px;
  background: #fff;
  border-radius: 10px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.15);
}

/* ===== 蓝色描边按钮（更改/立即清理/管理） ===== */
.btn-outline-blue {
  height: 32px;
  padding: 0 14px;
  background: var(--im-soft-blue);
  color: var(--im-text-title);
  border: none;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s;
  flex-shrink: 0;
}

.btn-outline-blue:hover {
  background: var(--im-soft-blue-hover);
}

/* ===== 蓝色实心按钮（检查更新） ===== */
.btn-primary-blue {
  height: 32px;
  padding: 0 16px;
  background: var(--im-primary);
  color: #fff;
  border: none;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s;
  flex-shrink: 0;
}

.btn-primary-blue:hover {
  background: var(--im-primary-hover);
}

/* ===== 实心蓝色按钮（导出） ===== */
.btn-primary {
  height: 32px;
  padding: 0 16px;
  background: var(--im-primary);
  color: #fff;
  border: none;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s;
  flex-shrink: 0;
}

.btn-primary:hover {
  background: var(--im-primary-hover);
}

/* ===== 路径文字 / 缓存大小 ===== */
.path-text {
  font-size: 12px;
  color: var(--im-text-title);
  font-family: 'Inter', 'Noto Sans SC', sans-serif;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 160px;
}

.size-text {
  font-size: 14px;
  font-weight: 500;
  color: var(--im-text-title);
}

.version-text {
  font-size: 14px;
  font-weight: 500;
  color: var(--im-text-title);
  font-family: 'Inter', 'Noto Sans SC', sans-serif;
}

/* ===== 链接文字（查看 ›） ===== */
.link-text {
  font-size: 13px;
  font-weight: 500;
  color: var(--im-primary);
  cursor: pointer;
}

/* ===== 账号分组（退出登录） ===== */
.account-card {
  margin-bottom: 8px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.account-label {
  font-size: 12px;
  font-weight: 700;
  color: var(--im-text-muted);
  letter-spacing: 0.5px;
}

.account-row {
  height: 72px;
  padding: 0 16px;
  background: var(--im-surface-2);
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.account-info {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  flex: 1;
}

.account-avatar {
  width: 44px;
  height: 44px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 1.286rem;
  font-weight: 700;
  flex-shrink: 0;
}

.account-text {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.account-name {
  font-size: 1rem;
  font-weight: 700;
  color: var(--im-text-title);
  line-height: 20px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.account-id {
  font-size: 0.857rem;
  color: var(--im-text-secondary);
  line-height: 17px;
  font-family: 'Inter', 'Noto Sans SC', sans-serif;
}

.account-signature {
  font-size: 0.786rem;
  color: var(--im-text-muted);
  line-height: 16px;
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.btn-logout {
  height: 32px;
  padding: 0 14px;
  background: var(--im-surface);
  color: var(--im-text-title);
  border: 1px solid var(--im-border);
  border-radius: 8px;
  font-family: inherit;
  font-size: 0.929rem;
  font-weight: 500;
  cursor: pointer;
  flex-shrink: 0;
  transition: background 0.15s ease, border-color 0.15s ease, color 0.15s ease;
}

.btn-logout:hover {
  background: var(--im-danger);
  border-color: var(--im-danger);
  color: #fff;
}

.btn-logout:active {
  transform: scale(0.97);
}

/* ===== 退出登录二次确认弹框 ===== */
.logout-confirm-mask {
  position: fixed;
  inset: 0;
  z-index: 1000;
  background: var(--im-overlay);
  display: flex;
  align-items: center;
  justify-content: center;
  animation: lc-mask-in 0.18s ease;
}

.logout-confirm-dialog {
  width: 360px;
  background: var(--im-surface);
  border-radius: 16px;
  padding: 28px 28px 24px;
  display: flex;
  flex-direction: column;
  align-items: center;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.18);
  text-align: center;
  animation: lc-dialog-in 0.22s cubic-bezier(0.34, 1.2, 0.64, 1);
}

.logout-confirm-icon {
  width: 52px;
  height: 52px;
  border-radius: 50%;
  background: rgba(240, 69, 69, 0.1);
  color: var(--im-danger);
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 16px;
}

.logout-confirm-title {
  margin: 0;
  font-size: 1.143rem;
  font-weight: 700;
  color: var(--im-text-title);
}

.logout-confirm-desc {
  margin: 8px 0 24px;
  font-size: 0.929rem;
  line-height: 20px;
  color: var(--im-text-secondary);
}

.logout-confirm-actions {
  width: 100%;
  display: flex;
  gap: 12px;
}

.logout-confirm-actions button {
  flex: 1;
  height: 38px;
  border-radius: 8px;
  font-family: inherit;
  font-size: 0.929rem;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.15s ease, color 0.15s ease;
}

.logout-btn-cancel {
  background: var(--im-surface-2);
  color: var(--im-text-title);
  border: 1px solid var(--im-border);
}

.logout-btn-cancel:hover {
  background: var(--im-hover-gray);
}

.logout-btn-danger {
  background: var(--im-danger);
  color: #fff;
  border: none;
}

.logout-btn-danger:hover {
  background: #e03535;
}

.logout-btn-danger:active {
  transform: scale(0.98);
}

@keyframes lc-mask-in {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

@keyframes lc-dialog-in {
  from {
    opacity: 0;
    transform: scale(0.92);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}

/* 减少动画偏好 */
@media (prefers-reduced-motion: reduce) {
  .logout-confirm-mask,
  .logout-confirm-dialog {
    animation: none !important;
  }
}

/* 触屏：加大按钮触摸目标 */
@media (hover: none) and (pointer: coarse) {
  .logout-confirm-actions button {
    min-height: 44px;
  }
}

/* ===== 关于页品牌区 ===== */
.about-brand {
  margin-top: 36px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding-bottom: 24px;
}

.brand-logo {
  width: 60px;
  height: 60px;
  border-radius: 15px;
  background: var(--im-primary);
  color: var(--im-text-title);
  font-size: 30px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 6px;
}

.brand-name {
  font-size: 18px;
  font-weight: 500;
  color: var(--im-text-title);
  line-height: 26px;
}

.brand-desc {
  font-size: 13px;
  color: var(--im-text-secondary);
  line-height: 19px;
}

.brand-copy {
  font-size: 12px;
  color: var(--im-text-muted);
  font-family: 'Inter', 'Noto Sans SC', sans-serif;
  margin-top: 4px;
}

/* ===== 触屏优化 ===== */
@media (hover: none) and (pointer: coarse) {
  .setting-row {
    min-height: 72px;
  }
  * {
    -webkit-tap-highlight-color: transparent;
  }
}

/* ===== 减少动画偏好 ===== */
@media (prefers-reduced-motion: reduce) {
  .sidebar-item,
  .toggle,
  .seg-btn,
  .select-btn,
  .btn-primary,
  .btn-outline-blue,
  .btn-primary-blue,
  .setting-row.clickable {
    transition: none !important;
  }
}

/* ===== 更改本地存储路径弹框 ===== */
.path-dialog-mask {
  position: fixed;
  inset: 0;
  z-index: 1100;
  background: var(--im-overlay);
  display: flex;
  align-items: center;
  justify-content: center;
  animation: path-mask-in 0.18s ease;
}

.path-dialog {
  width: 480px;
  max-width: calc(100vw - 48px);
  background: var(--im-surface);
  border-radius: 16px;
  padding: 24px 24px 20px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.18);
  animation: path-dialog-in 0.22s cubic-bezier(0.34, 1.2, 0.64, 1);
}

.path-dialog-icon {
  width: 44px;
  height: 44px;
  border-radius: 12px;
  background: rgba(37, 99, 235, 0.1);
  color: var(--im-primary);
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 14px;
}

.path-dialog-title {
  margin: 0;
  font-size: 1.143rem;
  font-weight: 700;
  color: var(--im-text-title);
}

.path-dialog-hint {
  margin: 6px 0 18px;
  font-size: 0.857rem;
  color: var(--im-text-secondary);
  line-height: 1.5;
}

.path-field-label {
  display: block;
  margin: 14px 0 8px;
  font-size: 0.786rem;
  font-weight: 600;
  color: var(--im-text-muted);
  letter-spacing: 0.4px;
}

.path-presets {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 168px;
  overflow-y: auto;
}

.path-preset {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 38px;
  padding: 0 12px;
  background: var(--im-surface-2);
  border: 1px solid transparent;
  border-radius: 8px;
  font-family: inherit;
  font-size: 0.857rem;
  color: var(--im-text-title);
  cursor: pointer;
  text-align: left;
  transition: background 0.15s, border-color 0.15s;
}

.path-preset:hover {
  background: var(--im-hover-gray);
}

.path-preset.active {
  background: rgba(37, 99, 235, 0.08);
  border-color: var(--im-primary);
  color: var(--im-primary);
}

.path-preset-icon {
  color: var(--im-text-secondary);
  flex-shrink: 0;
  display: flex;
  align-items: center;
}

.path-preset.active .path-preset-icon {
  color: var(--im-primary);
}

.path-preset-text {
  font-family: 'Inter', 'Noto Sans SC', sans-serif;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex: 1;
}

.path-input-row {
  width: 100%;
}

.path-input {
  width: 100%;
  height: 38px;
  padding: 0 12px;
  background: var(--im-surface-2);
  border: 1px solid var(--im-border);
  border-radius: 8px;
  font-family: 'Inter', 'Noto Sans SC', sans-serif;
  font-size: 0.857rem;
  color: var(--im-text-title);
  outline: none;
  box-sizing: border-box;
  transition: border-color 0.15s, box-shadow 0.15s;
}

.path-input:focus {
  border-color: var(--im-primary);
  box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.15);
}

.path-dialog-tip {
  margin: 10px 0 20px;
  font-size: 0.786rem;
  color: var(--im-text-muted);
}

.path-dialog-actions {
  display: flex;
  gap: 12px;
  justify-content: flex-end;
  border-top: 1px solid var(--im-border);
  padding-top: 16px;
}

.path-dialog-actions button {
  height: 36px;
  padding: 0 18px;
  border-radius: 8px;
  font-family: inherit;
  font-size: 0.929rem;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.path-btn-cancel {
  background: var(--im-surface-2);
  color: var(--im-text-title);
  border: 1px solid var(--im-border);
}

.path-btn-cancel:hover {
  background: var(--im-hover-gray);
}

.path-btn-confirm {
  background: var(--im-primary);
  color: #fff;
  border: none;
}

.path-btn-confirm:hover {
  background: var(--im-primary-hover);
}

.path-btn-confirm:active {
  transform: scale(0.98);
}

@keyframes path-mask-in {
  from { opacity: 0; }
  to { opacity: 1; }
}

@keyframes path-dialog-in {
  from { opacity: 0; transform: scale(0.92); }
  to { opacity: 1; transform: scale(1); }
}

@media (prefers-reduced-motion: reduce) {
  .path-dialog-mask,
  .path-dialog { animation: none !important; }
}

@media (hover: none) and (pointer: coarse) {
  .path-dialog-actions button { min-height: 44px; }
  .path-preset { min-height: 44px; }
}

/* ===== 小型下拉菜单（保留时长 / 导出格式） ===== */
.picker-wrap {
  position: relative;
  display: flex;
  align-items: center;
  gap: 8px;
}

.mini-menu {
  position: absolute;
  right: 0;
  top: calc(100% + 6px);
  min-width: 148px;
  background: var(--im-surface, #fff);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.16);
  padding: 4px;
  z-index: 30;
}

.mini-menu-item {
  display: block;
  width: 100%;
  text-align: left;
  padding: 8px 12px;
  border: none;
  background: none;
  border-radius: 6px;
  cursor: pointer;
  font-size: 0.9rem;
  color: var(--im-text-title, #1f2329);
}

.mini-menu-item:hover {
  background: rgba(0, 0, 0, 0.05);
}

.mini-menu-item.active {
  color: #2563eb;
}

/* 危险操作按钮（清除本账户数据） */
.btn-danger {
  padding: 6px 16px;
  border-radius: 6px;
  border: 1px solid rgba(239, 68, 68, 0.45);
  background: rgba(239, 68, 68, 0.08);
  color: #ef4444;
  font-size: 0.86rem;
  cursor: pointer;
}

.btn-danger:hover:not(:disabled) {
  background: rgba(239, 68, 68, 0.16);
}

.btn-danger:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

/* ===== 通用确认弹框 ===== */
.wc-confirm-mask {
  position: fixed;
  inset: 0;
  z-index: 2100;
  background: var(--im-overlay, rgba(0, 0, 0, 0.45));
  display: flex;
  align-items: center;
  justify-content: center;
  animation: wc-fade-in 0.18s ease;
}

.wc-confirm-dialog {
  width: 380px;
  max-width: calc(100vw - 48px);
  background: var(--im-surface, #fff);
  border-radius: 16px;
  padding: 28px 28px 24px;
  display: flex;
  flex-direction: column;
  align-items: center;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.18);
  animation: wc-pop-in 0.2s ease;
}

.wc-confirm-icon {
  width: 52px;
  height: 52px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(37, 99, 235, 0.1);
  color: #2563eb;
}

.wc-confirm-icon.danger {
  background: rgba(239, 68, 68, 0.1);
  color: #ef4444;
}

.wc-confirm-title {
  margin: 14px 0 0;
  font-size: 1.143rem;
  font-weight: 700;
  color: var(--im-text-title, #1f2329);
}

.wc-confirm-desc {
  margin: 10px 0 22px;
  text-align: center;
}

.wc-confirm-desc p {
  margin: 3px 0;
  font-size: 0.9rem;
  line-height: 1.55;
  color: var(--im-text-secondary, #646a73);
  word-break: break-all;
}

.wc-confirm-desc p.strong {
  color: #ef4444;
  font-weight: 600;
}

.wc-confirm-actions {
  display: flex;
  gap: 12px;
  width: 100%;
}

.wc-confirm-btn {
  flex: 1;
  padding: 10px 0;
  border-radius: 8px;
  font-size: 0.93rem;
  cursor: pointer;
  transition: background 0.15s ease, opacity 0.15s ease;
}

.wc-confirm-btn.cancel {
  background: var(--im-surface-2, #f5f6f7);
  color: var(--im-text-title, #1f2329);
  border: 1px solid var(--im-border, #e5e6eb);
}

.wc-confirm-btn.cancel:hover {
  background: var(--im-hover-gray, #eceef0);
}

.wc-confirm-btn.primary {
  background: #2563eb;
  color: #fff;
  border: none;
}

.wc-confirm-btn.primary:hover {
  background: #1d4fd7;
}

.wc-confirm-btn.danger {
  background: #ef4444;
  color: #fff;
  border: none;
}

.wc-confirm-btn.danger:hover {
  background: #dc2626;
}

@keyframes wc-fade-in {
  from { opacity: 0; }
  to { opacity: 1; }
}

@keyframes wc-pop-in {
  from { opacity: 0; transform: scale(0.94) translateY(6px); }
  to { opacity: 1; transform: scale(1) translateY(0); }
}

/* ===== 轻量 toast ===== */
.wc-toast {
  position: fixed;
  top: 28px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 3000;
  display: flex;
  align-items: center;
  gap: 8px;
  max-width: min(560px, calc(100vw - 48px));
  padding: 10px 18px;
  border-radius: 10px;
  background: #1f2329;
  color: #fff;
  font-size: 0.9rem;
  box-shadow: 0 8px 28px rgba(0, 0, 0, 0.24);
  animation: wc-toast-in 0.22s ease;
}

.wc-toast-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex: none;
  background: #8a919f;
}

.wc-toast.success .wc-toast-dot { background: #22c55e; }
.wc-toast.error .wc-toast-dot { background: #ef4444; }
.wc-toast.info .wc-toast-dot { background: #3b82f6; }

.wc-toast-text {
  word-break: break-all;
  line-height: 1.45;
}

@keyframes wc-toast-in {
  from { opacity: 0; transform: translate(-50%, -10px); }
  to { opacity: 1; transform: translate(-50%, 0); }
}

/* 新版本弹框：更新说明与当前版本 */
.update-notes {
  white-space: pre-wrap;
  max-height: 160px;
  overflow-y: auto;
  text-align: left;
}

.update-cur {
  margin-top: 8px;
  font-size: 0.82rem;
  opacity: 0.8;
}

@media (prefers-reduced-motion: reduce) {
  .wc-confirm-mask,
  .wc-confirm-dialog,
  .wc-toast { animation: none !important; }
}
</style>
