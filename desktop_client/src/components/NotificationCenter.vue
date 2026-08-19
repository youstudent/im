<script setup>
import { ref, computed, onMounted } from 'vue'
import { notifyApi, friendApi } from '../api/social'

/**
 * 通知中心页面
 * ---------------- 设计稿结构 ----------------
 * 标题栏（38px）
 *   ├─ 左侧：应用图标 + WorkChat
 *   ├─ 中间：应用名
 *   └─ 右侧：窗口控制（最小化/最大化/关闭）
 * 主体区（782px）
 *   ├─ 导航栏（72px）：我的头像 / 消息 / 通讯录 / 设置 / 通知 / 创建 / 设置按钮
 *   ├─ 列分隔线（1px）
 *   ├─ 通知列表区（906px）
 *   │   ├─ 顶栏（56px）：标题 + "全部已读"按钮
 *   │   ├─ 分类标签（44px）：全部 / 未读 / @提及 / 系统
 *   │   └─ 通知列表（682px）：5 条通知 + "已显示全部通知"占位
 *   ├─ 列分隔线（1px）
 *   └─ 快捷面板（300px）
 *       ├─ 面板头部（200px）：标题 + 勿扰模式 + 桌面通知
 *       ├─ 统计卡片（268x130）：今日通知 / 12 条未读 / 较昨日 -18%
 *       ├─ 面板分隔线
 *       └─ 快捷操作卡片（268x180）：清空所有通知 / 前往通知设置 / 查看历史记录
 */

// ---- 标签分类 ----
const tabs = [
  { key: 'all', label: '全部' },
  { key: 'unread', label: '未读' },
  { key: 'mention', label: '@提及' },
  { key: 'system', label: '系统' },
]
const activeTab = ref('all')

// ---- 通知数据 ----
// type: reply | mention | system | friend | invite
// unread: true 时显示右上角红色圆点
// avatar/avatarText/avatarColor: 头像颜色与首字
// iconBg/iconChar: 系统通知使用图标方块（无文字）
// action: 'accept' 好友请求类型带"接受"按钮
const notifications = ref([])

const colors = ['#f59e0b', '#8b5cf6', '#ed4799', '#2563eb', '#f97316', '#10b981', '#22c55e']
const iconColors = { system: '#ed4799', friend: '#2563eb', invite: '#f97316' }

// 时间格式化：相对时间
function formatTime(unixSec) {
  if (!unixSec) return ''
  const diff = Math.floor(Date.now() / 1000) - unixSec
  if (diff < 60) return '刚刚'
  if (diff < 3600) return `${Math.floor(diff / 60)}分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)}小时前`
  if (diff < 86400 * 7) return `${Math.floor(diff / 86400)}天前`
  const d = new Date(unixSec * 1000)
  return `${d.getMonth() + 1}月${d.getDate()}日`
}

// 加载真实通知
async function loadNotifications() {
  try {
    const list = await notifyApi.list()
    if (!list) return
    notifications.value = list.map((n, i) => {
      const isFriend = n.type === 'friend'
      const avatarText = isFriend ? (n.summary || '友')[0] : n.type === 'system' ? '系' : (n.summary || '?')[0]
      return {
        id: n.id,
        type: n.type,
        unread: n.read === 0,
        avatarText,
        avatarColor: colors[i % colors.length],
        iconBg: n.type === 'system' ? (iconColors.system || '#ed4799') : '',
        iconChar: n.type === 'system' ? '系' : '',
        title: n.title || n.type,
        summary: n.summary || '',
        time: formatTime(n.time),
        action: '', // 取消"接受"操作按钮，仅展示通知
        reqId: isFriend ? parseReqId(n.action) : null,
      }
    })
  } catch {
    /* 后端不可达，保留空列表 */
  }
}

function parseReqId(action) {
  if (!action) return null
  try {
    const obj = JSON.parse(action)
    return obj.req_id || null
  } catch {
    return null
  }
}

onMounted(loadNotifications)

// 全部未读数量
const unreadCount = computed(() => notifications.value.filter((n) => n.unread).length)

// 按当前 tab 过滤通知
const filteredNotifications = computed(() => {
  switch (activeTab.value) {
    case 'unread':
      return notifications.value.filter((n) => n.unread)
    case 'mention':
      return notifications.value.filter((n) => n.type === 'mention' || n.type === 'invite')
    case 'system':
      return notifications.value.filter((n) => n.type === 'system')
    case 'all':
    default:
      return notifications.value
  }
})

// ---- 交互：tab 切换 ----
function selectTab(key) {
  if (activeTab.value === key) return
  activeTab.value = key
}

// ---- 交互：全部已读 ----
async function markAllRead() {
  // 仅在确实有未读时执行，避免无意义 DOM 更新
  if (!unreadCount.value) return
  try {
    await notifyApi.markAllRead()
    notifications.value = notifications.value.map((n) => ({ ...n, unread: false }))
  } catch {
    /* 失败忽略 */
  }
}

// ---- 交互：点击单条通知 ----
// 标记为已读（若是未读），并预留跳转逻辑
function openNotification(n) {
  if (n.unread) {
    n.unread = false
    notifyApi.markRead(n.id).catch(() => {})
  }
  // 真实场景下会根据 n.type 跳转聊天/系统设置/好友请求页等
}

// ---- 交互：接受好友请求 ----
async function acceptFriend(n) {
  // 接受后从列表中移除
  const idx = notifications.value.findIndex((x) => x.id === n.id)
  if (idx > -1) notifications.value.splice(idx, 1)
  try {
    await friendApi.handleRequest(n.reqId, true)
  } catch {
    /* 处理失败 */
  }
}

// ---- 交互：快捷面板 ----
const dnd = ref(true) // 勿扰模式
const desktopNotif = ref(false) // 桌面通知

function toggleDnd() {
  dnd.value = !dnd.value
}
function toggleDesktopNotif() {
  desktopNotif.value = !desktopNotif.value
}

// 清空所有通知（自定义确认弹窗）
const showClearConfirm = ref(false) // 控制确认弹窗显隐

function clearAll() {
  if (notifications.value.length === 0) return
  showClearConfirm.value = true // 打开自定义确认弹窗
}

async function confirmClear() {
  showClearConfirm.value = false
  try {
    await notifyApi.clear()
    notifications.value = []
  } catch {
    /* 失败忽略 */
  }
}

function cancelClear() {
  showClearConfirm.value = false
}

// 跳转动作（演示用）
function goSettings() {
  if (typeof window !== 'undefined') {
    window.location.hash = 'settings'
  }
}
function goHistory() {
  if (typeof window !== 'undefined') {
    window.alert('跳转到历史通知记录（演示）')
  }
}
</script>

<template>
  <div class="window">
    <main class="body">
      <!-- ============== 通知列表区 ============== -->
      <section class="list-area">
        <!-- 顶栏：标题 + 全部已读 -->
        <header class="list-header">
          <h1 class="list-title">通知中心</h1>
          <button
            class="mark-read-btn"
            type="button"
            :disabled="unreadCount === 0"
            :aria-label="`将 ${unreadCount} 条未读通知标记为已读`"
            @click="markAllRead"
          >
            全部已读
          </button>
        </header>

        <!-- 分类标签 -->
        <nav class="tabs" aria-label="通知分类">
          <button
            v-for="t in tabs"
            :key="t.key"
            class="tab"
            :class="{ active: activeTab === t.key }"
            :aria-current="activeTab === t.key ? 'page' : undefined"
            type="button"
            @click="selectTab(t.key)"
          >
            {{ t.label }}
          </button>
        </nav>

        <!-- 通知列表 -->
        <div class="list-scroll" role="list" aria-label="通知列表">
          <template v-if="filteredNotifications.length > 0">
            <div
              v-for="n in filteredNotifications"
              :key="n.id"
              class="notif"
              :class="{ unread: n.unread }"
              role="listitem"
              tabindex="0"
              @click="openNotification(n)"
              @keydown.enter.prevent="openNotification(n)"
            >
              <!-- 头像 / 系统图标 -->
              <div
                v-if="n.iconBg"
                class="avatar icon"
                :style="{ background: n.iconBg }"
                aria-hidden="true"
              >
                <span>{{ n.iconChar }}</span>
              </div>
              <div
                v-else
                class="avatar"
                :style="{ background: n.avatarColor }"
                aria-hidden="true"
              >
                <span>{{ n.avatarText }}</span>
              </div>

              <!-- 文本块 -->
              <div class="text">
                <div class="text-title">{{ n.title }}</div>
                <div class="text-summary">{{ n.summary }}</div>
              </div>

              <!-- 右侧：未读点 / 接受按钮 / 时间 -->
              <div class="meta">
                <span v-if="n.unread" class="unread-dot" aria-label="未读"></span>
                <button
                  v-if="n.action === 'accept'"
                  class="accept-btn"
                  type="button"
                  @click.stop="acceptFriend(n)"
                  aria-label="接受好友请求"
                >
                  接受
                </button>
                <span class="time">{{ n.time }}</span>
              </div>
            </div>

            <!-- 列表底部：已显示全部通知 -->
            <div class="list-end" aria-hidden="true">
              <span>— 已显示全部通知 —</span>
            </div>
          </template>

          <!-- 空态 -->
          <div v-else class="empty">
            <p class="empty-title">暂无通知</p>
            <p class="empty-sub">当前分类下还没有通知记录</p>
          </div>
        </div>
      </section>

      <div class="divider-col" aria-hidden="true"></div>

      <!-- ============== 快捷面板 ============== -->
      <aside class="panel" aria-label="快捷设置">
        <!-- 面板头部：勿扰模式 + 桌面通知 -->
        <div class="panel-head">
          <h2 class="panel-title">快捷设置</h2>

          <div class="setting-row">
            <span class="setting-label">勿扰模式</span>
            <button
              class="toggle"
              :class="{ on: dnd }"
              type="button"
              role="switch"
              :aria-checked="dnd"
              aria-label="切换勿扰模式"
              @click="toggleDnd"
            >
              <span class="toggle-dot"></span>
            </button>
          </div>

          <div class="setting-row">
            <span class="setting-label">桌面通知</span>
            <button
              class="toggle"
              :class="{ on: desktopNotif }"
              type="button"
              role="switch"
              :aria-checked="desktopNotif"
              aria-label="切换桌面通知"
              @click="toggleDesktopNotif"
            >
              <span class="toggle-dot"></span>
            </button>
          </div>

          <div class="row-divider" aria-hidden="true"></div>
        </div>

        <!-- 统计卡片 -->
        <div class="stat-card" aria-label="今日通知统计">
          <span class="stat-label">今日通知</span>
          <span class="stat-value">{{ unreadCount }} 条未读</span>
          <span class="stat-meta">较昨日 -18%</span>
        </div>

        <div class="panel-divider" aria-hidden="true"></div>

        <!-- 快捷操作卡片 -->
        <div class="action-card" aria-label="快捷操作">
          <h3 class="action-title">快捷操作</h3>

          <button
            class="action-row"
            type="button"
            :disabled="notifications.length === 0"
            @click="clearAll"
          >
            <span>清空所有通知</span>
            <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
              <path d="M6 4l4 4-4 4" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>

          <button class="action-row" type="button" @click="goSettings">
            <span>前往通知设置</span>
            <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
              <path d="M6 4l4 4-4 4" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>

          <button class="action-row" type="button" @click="goHistory">
            <span>查看历史记录</span>
            <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
              <path d="M6 4l4 4-4 4" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
        </div>
      </aside>
    </main>

    <!-- 清空确认弹窗 -->
    <Teleport to="body">
      <div
        v-if="showClearConfirm"
        class="confirm-mask"
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
        @click.self="cancelClear"
      >
          <div class="confirm-dialog">
            <div class="confirm-icon" aria-hidden="true">
              <svg viewBox="0 0 24 24" width="22" height="22">
                <path
                  d="M3.5 6.5h17M9 6.5V5a1.5 1.5 0 0 1 1.5-1.5h3A1.5 1.5 0 0 1 15 5v1.5M6 6.5l1 12a2 2 0 0 0 2 1.8h6a2 2 0 0 0 2-1.8l1-12"
                  fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"
                />
                <path d="M10 10.5v6M14 10.5v6" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
              </svg>
            </div>

            <h2 id="confirm-title" class="confirm-title">清空所有通知？</h2>
            <p class="confirm-desc">
              将永久删除当前全部 {{ notifications.length }} 条通知记录，此操作不可恢复。
            </p>

            <div class="confirm-actions">
              <button class="btn-cancel" type="button" @click="cancelClear">取消</button>
              <button class="btn-danger" type="button" @click="confirmClear">确认清空</button>
            </div>
          </div>
        </div>
    </Teleport>
  </div>
</template>

<style scoped>
.window {
  width: 100%;
  height: 100%;
  background: var(--im-chat-bg);
  font-family: var(--im-font-family);
  color: var(--im-text-title);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.body {
  flex: 1;
  display: flex;
  min-height: 0;
}

/* ============== 通知列表区 ============== */
.list-area {
  flex: 1;
  min-width: 0;
  background: var(--im-surface);
  display: flex;
  flex-direction: column;
}

/* 顶栏：标题 + 全部已读 */
.list-header {
  height: 56px;
  padding: 0 20px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
  background: var(--im-surface);
}

.list-title {
  margin: 0;
  font-size: 1.286rem; /* 18px */
  font-weight: 700;
  color: var(--im-text-title);
  line-height: 1;
}

.mark-read-btn {
  height: 32px;
  padding: 0 14px;
  background: var(--im-surface-2);
  color: var(--im-text-secondary);
  border: 1px solid var(--im-border);
  border-radius: 8px;
  font-family: inherit;
  font-size: 0.929rem; /* 13px */
  font-weight: 400;
  cursor: pointer;
  transition: background 0.15s ease, color 0.15s ease;
}

.mark-read-btn:hover:not(:disabled) {
  background: var(--im-hover-gray);
  color: var(--im-text-title);
}

.mark-read-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 分类标签 */
.tabs {
  height: 44px;
  padding: 0 16px;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  background: var(--im-surface);
}

.tab {
  height: 32px;
  padding: 0 14px;
  background: transparent;
  border: none;
  border-radius: 999px;
  font-family: inherit;
  font-size: 0.929rem; /* 13px */
  font-weight: 500;
  color: var(--im-text-secondary);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.15s ease, color 0.15s ease;
}

.tab:hover:not(.active) {
  background: var(--im-surface-2);
}

.tab.active {
  font-weight: 500;
  color: var(--im-primary);
  background: rgba(37, 99, 235, 0.1);
}

/* 列表滚动区 */
.list-scroll {
  flex: 1;
  overflow-y: auto;
  background: var(--im-surface);
  overscroll-behavior: contain;
}

.list-scroll::-webkit-scrollbar {
  width: 6px;
}
.list-scroll::-webkit-scrollbar-thumb {
  background: transparent;
  border-radius: 3px;
}
.list-scroll:hover::-webkit-scrollbar-thumb {
  background: rgba(0, 0, 0, 0.15);
}

/* 单条通知 */
.notif {
  height: 72px;
  padding: 0 20px;
  display: flex;
  align-items: center;
  gap: 12px;
  background: var(--im-surface);
  border-bottom: 1px solid var(--im-border);
  cursor: pointer;
  outline: none;
  transition: background 0.12s ease;
  user-select: none;
  -webkit-user-select: none;
}

.notif:hover {
  background: var(--im-surface-2);
}

.notif:focus-visible {
  box-shadow: inset 0 0 0 2px var(--im-primary);
}

/* 未读态：比普通项略深，深色模式下也保持可辨识 */
.notif.unread {
  background: var(--im-surface-2);
}

.notif.unread:hover {
  background: var(--im-surface-2);
  filter: brightness(1.06);
}

/* 头像 */
.avatar {
  width: 40px;
  height: 40px;
  border-radius: 999px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 1rem;
  font-weight: 500;
  flex-shrink: 0;
  position: relative;
}

/* 系统通知：方块图标 + 圆角 10px */
.avatar.icon {
  border-radius: 10px;
  font-size: 1.143rem;
  font-weight: 600;
}

/* 文本块 */
.text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  height: 100%;
  justify-content: center;
}

.text-title {
  font-size: 1rem; /* 14px */
  font-weight: 700;
  color: var(--im-text-title);
  line-height: 20px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.text-summary {
  font-size: 0.929rem; /* 13px */
  color: var(--im-text-secondary);
  line-height: 19px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 右侧：未读点 + 接受按钮 + 时间 */
.meta {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-shrink: 0;
}

.unread-dot {
  width: 8px;
  height: 8px;
  background: var(--im-danger);
  border-radius: 999px;
  flex-shrink: 0;
}

.accept-btn {
  height: 32px;
  padding: 0 16px;
  background: var(--im-primary);
  color: #fff;
  border: none;
  border-radius: 8px;
  font-family: inherit;
  font-size: 0.929rem;
  font-weight: 700;
  cursor: pointer;
  transition: background 0.15s ease;
}

.accept-btn:hover {
  background: var(--im-primary-hover);
}

.time {
  font-size: 0.857rem; /* 12px */
  color: var(--im-text-secondary);
  flex-shrink: 0;
  min-width: 50px;
  text-align: right;
}

/* 列表底部 */
.list-end {
  padding: 24px 0;
  text-align: center;
  font-size: 0.929rem;
  color: var(--im-text-secondary);
  background: var(--im-surface);
}

/* 空态 */
.empty {
  padding: 80px 24px;
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
}

.empty-title {
  margin: 0;
  font-size: 1.143rem;
  font-weight: 700;
  color: var(--im-text-title);
}

.empty-sub {
  margin: 0;
  font-size: 0.929rem;
  color: var(--im-text-secondary);
}

/* 列分隔线 */
.divider-col {
  width: 1px;
  background: var(--im-border);
  flex-shrink: 0;
}

/* ============== 快捷面板 ============== */
.panel {
  width: 300px;
  background: var(--im-surface);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
}

.panel::-webkit-scrollbar {
  width: 6px;
}
.panel::-webkit-scrollbar-thumb {
  background: transparent;
  border-radius: 3px;
}
.panel:hover::-webkit-scrollbar-thumb {
  background: rgba(0, 0, 0, 0.15);
}

/* 面板头部 */
.panel-head {
  padding: 24px 16px 16px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  flex-shrink: 0;
}

.panel-title {
  margin: 0 0 8px;
  font-size: 1.143rem; /* 16px */
  font-weight: 700;
  color: var(--im-text-title);
  line-height: 23px;
}

.setting-row {
  height: 44px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.setting-label {
  font-size: 1rem; /* 14px */
  color: var(--im-text-title);
}

/* 切换开关 */
.toggle {
  width: 36px;
  height: 20px;
  background: var(--im-primary);
  border: none;
  border-radius: 999px;
  padding: 2px;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  cursor: pointer;
  transition: background 0.2s ease, border-color 0.2s ease;
  flex-shrink: 0;
}

.toggle:not(.on) {
  background: var(--im-surface);
  border: 1px solid var(--im-border);
  justify-content: flex-start;
}

.toggle-dot {
  width: 16px;
  height: 16px;
  background: #fff;
  border-radius: 999px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
  transition: background 0.2s ease;
}

.toggle:not(.on) .toggle-dot {
  background: #fff;
}

.row-divider {
  height: 1px;
  background: var(--im-border);
  margin: 0;
}

/* 统计卡片 */
.stat-card {
  margin: 0 16px;
  width: calc(100% - 32px);
  height: 130px;
  background: var(--im-surface-2);
  border-radius: 12px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  flex-shrink: 0;
  box-sizing: border-box;
}

.stat-label {
  font-size: 0.929rem; /* 13px */
  color: var(--im-text-secondary);
}

.stat-value {
  font-size: 1.857rem; /* 26px ≈ 设计稿 28px 缩放 */
  font-weight: 700;
  color: var(--im-danger);
  letter-spacing: -0.5px;
  font-family: 'Inter', var(--im-font-family);
  line-height: 1;
}

.stat-meta {
  font-size: 0.857rem; /* 12px */
  color: var(--im-text-secondary);
}

/* 面板分隔线 */
.panel-divider {
  height: 1px;
  background: var(--im-border);
  margin: 24px 16px 0;
  flex-shrink: 0;
}

/* 快捷操作卡片 */
.action-card {
  margin: 0 16px 16px;
  width: calc(100% - 32px);
  background: var(--im-surface-2);
  border-radius: 12px;
  padding: 12px 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  flex-shrink: 0;
  box-sizing: border-box;
}

.action-title {
  margin: 0 0 8px;
  padding: 0 16px;
  font-size: 1rem; /* 14px */
  font-weight: 700;
  color: var(--im-text-title);
}

.action-row {
  height: 40px;
  background: transparent;
  border: none;
  padding: 0 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-family: inherit;
  font-size: 0.929rem; /* 13px */
  color: var(--im-text-secondary);
  cursor: pointer;
  transition: background 0.12s ease, color 0.12s ease;
}

.action-row:hover:not(:disabled) {
  background: var(--im-hover-gray);
  color: var(--im-text-title);
}

.action-row:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.action-row svg {
  color: var(--im-text-secondary);
  flex-shrink: 0;
}

/* 触屏优化 */
@media (hover: none) and (pointer: coarse) {
  .notif {
    min-height: 72px;
  }

  .mark-read-btn,
  .tab,
  .accept-btn,
  .toggle,
  .action-row {
    min-height: 40px;
  }

  .list-scroll,
  .panel {
    -webkit-overflow-scrolling: touch;
  }

  * {
    -webkit-tap-highlight-color: transparent;
  }
}

/* 减少动画偏好 */
@media (prefers-reduced-motion: reduce) {
  .notif,
  .tab,
  .mark-read-btn,
  .toggle,
  .action-row,
  .confirm-mask,
  .confirm-dialog {
    transition: none !important;
    animation: none !important;
  }
}

/* ============== 清空确认弹窗 ============== */
/* 遮罩层 */
.confirm-mask {
  position: fixed;
  inset: 0;
  z-index: 999;
  background: var(--im-overlay);
  display: flex;
  align-items: center;
  justify-content: center;
}

/* 弹窗卡片 */
.confirm-dialog {
  width: 360px;
  background: var(--im-surface);
  border-radius: 16px;
  padding: 28px 28px 24px;
  display: flex;
  flex-direction: column;
  align-items: center;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.18);
  text-align: center;
}

/* 图标 */
.confirm-icon {
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

.confirm-title {
  margin: 0;
  font-size: 1.143rem; /* 16px */
  font-weight: 700;
  color: var(--im-text-title);
}

.confirm-desc {
  margin: 8px 0 24px;
  font-size: 0.929rem; /* 13px */
  line-height: 20px;
  color: var(--im-text-secondary);
}

/* 按钮行 */
.confirm-actions {
  width: 100%;
  display: flex;
  gap: 12px;
}

.confirm-actions button {
  flex: 1;
  height: 38px;
  border-radius: 8px;
  font-family: inherit;
  font-size: 0.929rem;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.15s ease, opacity 0.15s ease, color 0.15s ease;
}

.btn-cancel {
  background: var(--im-surface-2);
  color: var(--im-text-title);
  border: 1px solid var(--im-border);
}

.btn-cancel:hover {
  background: var(--im-hover-gray);
}

.btn-danger {
  background: var(--im-danger);
  color: #fff;
  border: none;
}

.btn-danger:hover {
  background: #e03535;
  opacity: 0.92;
}

.btn-danger:active {
  transform: scale(0.98);
}

/* 进入动画：遮罩淡入 + 卡片缩放入场 */
.confirm-mask {
  animation: mask-in 0.18s ease;
}

.confirm-dialog {
  animation: dialog-in 0.22s cubic-bezier(0.34, 1.2, 0.64, 1);
}

@keyframes mask-in {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

@keyframes dialog-in {
  from {
    opacity: 0;
    transform: scale(0.92);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}

/* 触屏：加大按钮触摸目标 */
@media (hover: none) and (pointer: coarse) {
  .confirm-actions button {
    min-height: 44px;
  }
}
</style>
