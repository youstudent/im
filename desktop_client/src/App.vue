<script setup>
import { ref, computed, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { tokenStore } from './api/token'
import { authApi } from './api/auth'
import { trySilentRefresh } from './api/http'
import { checkLatestVersion, getAppVersion, openDownloadUrl, autoUpdateAvailable, onInstallProgress, downloadAndInstall } from './api/version'
import { wsClient } from './api/ws'
import { friendApi } from './api/social'
import { localdb } from './api/localdb'
import NavRail from './components/NavRail.vue'
import MainWindow from './components/MainWindow.vue'
import ContactsWindow from './components/ContactsWindow.vue'
import AddFriendModal from './components/AddFriendModal.vue'
import SettingsWindow from './components/SettingsWindow.vue'
import NotificationCenter from './components/NotificationCenter.vue'
import LoginWindow from './components/LoginWindow.vue'
import RegisterWindow from './components/RegisterWindow.vue'

// 当前激活的页面：messages | contacts | settings | notifications
const activePage = ref('messages')

// 聊天未读总数（左侧导航栏消息按钮气泡）
const chatBadge = ref(0)

// 新的好友申请提醒（左侧导航栏"新建/添加好友"按钮上的红点）
const addFriendBadge = ref(false)

// 红点由 WS 实时推送驱动：收到 friend.request 事件时显示，打开添加好友弹窗时清除。
// 不再主动请求 /friends/requests（按需加载）。

// 监听 WS social 事件：收到新的好友申请时显示红点（含对方在线时实时推送）
let wsSocialUnsub = null
function setupWsBadge() {
  if (wsSocialUnsub) return
  wsSocialUnsub = wsClient.on('social', (body) => {
    if (body && body.event === 'friend.request') {
      addFriendBadge.value = true
    }
  })
}

// "添加好友"弹窗开关
const showAddFriend = ref(false)

// "设置窗口"弹窗开关：点击底部 + 按钮下方的齿轮按钮打开
const showSettings = ref(false)

// "查找聊天记录"弹窗开关：点击聊天头部搜索按钮 / 资料面板对应行打开
const showSearchHistory = ref(false)

// 待打开的消息会话 id：通讯录点击"发消息"后，切到消息页并打开指定会话
const openConversation = ref(null)

// 通讯录"发消息"/"进入"：切换到消息页，并让 MainWindow 打开对应会话
// 先置空再赋值：同一联系人重复点击时也能触发 MainWindow 的 watch 重新跳转
function handleSendMessage(id) {
  openConversation.value = null
  nextTick(() => {
    openConversation.value = id
  })
  activePage.value = 'messages'
  if (typeof window !== 'undefined') {
    window.location.hash = 'messages'
  }
}

// 群创建成功：切换到消息页，MainWindow 的 onActivated 会重新拉取会话列表（含新群）
function handleGroupCreated() {
  activePage.value = 'messages'
  if (typeof window !== 'undefined') {
    window.location.hash = 'messages'
  }
}

// ===== 登录状态 =====
// 未登录时，整个 App 渲染 LoginWindow，登录成功后切回主框架
const isLoggedIn = ref(false)

// 未登录时展示的认证页：login | register
const authView = ref('login')

// 登录失效提示（接口 401 / WS 断开 / 被管理员强制下线）：弹框提示，2 秒后跳登录页
const authExpired = ref(false)
const authExpiredText = ref('登录已失效')
const authExpiredSub = ref('请重新登录')
let authExpiredTimer = null
let isManualLogout = false // 主动退出登录时不弹"登录失效"提示

// 监听登录失效事件（由 http.js / ws.js 派发）
function onAuthExpired(e) {
  if (isManualLogout) return
  if (authExpired.value) return
  authExpired.value = true
  // 支持被强制下线（如账号被禁用）时展示具体原因
  const detail = e && e.detail ? e.detail : {}
  if (detail.reason) {
    authExpiredText.value = detail.reason
    authExpiredSub.value = '如需恢复使用，请联系管理员'
  } else {
    authExpiredText.value = '登录已失效'
    authExpiredSub.value = '请重新登录'
  }
  // 清空本地登录态，并关闭当前账户的本地库会话（防止旧会话继续写入）
  wsClient.disconnect() // 主动断开 WS 并禁止重连，不依赖服务端踢线
  if (typeof window !== 'undefined') {
    tokenStore.clear()
    localStorage.removeItem('workchat:me')
  }
  friendApi.clearAccountCache()
  localdb.session.close()
  // 2 秒后跳转登录页
  if (authExpiredTimer) clearTimeout(authExpiredTimer)
  authExpiredTimer = setTimeout(() => {
    authExpired.value = false
    isLoggedIn.value = false
    authView.value = 'login'
  }, 2000)
}

// 启动时若本地有持久化令牌，直接恢复登录态（实现重启后保持登录）
async function restoreSession() {
  try {
    const refresh = await tokenStore.getRefreshToken()
    if (refresh) {
      isLoggedIn.value = true
      // 恢复登录态：同步打开当前账户的本地库（秒开依赖）
      await openStoreSession()
      // 已登录：监听 WS 实时推送（不主动请求好友申请，红点在收到推送/用到时才更新）
      setupWsBadge()
      // 静默刷新：长时间关机后 access token（2h）已过期，先换新再让 WS/接口使用；
      // 失败（离线）不阻断，接口层 401 时会自动重试刷新
      trySilentRefresh()
    }
  } catch {
    /* 读取失败则走登录页 */
  }
}

// 打开当前登录账户的本地库会话（uid 取自 workchat:me；幂等）
async function openStoreSession() {
  try {
    const me = JSON.parse(localStorage.getItem('workchat:me') || 'null')
    if (me && me.uid) await localdb.session.open(me.uid)
  } catch {
    /* 本地库不可用不影响主流程 */
  }
}

function handleLoggedIn(user, hasPendingFriendRequest = false) {
  isLoggedIn.value = true
  // 本次为新的登录会话，重置主动退出标志（退出时保持到下次登录，避免 WS 断开误报）
  isManualLogout = false
  // 可选：后续把 user 写入全局状态/本地缓存
  if (user && typeof window !== 'undefined') {
    localStorage.setItem('workchat:me', JSON.stringify(user))
  }
  // 多账户隔离：打开该账户的本地库（登录会话）
  if (user && user.uid) {
    localdb.session.open(user.uid)
  }
  // 登录接口返回是否有待处理好友申请，直接初始化导航栏红点（无需额外请求 /friends/requests）
  addFriendBadge.value = !!hasPendingFriendRequest
  // 登录成功：监听 WS 实时推送
  setupWsBadge()
}

// 登录/注册页之间切换
function switchAuthView(view) {
  authView.value = view
}

// 添加好友接受事件：广播给 MainWindow 刷新会话/通讯录
function onFriendAdded(payload) {
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent('wc:friend-added', { detail: payload }))
  }
}

// 设置页"退出登录"：通知服务端撤销 refresh + 清空本地令牌 + 切回登录页面
async function handleLogout() {
  isManualLogout = true // 主动退出，不弹"登录失效"提示
  try {
    const refresh = await tokenStore.getRefreshToken()
    if (refresh) {
      await authApi.logout(refresh).catch(() => {})
    }
  } catch {
    /* 退出接口失败不阻塞本地登出 */
  } finally {
    // 确定性清理：断开 WS 并禁止重连（退出接口失败时服务端不会踢线，
    // 不能依赖后端断连，否则已退出账户的长连接仍保持鉴权并可继续收推送）
    wsClient.disconnect()
    tokenStore.clear()
    if (typeof window !== 'undefined') localStorage.removeItem('workchat:me')
    // 关闭当前账户本地库会话 + 清理账户级联系人缓存（DB 文件保留，下次登录仍秒开）
    friendApi.clearAccountCache()
    localdb.session.close()
    showSettings.value = false
    isLoggedIn.value = false
    // 注意：不在此时重置 isManualLogout。
    // WS 断开事件是异步触发的（后端撤销 refresh 后断开连接），
    // 需在下次登录成功时重置，避免退出后的 WS onclose 误触发"登录失效"弹框。
  }
}

// 支持 URL hash 路由：#messages → 聊天页, #contacts → 通讯录页
// 方便在不同设计稿页面之间快速切换验证。
function readHash() {
  const h = typeof window !== 'undefined' ? window.location.hash || '' : ''
  if (h.includes('contacts')) return 'contacts'
  if (h.includes('settings')) return 'settings'
  if (h.includes('notifications')) return 'notifications'
  return 'messages'
}

// 导航：更新激活页 + 同步 URL hash
function navigate(key) {
  activePage.value = key
  if (typeof window !== 'undefined') {
    window.location.hash = key
  }
}

// 打开设置窗口（由 NavRail 触发）
function openSettings() {
  showSettings.value = true
}

// 打开"添加好友"弹窗：清除红点
function openAddFriend() {
  showAddFriend.value = true
  addFriendBadge.value = false
}

// ===== 启动自动检查更新：每次启动执行一次，发现新版本弹框提示（静默，失败不打扰） =====
const appUpdate = ref(null) // 新版本信息：{ version, download_url, release_notes }
const appVersionCur = ref('')
const canAutoInstall = autoUpdateAvailable() // 支持一键自动安装（Electron + Windows）
const installing = ref(false) // 下载并安装中
const installProgress = ref(0)
async function autoCheckUpdate() {
  try {
    const cur = await getAppVersion()
    appVersionCur.value = cur
    const r = await checkLatestVersion(cur)
    if (r.hasNew && r.latest) appUpdate.value = r.latest
  } catch {
    /* 静默降级 */
  }
}
function closeAppUpdate() {
  appUpdate.value = null
}
// 下载并安装：支持自动安装时走静默安装流程（应用自动退出）；否则兜底浏览器下载
async function downloadAppUpdate() {
  const url = appUpdate.value && appUpdate.value.download_url
  if (!url || installing.value) return
  if (!canAutoInstall) {
    openDownloadUrl(url)
    appUpdate.value = null
    return
  }
  installing.value = true
  installProgress.value = 0
  const r = await downloadAndInstall(url, appUpdate.value && appUpdate.value.sha256)
  if (!r || !r.ok) {
    installing.value = false
    // 摘要校验失败（可能被篡改/投毒）：不降级到浏览器下载，只提示风险
    if (r && String(r.error || '').includes('摘要')) {
      alert('安装包校验失败，已中止更新。更新源可能存在风险，请通过官方渠道核实。')
      appUpdate.value = null
      return
    }
    // 自动安装失败：兜底浏览器下载
    openDownloadUrl(url)
    appUpdate.value = null
  }
  // 成功时应用即将退出，无需关闭弹框
}

onMounted(() => {
  // 恢复登录态，登录成功后（restoreSession 内部）再初始化好友红点 + WS 监听
  restoreSession()
  // 延迟自动检查更新，避开启动高峰
  setTimeout(autoCheckUpdate, 2500)
  // 注册更新下载进度回调
  if (canAutoInstall) {
    onInstallProgress((p) => {
      installProgress.value = p.percent || 0
    })
  }
  activePage.value = readHash()
  // 支持 #addfriend 打开"添加好友"弹窗（便于演示/截图验证）
  if (typeof window !== 'undefined' && /#addfriend/.test(window.location.hash)) {
    showAddFriend.value = true
  }
  // 支持 #settings 打开"设置窗口"弹窗（便于演示/截图验证）
  // 注意：与左侧导航的 #settings 路由（会切换到 settings 页）冲突，
  // 这里通过判断是否在 hash 中同时出现 show/modal 等关键词来区分弹窗模式。
  if (typeof window !== 'undefined' && /#settings.*(show|modal|window)/.test(window.location.hash)) {
    showSettings.value = true
  }
  // 支持 #search 打开"查找聊天记录"弹窗（便于演示/截图验证）
  if (typeof window !== 'undefined' && /#search/.test(window.location.hash)) {
    showSearchHistory.value = true
  }
  // 监听登录失效事件（接口 401 / WS 断开）
  if (typeof window !== 'undefined') {
    window.addEventListener('auth:expired', onAuthExpired)
  }
})

onBeforeUnmount(() => {
  if (typeof window !== 'undefined') {
    window.removeEventListener('auth:expired', onAuthExpired)
  }
  if (authExpiredTimer) clearTimeout(authExpiredTimer)
})

// 根据激活页渲染对应的窗口组件
const currentView = computed(() => {
  switch (activePage.value) {
    case 'contacts':
      return ContactsWindow
    case 'notifications':
      return NotificationCenter
    case 'settings':
    default:
      return MainWindow
  }
})
</script>

<template>
  <div class="app">
    <!-- 已登录：主框架（导航栏 + 当前页） -->
    <div v-if="isLoggedIn" class="window-frame">
      <!-- 共享窗口框架：左侧导航栏 + 页面内容
           使用 KeepAlive 缓存各页面组件，切换页面后再返回时保留滚动位置等状态 -->
      <NavRail
        :active="activePage"
        :badge="chatBadge"
        :add-friend-badge="addFriendBadge"
        @navigate="navigate"
        @create="openAddFriend"
        @open-settings="openSettings"
      />
      <KeepAlive>
        <component
          :is="currentView"
          class="frame-view"
          :show-search-history="showSearchHistory"
          :open-conversation="openConversation"
          :chat-badge="chatBadge"
          @update:show-search-history="showSearchHistory = $event"
          @update:chat-badge="chatBadge = $event"
          @send-message="handleSendMessage"
          @group-created="handleGroupCreated"
        />
      </KeepAlive>
    </div>

    <!-- 未登录：渲染登录/注册窗口，覆盖整个 App -->
    <LoginWindow
      v-else-if="authView === 'login'"
      class="login-view"
      @switch="switchAuthView"
      @logged-in="handleLoggedIn"
    />
    <RegisterWindow
      v-else
      class="login-view"
      @switch="switchAuthView"
      @logged-in="handleLoggedIn"
    />

    <!-- 添加好友弹窗：点击底部"新建"按钮打开 -->
    <AddFriendModal v-if="showAddFriend" @close="showAddFriend = false" @friend-added="onFriendAdded" />

    <!-- 设置窗口弹窗：点击底部"+"下方齿轮按钮打开 -->
    <SettingsWindow v-if="showSettings" @close="showSettings = false" @logout="handleLogout" />

    <!-- 登录失效弹框：接口 401 / WS 断开触发，2 秒后跳登录页 -->
    <div v-if="authExpired" class="auth-expired-overlay">
      <div class="auth-expired-modal">
        <div class="auth-expired-icon">
          <svg viewBox="0 0 24 24" width="28" height="28">
            <path d="M12 3a9 9 0 1 0 9 9" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
            <path d="M12 8v5M12 17h.01" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
          </svg>
        </div>
        <p class="auth-expired-text">{{ authExpiredText }}</p>
        <p class="auth-expired-sub">{{ authExpiredSub }}</p>
      </div>
    </div>

    <!-- 启动自动检查更新：发现新版本弹框 -->
    <div v-if="appUpdate" class="app-update-overlay" @click.self="closeAppUpdate">
      <div class="app-update-modal">
        <div class="app-update-icon">
          <svg viewBox="0 0 24 24" width="26" height="26">
            <path d="M12 16V5" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
            <path d="m7 10 5-5 5 5" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
            <path d="M4 17v2a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-2" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
          </svg>
        </div>
        <h2 class="app-update-title">发现新版本 v{{ appUpdate.version }}</h2>
        <p v-if="appUpdate.release_notes" class="app-update-notes">{{ appUpdate.release_notes }}</p>
        <p v-else class="app-update-notes">新版本已发布，建议下载更新。</p>
        <p class="app-update-cur">当前版本：v{{ appVersionCur }}</p>
        <div class="app-update-actions">
          <button class="app-update-btn cancel" type="button" @click="closeAppUpdate">稍后</button>
          <button class="app-update-btn primary" type="button" :disabled="installing" @click="downloadAppUpdate">
            <template v-if="installing">下载中 {{ installProgress }}%</template>
            <template v-else>{{ canAutoInstall ? '下载并更新' : '前往下载' }}</template>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.app {
  width: 100vw;
  height: 100vh;
  background: var(--im-bg-app);
  display: flex;
  align-items: stretch;
  justify-content: stretch;
  padding: 0;
}

/* 窗口容器：左侧导航栏 + 右侧页面（无标题栏） */
.window-frame {
  flex: 1;
  display: flex;
  min-width: 0;
  min-height: 0;
  background: var(--im-surface);
  border-radius: var(--im-radius-window);
  overflow: hidden;
}

.frame-view {
  flex: 1;
  min-width: 0;
}

/* 未登录时渲染的登录窗口：撑满整个 App 容器 */
.login-view {
  flex: 1;
  width: 100%;
  height: 100%;
  display: flex;
  align-items: stretch;
  justify-content: stretch;
}

/* 登录失效弹框 */
.auth-expired-overlay {
  position: fixed;
  inset: 0;
  z-index: 5000;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
}

.auth-expired-modal {
  background: var(--im-surface);
  border-radius: 16px;
  padding: 32px 48px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.3);
  animation: authExpiredIn 0.25s ease;
}

@keyframes authExpiredIn {
  from {
    opacity: 0;
    transform: scale(0.92);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}

.auth-expired-icon {
  width: 56px;
  height: 56px;
  border-radius: 50%;
  background: rgba(239, 68, 68, 0.12);
  color: #ef4444;
  display: flex;
  align-items: center;
  justify-content: center;
}

.auth-expired-text {
  margin: 8px 0 0;
  font-size: 1.143rem;
  font-weight: 600;
  color: var(--im-text-title);
}

.auth-expired-sub {
  margin: 0;
  font-size: 0.857rem;
  color: var(--im-text-secondary);
}

/* ===== 启动检查更新弹框 ===== */
.app-update-overlay {
  position: fixed;
  inset: 0;
  z-index: 5000;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  animation: authExpiredIn 0.2s ease;
}

.app-update-modal {
  width: 400px;
  max-width: calc(100vw - 48px);
  background: var(--im-surface);
  border-radius: 16px;
  padding: 28px 28px 24px;
  display: flex;
  flex-direction: column;
  align-items: center;
  box-shadow: 0 16px 48px rgba(0, 0, 0, 0.24);
}

.app-update-icon {
  width: 52px;
  height: 52px;
  border-radius: 50%;
  background: rgba(37, 99, 235, 0.1);
  color: #2563eb;
  display: flex;
  align-items: center;
  justify-content: center;
}

.app-update-title {
  margin: 14px 0 0;
  font-size: 1.143rem;
  font-weight: 700;
  color: var(--im-text-title);
}

.app-update-notes {
  margin: 10px 0 0;
  font-size: 0.9rem;
  line-height: 1.55;
  color: var(--im-text-secondary);
  white-space: pre-wrap;
  max-height: 160px;
  overflow-y: auto;
  text-align: center;
}

.app-update-cur {
  margin: 8px 0 18px;
  font-size: 0.82rem;
  color: var(--im-text-secondary);
  opacity: 0.8;
}

.app-update-actions {
  display: flex;
  gap: 12px;
  width: 100%;
}

.app-update-btn {
  flex: 1;
  padding: 10px 0;
  border-radius: 8px;
  font-size: 0.93rem;
  cursor: pointer;
}

.app-update-btn.cancel {
  background: var(--im-surface-2, #f5f6f7);
  color: var(--im-text-title);
  border: 1px solid var(--im-border, #e5e6eb);
}

.app-update-btn.primary {
  background: #2563eb;
  color: #fff;
  border: none;
}

.app-update-btn.primary:hover {
  background: #1d4fd7;
}
</style>