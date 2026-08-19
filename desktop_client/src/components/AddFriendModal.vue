<script setup>
import { ref, computed, onMounted } from 'vue'
import { friendApi } from '../api/social'

const emit = defineEmits(['close', 'friend-added'])

// 搜索关键字
const query = ref('')

// 是否已执行搜索（用于控制结果卡片显隐）
const hasSearched = ref(false)

// 是否已发送验证申请
const applied = ref(false)

// 搜索结果是否已是好友（按钮置灰为“已是好友”，后端同样拦截）
const alreadyFriend = ref(false)

// 好友请求列表数据（后端可达时用真实数据）
const requests = ref([])

const colors = ['#3399b2', '#8066cc', '#f59e0b', '#10b981', '#3b82f6', '#ec4899']

// 未处理的好友请求数量
const pendingCount = computed(() => requests.value.filter((r) => !r.accepted).length)

// 加载真实好友请求
async function loadRequests() {
  try {
    const list = await friendApi.listRequests()
    requests.value = (list || []).map((r, i) => ({
      id: r.id,
      name: r.nickname || `用户${r.from_uid}`,
      avatar: (r.nickname || '?')[0],
      color: colors[i % colors.length],
      message: r.message ? '附加消息：' + r.message : '请求添加你为好友',
      fromUid: r.from_uid,
      accepted: false,
    }))
  } catch (e) {
    // 保留空列表，控制台输出便于定位（token 失效/后端不可达等）
    console.warn('[AddFriendModal] 加载好友请求失败:', e?.message || e)
  }
}

onMounted(loadRequests)

// 搜索结果（后端按手机号/邮箱/昵称查到用户）
const searchResult = ref(null)
const searchError = ref('')
const searching = ref(false)

// 当前登录用户 uid（登录时写入 localStorage['workchat:me']），用于拦截添加自己
function meUid() {
  try {
    const me = JSON.parse(localStorage.getItem('workchat:me') || '{}')
    return me.uid || 0
  } catch {
    return 0
  }
}

// 判断 uid 是否已是好友：优先用共享缓存，未加载则先拉一次后端；查询失败不阻断（后端兜底拦截）
async function isFriend(uid) {
  try {
    const friends = friendApi.isFriendCacheLoaded() ? friendApi.getCachedFriends() : await friendApi.list(true)
    return (friends || []).some((f) => Number(f.uid) === Number(uid))
  } catch {
    return false
  }
}

// 搜索：调后端按手机号/账号查用户，拿到 uid 用于发送申请
async function doSearch() {
  const v = query.value.trim()
  if (!v) return
  hasSearched.value = true
  searchError.value = ''
  searchResult.value = null
  searching.value = true
  applied.value = false
  alreadyFriend.value = false
  try {
    const u = await friendApi.search(v)
    // 拦截添加自己（后端同样拦截，前端先行提示）
    if (u && Number(u.uid) === Number(meUid())) {
      searchError.value = '不能添加自己为好友'
      return
    }
    searchResult.value = u
    // 已是好友：禁用申请按钮
    alreadyFriend.value = await isFriend(u.uid)
  } catch (e) {
    searchError.value = e.message || '未找到该用户'
  } finally {
    searching.value = false
  }
}

// 发送验证申请（用搜索到的用户 uid；自己/已是好友/已发送时不允许点击）
async function sendApply() {
  if (!searchResult.value || applied.value || alreadyFriend.value) return
  applied.value = true
  try {
    await friendApi.sendRequest(searchResult.value.uid, '你好，我是 WorkChat 用户')
  } catch (e) {
    applied.value = false
    searchError.value = e.message || '发送失败'
  }
}

// 接受好友请求：接受成功后从列表移除，并通知父组件刷新会话/通讯录
async function acceptRequest(id) {
  const r = requests.value.find((x) => x.id === id)
  if (!r) return
  try {
    await friendApi.handleRequest(id, true)
    requests.value = requests.value.filter((x) => x.id !== id)
    emit('friend-added', { friendUid: r.fromUid, nickname: r.name })
  } catch (e) {
    console.warn('[AddFriendModal] 接受好友请求失败:', e?.message || e)
  }
}

// 拒绝好友请求（从列表移除）
async function rejectRequest(id) {
  try {
    await friendApi.handleRequest(id, false)
    requests.value = requests.value.filter((x) => x.id !== id)
  } catch {
    /* 处理失败 */
  }
}

// 复制邀请链接
function copyLink() {
  const link = 'workchat.com/i/invite'
  if (navigator.clipboard) {
    navigator.clipboard.writeText(link)
  }
  // 简单反馈：按钮文字短暂变为"已复制"
  copied.value = true
  setTimeout(() => (copied.value = false), 1500)
}

const copied = ref(false)

// 关闭弹窗（点击遮罩或右上角关闭）
function closeModal() {
  emit('close')
}
</script>

<template>
  <!-- 全屏遮罩 + 弹窗内容，淡入淡出 -->
  <div class="modal-overlay" @click.self="closeModal">
    <div class="modal">
      <!-- 弹窗头部：标题 + 关闭按钮 -->
      <header class="modal-header">
        <h2 class="modal-title">添加好友</h2>
        <button class="close-btn" aria-label="关闭" @click="closeModal">
          <svg viewBox="0 0 16 16" width="16" height="16">
            <path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
          </svg>
        </button>
      </header>

      <div class="modal-body">
        <!-- ===== 搜索行 ===== -->
        <div class="search-row">
          <div class="search-input">
            <svg viewBox="0 0 20 20" width="20" height="20" class="search-magnifier">
              <circle cx="8.67" cy="8.67" r="5.83" fill="none" stroke="currentColor" stroke-width="1.6" />
              <line x1="13.75" y1="13.75" x2="17.5" y2="17.5" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
            </svg>
            <input
              v-model="query"
              class="search-field"
              type="text"
              placeholder="输入账号 / 手机号查找好友"
              aria-label="输入账号或手机号查找好友"
              @keydown.enter="doSearch"
            />
          </div>
          <button class="btn-search" @click="doSearch">搜索</button>
        </div>

        <!-- ===== 搜索结果卡片 ===== -->
        <div v-if="hasSearched" class="result-card">
          <!-- 搜索中 -->
          <div v-if="searching" class="result-info">
            <div class="result-name">正在搜索…</div>
          </div>
          <!-- 未找到 -->
          <div v-else-if="searchError && !searchResult" class="result-info">
            <div class="result-name">{{ searchError }}</div>
          </div>
          <!-- 找到用户 -->
          <template v-else-if="searchResult">
            <div class="result-avatar" :style="{ background: '#d9804d' }">
              <span>{{ (searchResult.nickname || '?')[0] }}</span>
            </div>
            <div class="result-info">
              <div class="result-name">{{ searchResult.nickname }}</div>
              <div class="result-id">WorkChat UID: {{ searchResult.uid }} · 账号 {{ searchResult.remark }}</div>
            </div>
            <button
              class="btn-apply"
              :class="{ done: applied || alreadyFriend }"
              :disabled="applied || alreadyFriend"
              @click="sendApply"
            >
              <span>{{ alreadyFriend ? '已是好友' : applied ? '已发送' : '发送验证申请' }}</span>
            </button>
          </template>
        </div>

        <!-- ===== 分隔线 ===== -->
        <div class="divider"></div>

        <!-- ===== 邀请区 ===== -->
        <section class="invite-section">
          <h3 class="invite-title">通过二维码或链接邀请</h3>
          <div class="invite-row">
            <!-- 二维码 -->
            <div class="qr-code" aria-label="二维码">
              <svg viewBox="0 0 56 56" width="56" height="56">
                <g fill="none" stroke="currentColor" stroke-width="2.5">
                  <rect x="7" y="7" width="16.33" height="16.33" />
                  <rect x="32.67" y="7" width="16.33" height="16.33" />
                  <rect x="7" y="32.67" width="16.33" height="16.33" />
                  <path d="M32.67 32.67h4.67v4.67h-4.67z" />
                  <path d="M40.33 32.67h8.33" />
                  <path d="M32.67 40.33h8.33" />
                  <path d="M40.33 44.33h8.33v4.67h-4.67v4.67" />
                </g>
              </svg>
            </div>
            <!-- 右侧说明 -->
            <div class="invite-info">
              <p class="invite-desc">扫码或分享链接，让好友快速添加你</p>
              <div class="link-row">
                <span class="link-text">workchat.com/i/lin-2024</span>
                <button class="btn-copy" @click="copyLink">
                  <span>{{ copied ? '已复制' : '复制' }}</span>
                </button>
              </div>
            </div>
          </div>
        </section>

        <!-- ===== 好友请求 ===== -->
        <section class="requests-section">
          <h3 class="requests-title">好友请求 ({{ pendingCount }})</h3>
          <div
            v-for="r in requests"
            :key="r.id"
            class="request-item"
            :class="{ accepted: r.accepted }"
          >
            <div class="request-avatar" :style="{ background: r.color }">
              <span>{{ r.avatar }}</span>
            </div>
            <div class="request-info">
              <div class="request-name">{{ r.name }}</div>
              <div class="request-msg">{{ r.message }}</div>
            </div>
            <div class="request-actions">
              <template v-if="r.accepted">
                <span class="accepted-text">已添加</span>
              </template>
              <template v-else>
                <button class="btn-accept" @click="acceptRequest(r.id)">接受</button>
                <button class="btn-reject" @click="rejectRequest(r.id)">拒绝</button>
              </template>
            </div>
          </div>
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 全屏遮罩：半透明黑（进入/退出动画由父组件 Transition 控制） */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: var(--im-overlay);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  padding: 40px;
}

/* 弹窗容器：白色内容 */
.modal {
  width: 100%;
  max-width: 700px;
  max-height: calc(100vh - 80px);
  background: var(--im-surface);
  border-radius: 16px;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.2);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* ===== 弹窗头部 ===== */
.modal-header {
  height: 56px;
  padding: 0 20px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
  border-bottom: 1px solid var(--im-border);
}

.modal-title {
  margin: 0;
  font-size: 1.143rem;
  font-weight: 700;
  color: var(--im-text-title);
}

.close-btn {
  width: 32px;
  height: 32px;
  background: transparent;
  border: none;
  border-radius: 8px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--im-text-secondary);
  position: relative;
}

/* 关闭按钮 hover：红色小圆背景，比按钮更紧凑，避免过大色块 */
.close-btn:hover::before {
  content: '';
  position: absolute;
  inset: 4px;
  border-radius: 50%;
  background: var(--im-danger);
}

.close-btn:hover svg {
  position: relative;
  z-index: 1;
}

.close-btn:hover svg path {
  stroke: #fff;
}

/* ===== 弹窗主体 ===== */
.modal-body {
  flex: 1;
  min-height: 0;
  padding: 32px 48px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.modal-body::-webkit-scrollbar {
  width: 6px;
}

.modal-body::-webkit-scrollbar-thumb {
  background: rgba(0, 0, 0, 0.15);
  border-radius: 3px;
}

/* ===== 搜索行 ===== */
.search-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.search-input {
  flex: 1;
  height: 48px;
  padding: 0 16px;
  display: flex;
  align-items: center;
  gap: 10px;
  background: var(--im-surface);
  border: 1px solid var(--im-border);
  border-radius: 10px;
  transition: border-color 0.15s ease;
}

.search-input:focus-within {
  border-color: var(--im-primary);
}

.search-magnifier {
  flex-shrink: 0;
  color: var(--im-text-secondary);
}

.search-field {
  flex: 1;
  min-width: 0;
  border: none;
  background: transparent;
  outline: none;
  font-family: inherit;
  font-size: 1rem;
  color: var(--im-text-title);
}

.search-field::placeholder {
  color: var(--im-text-secondary);
}

.btn-search {
  width: 76px;
  height: 48px;
  background: var(--im-primary);
  color: #fff;
  border: none;
  border-radius: 10px;
  font-size: 1rem;
  font-weight: 500;
  cursor: pointer;
  flex-shrink: 0;
  font-family: inherit;
  transition: background 0.15s;
}

.btn-search:hover {
  background: var(--im-primary-hover);
}

/* ===== 结果卡片 ===== */
.result-card {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px;
  background: var(--im-card-bg);
  border: 1px solid var(--im-card-border);
  border-radius: 12px;
}

.result-avatar {
  width: 56px;
  height: 56px;
  border-radius: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 1.571rem;
  font-weight: 500;
  flex-shrink: 0;
}

.result-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.result-name {
  font-size: 1.143rem;
  font-weight: 500;
  color: var(--im-text-title);
}

.result-id {
  font-size: 0.929rem;
  color: var(--im-text-secondary);
}

.btn-apply {
  height: 36px;
  padding: 0 20px;
  background: var(--im-primary);
  color: #fff;
  border: none;
  border-radius: 8px;
  font-size: 0.929rem;
  font-weight: 500;
  cursor: pointer;
  flex-shrink: 0;
  font-family: inherit;
  transition: background 0.15s;
}

.btn-apply:hover {
  background: var(--im-primary-hover);
}

.btn-apply.done {
  background: var(--im-surface-2);
  color: var(--im-text-secondary);
  cursor: default;
}

/* ===== 分隔线 ===== */
.divider {
  height: 1px;
  background: var(--im-border);
}

/* ===== 邀请区 ===== */
.invite-title {
  margin: 0;
  font-size: 1.071rem;
  font-weight: 500;
  color: var(--im-text-title);
  margin-bottom: 12px;
}

.invite-row {
  display: flex;
  align-items: center;
  gap: 16px;
}

.qr-code {
  width: 96px;
  height: 96px;
  background: var(--im-surface);
  border: 1px solid var(--im-border);
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  color: var(--im-text-secondary);
}

.invite-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.invite-desc {
  margin: 0;
  font-size: 0.929rem;
  color: var(--im-text-secondary);
}

.link-row {
  height: 40px;
  padding: 0 14px;
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--im-input-bg);
  border-radius: 8px;
}

.link-text {
  flex: 1;
  min-width: 0;
  font-size: 0.929rem;
  color: var(--im-text-title);
  font-family: 'Inter', 'Noto Sans SC', sans-serif;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.btn-copy {
  height: 28px;
  padding: 0 14px;
  background: var(--im-soft-blue);
  color: var(--im-text-title);
  border: none;
  border-radius: 6px;
  font-size: 0.857rem;
  font-weight: 500;
  cursor: pointer;
  font-family: inherit;
  flex-shrink: 0;
  transition: background 0.15s;
}

.btn-copy:hover {
  background: var(--im-soft-blue-hover);
}

/* ===== 好友请求 ===== */
.requests-title {
  margin: 0;
  font-size: 1.071rem;
  font-weight: 500;
  color: var(--im-text-title);
}

.request-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px;
  background: var(--im-card-bg);
  border: 1px solid var(--im-card-border);
  border-radius: 12px;
  transition: opacity 0.2s ease;
}

.request-avatar {
  width: 44px;
  height: 44px;
  border-radius: 22px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 1.143rem;
  font-weight: 500;
  flex-shrink: 0;
}

.request-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.request-name {
  font-size: 1rem;
  font-weight: 500;
  color: var(--im-text-title);
}

.request-msg {
  font-size: 0.857rem;
  color: var(--im-text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.request-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.btn-accept {
  height: 32px;
  padding: 0 16px;
  background: var(--im-primary);
  color: #fff;
  border: none;
  border-radius: 8px;
  font-size: 0.929rem;
  font-weight: 500;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s;
}

.btn-accept:hover {
  background: var(--im-primary-hover);
}

.btn-reject {
  height: 32px;
  padding: 0 16px;
  background: var(--im-soft-gray);
  color: var(--im-text-title);
  border: none;
  border-radius: 8px;
  font-size: 0.929rem;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s;
}

.btn-reject:hover {
  background: var(--im-soft-gray-hover);
}

.accepted-text {
  height: 32px;
  display: flex;
  align-items: center;
  padding: 0 16px;
  font-size: 0.929rem;
  color: var(--im-online);
  font-weight: 500;
}

/* 触屏优化 */
@media (hover: none) and (pointer: coarse) {
  .btn-search,
  .btn-apply,
  .btn-accept,
  .btn-reject {
    min-height: 40px;
  }
}

/* 减少动画偏好：关闭不必要的过渡 */
@media (prefers-reduced-motion: reduce) {
  .modal-overlay,
  .modal,
  .result-card,
  .search-input {
    transition: none !important;
  }
}
</style>