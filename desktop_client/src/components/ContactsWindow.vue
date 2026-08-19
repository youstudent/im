<script setup>
import { ref, computed, onActivated } from 'vue'
import { friendApi, groupApi } from '../api/social'

// 创建群聊入口已移至会话列表搜索框旁“+”菜单（MainWindow），本组件不再持有弹窗
const emit = defineEmits(['send-message'])

// 当前选中的通讯录项 id：默认详情面板展示第一个好友
const selectedDetailId = ref('')

// 通讯录数据：完全由后端接口驱动（好友/群）
const friends = ref([])
const groups = ref([])

const colors = ['#f59e0b', '#10b981', '#3b82f6', '#8b5cf6', '#ec4899', '#ef4444', '#0ea5e9', '#a855f7']

// 加载真实好友与群（通讯录页是 friends/groups 的加载源头，进入时强制刷新缓存）
async function loadRealData() {
  let flist
  try {
    flist = await friendApi.list(true)
  } catch {
    return
  }
  if (flist && flist.length) {
    friends.value = flist.map((f, i) => ({
      id: String(f.uid),
      // 列表展示名：有备注展示备注，无备注回落昵称
      name: f.remark || f.nickname || `用户${f.uid}`,
      nickname: f.nickname || `用户${f.uid}`,
      remark: f.remark || '',
      avatar: f.avatar ? f.avatar[0] : (f.nickname || '?')[0],
      color: colors[i % colors.length],
      signature: '', // 好友列表只展示一个名字（备注优先，无备注回落昵称），不展示副标题/ID
      workchatId: String(f.uid),
      uid: f.uid,
    }))
  }
  // 群列表
  try {
    const glist = await groupApi.list(true)
    if (glist && glist.length) {
      groups.value = glist.map((g, i) => ({
        id: String(g.g_uid),
        name: g.name,
        avatar: (g.name || '群')[0],
        color: colors[(i + 3) % colors.length],
        count: g.member_count || 0,
        gUid: g.g_uid,
      }))
    }
  } catch {
    /* 群加载失败忽略 */
  }
  if (!selectedDetailId.value && friends.value.length) {
    selectedDetailId.value = friends.value[0].id
  }
}

// 每次进入通讯录页面（含 KeepAlive 缓存后的重新激活）都从后端重新拉取，不使用本地缓存。
// Vue 3 KeepAlive 下组件首次挂载也会触发 onActivated，因此不再额外监听 onMounted，
// 否则首次点击通讯录会同时触发两个钩子，导致 /friends 和 /groups 各被请求 2 次。
onActivated(loadRealData)

// 按当前选中项动态计算详情面板内容（好友：头像/昵称/备注/ID；群：头像/群名/群号）
const currentDetail = computed(() => {
  const friend = friends.value.find((f) => f.id === selectedDetailId.value)
  if (friend) {
    return {
      type: 'friend',
      name: friend.name,
      nickname: friend.nickname || '',
      remark: friend.remark || '',
      avatar: friend.avatar,
      color: friend.color,
      workchatId: friend.workchatId,
    }
  }
  const group = groups.value.find((g) => g.id === selectedDetailId.value)
  if (group) {
    return { type: 'group', name: group.name, nickname: '', remark: '', avatar: group.avatar, color: group.color, workchatId: String(group.gUid || group.id), count: group.count }
  }
  const fallback = friends.value[0]
  return fallback
    ? { type: 'friend', name: fallback.name, nickname: fallback.nickname || '', remark: fallback.remark || '', avatar: fallback.avatar, color: fallback.color, workchatId: fallback.workchatId }
    : { type: 'empty', name: '暂无联系人', nickname: '', remark: '', avatar: '?', color: '#cbd5e1', workchatId: '' }
})

// ===== 列表搜索：纯前端过滤（好友按名字/昵称/备注，群按群名），不改变数据加载逻辑 =====
const searchKw = ref('')
const filteredFriends = computed(() => {
  const kw = searchKw.value.trim().toLowerCase()
  if (!kw) return friends.value
  return friends.value.filter(
    (f) =>
      f.name.toLowerCase().includes(kw) ||
      (f.nickname || '').toLowerCase().includes(kw) ||
      (f.remark || '').toLowerCase().includes(kw)
  )
})
const filteredGroups = computed(() => {
  const kw = searchKw.value.trim().toLowerCase()
  if (!kw) return groups.value
  return groups.value.filter((g) => (g.name || '').toLowerCase().includes(kw))
})

// ===== 分组折叠：好友/我的群组可点击标题收起展开 =====
const friendsCollapsed = ref(false)
const groupsCollapsed = ref(false)

function selectItem(id) {
  selectedDetailId.value = id
}

function sendMessage(id) {
  emit('send-message', id)
}
</script>

<template>
  <div class="window">
    <main class="body">
      <!-- ===== 左栏：搜索 + 通讯录列表 ===== -->
      <section class="contacts-side">
        <div class="side-search">
          <svg viewBox="0 0 16 16" width="14" height="14" class="side-search-icon">
            <circle cx="6.67" cy="6.67" r="4.67" fill="none" stroke="currentColor" stroke-width="1.4" />
            <line x1="10.67" y1="10.67" x2="13.33" y2="13.33" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
          </svg>
          <input
            v-model="searchKw"
            class="side-search-input"
            type="text"
            placeholder="搜索"
            aria-label="搜索联系人或群聊"
          />
        </div>

        <div class="side-scroll">
          <!-- 好友分组：标题可点击折叠 -->
          <div
            class="group-title clickable"
            role="button"
            tabindex="0"
            :aria-expanded="!friendsCollapsed"
            @click="friendsCollapsed = !friendsCollapsed"
            @keydown.enter.prevent="friendsCollapsed = !friendsCollapsed"
          >
            <svg class="chevron" :class="{ collapsed: friendsCollapsed }" viewBox="0 0 16 16" width="12" height="12">
              <path d="M4 6l4 4 4-4" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            <span>好友 ({{ friends.length }})</span>
          </div>

          <!-- 好友项（折叠时不渲染） -->
          <template v-if="!friendsCollapsed">
            <div
              v-for="f in filteredFriends"
              :key="f.id"
              class="contact-item"
              :class="{ active: selectedDetailId === f.id }"
              role="button"
              tabindex="0"
              @click="selectItem(f.id)"
              @keydown.enter.prevent="selectItem(f.id)"
            >
              <div class="avatar" :style="{ background: f.color }">
                <span>{{ f.avatar }}</span>
              </div>
              <div class="contact-text">
                <div class="contact-name">{{ f.name }}</div>
                <div v-if="f.signature" class="contact-signature">{{ f.signature }}</div>
              </div>
            </div>
            <div v-if="searchKw && !filteredFriends.length" class="side-empty">没有匹配的好友</div>
          </template>

          <!-- 群组分组：标题可点击折叠（创建群聊入口已移至会话列表“+”菜单） -->
          <div
            class="group-title clickable"
            role="button"
            tabindex="0"
            :aria-expanded="!groupsCollapsed"
            @click="groupsCollapsed = !groupsCollapsed"
            @keydown.enter.prevent="groupsCollapsed = !groupsCollapsed"
          >
            <svg class="chevron" :class="{ collapsed: groupsCollapsed }" viewBox="0 0 16 16" width="12" height="12">
              <path d="M4 6l4 4 4-4" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            <span>我的群组 ({{ groups.length }})</span>
          </div>

          <!-- 群组项（折叠时不渲染） -->
          <template v-if="!groupsCollapsed">
            <div
              v-for="g in filteredGroups"
              :key="g.id"
              class="contact-item"
              :class="{ active: selectedDetailId === g.id }"
              role="button"
              tabindex="0"
              @click="selectItem(g.id)"
              @keydown.enter.prevent="selectItem(g.id)"
            >
              <div class="avatar" :style="{ background: g.color }">
                <span>{{ g.avatar }}</span>
              </div>
              <div class="contact-text">
                <div class="contact-name">{{ g.name }}</div>
                <div class="contact-signature">{{ g.count }} 人</div>
              </div>
            </div>
            <div v-if="searchKw && !filteredGroups.length" class="side-empty">没有匹配的群聊</div>
          </template>
        </div>
      </section>

      <!-- 列分隔线 -->
      <div class="divider-col"></div>

      <!-- ===== 右栏：灰色底 + 居中白色资料卡片 ===== -->
      <aside class="detail-area">
        <div class="detail-card">
          <!-- 卡片头：头像 + 名称 + ID（横向排布，参考微信资料页） -->
          <div class="card-head">
            <div class="avatar card-avatar" :style="{ background: currentDetail.color }">
              <span>{{ currentDetail.avatar }}</span>
            </div>
            <div class="card-head-text">
              <div class="card-name">{{ currentDetail.name }}</div>
              <div class="card-id">
                {{ currentDetail.type === 'group' ? 'WorkChat 群号：' + (currentDetail.workchatId || '') : 'WorkChat ID：' + currentDetail.workchatId }}
              </div>
            </div>
          </div>

          <!-- 资料信息行：好友展示昵称/备注；群展示群成员数 -->
          <template v-if="currentDetail.type === 'friend'">
            <div class="card-row">
              <span class="card-row-label">昵称</span>
              <span class="card-row-value">{{ currentDetail.nickname || '—' }}</span>
            </div>
            <div v-if="currentDetail.remark" class="card-row">
              <span class="card-row-label">备注</span>
              <span class="card-row-value">{{ currentDetail.remark }}</span>
            </div>
          </template>
          <template v-else-if="currentDetail.type === 'group'">
            <div class="card-row">
              <span class="card-row-label">群成员</span>
              <span class="card-row-value">{{ currentDetail.count || 0 }} 人</span>
            </div>
          </template>

          <!-- 底部功能区：发消息 + 语音/视频（占位，与聊天页资料面板能力一致） -->
          <div v-if="currentDetail.type !== 'empty'" class="card-actions">
            <button class="action-btn primary" @click="sendMessage(selectedDetailId)">
              <span class="action-icon">
                <svg viewBox="0 0 24 24" width="22" height="22">
                  <path d="M4 5a4 4 0 0 1 4-4h8a4 4 0 0 1 4 4v8a4 4 0 0 1-4 4H9l-4 4v-4a4 4 0 0 1-1-2V5z" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
                </svg>
              </span>
              <span class="action-label">{{ currentDetail.type === 'group' ? '进入群聊' : '发消息' }}</span>
            </button>
            <button class="action-btn" aria-label="语音聊天">
              <span class="action-icon">
                <svg viewBox="0 0 24 24" width="22" height="22">
                  <path d="M4.5 6.5c0-1.7 1.3-3 3-3 1 0 1.8.5 2.4 1.3l1.2 1.6c.3.4.3.9 0 1.3l-1.4 1.7a10 10 0 0 0 4.4 4.4l1.7-1.4c.4-.3.9-.3 1.3 0l1.6 1.2c.8.6 1.3 1.4 1.3 2.4 0 1.7-1.3 3-3 3h-.5c-7-1-12.6-6.6-13.5-13.5v-.5z" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
                </svg>
              </span>
              <span class="action-label">语音聊天</span>
            </button>
            <button class="action-btn" aria-label="视频聊天">
              <span class="action-icon">
                <svg viewBox="0 0 24 24" width="22" height="22">
                  <rect x="3" y="6" width="13" height="12" rx="2.5" fill="none" stroke="currentColor" stroke-width="1.6" />
                  <path d="M16 10.5l4-2.5v8l-4-2.5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
                </svg>
              </span>
              <span class="action-label">视频聊天</span>
            </button>
          </div>

          <!-- 空态：无任何联系人 -->
          <div v-if="currentDetail.type === 'empty'" class="card-empty">
            <p>还没有联系人，先去添加好友吧</p>
          </div>
        </div>
      </aside>
    </main>
  </div>
</template>

<style scoped>
/* 复用现有 token 的整体外观
   注意：窗口圆角已由 App.vue 的 .window-frame 统一设置，这里不再叠加。 */
.window {
  width: 100%;
  height: 100%;
  background: var(--im-surface);
  font-family: var(--im-font-family);
  color: var(--im-text-title);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* ========== 主体 ========== */
.body {
  flex: 1;
  display: flex;
  min-height: 0;
}

/* ========== 左栏：搜索 + 列表 ========== */
.contacts-side {
  width: 280px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  min-height: 0;
  background: var(--im-surface);
}

/* 搜索框 */
.side-search {
  margin: 12px 12px 8px;
  height: 36px;
  padding: 0 10px;
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--im-surface-2);
  border-radius: 8px;
  flex-shrink: 0;
}

.side-search-icon {
  color: var(--im-text-muted);
  flex-shrink: 0;
}

.side-search-input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  font-family: inherit;
  font-size: 0.929rem;
  color: var(--im-text-title);
}

.side-search-input::placeholder {
  color: var(--im-text-muted);
}

.side-scroll {
  flex: 1;
  overflow-y: auto;
  padding-bottom: 12px;
}

.side-scroll::-webkit-scrollbar {
  width: 6px;
}

.side-scroll::-webkit-scrollbar-thumb {
  background: transparent;
  border-radius: 3px;
}

.side-scroll:hover::-webkit-scrollbar-thumb {
  background: rgba(0, 0, 0, 0.15);
}

/* 搜索无结果的轻提示 */
.side-empty {
  padding: 8px 16px 4px;
  font-size: 0.857rem;
  color: var(--im-text-muted);
}

/* ========== 分组标题 ========== */
.group-title {
  height: 40px;
  padding: 0 16px;
  display: flex;
  align-items: center;
  flex-shrink: 0;
}

.group-title span {
  font-size: 0.929rem;
  font-weight: 700;
  color: var(--im-text-muted);
}

/* 可折叠分组标题：点击收起/展开，箭头指示状态 */
.group-title.clickable {
  cursor: pointer;
  user-select: none;
  -webkit-user-select: none;
}

.group-title.clickable:hover > span,
.group-title.clickable:hover .group-title-left span {
  color: var(--im-text-secondary);
}

.group-title.clickable:focus-visible {
  outline: 2px solid var(--im-primary);
  outline-offset: -2px;
  border-radius: 6px;
}

.chevron {
  color: var(--im-text-muted);
  margin-right: 4px;
  flex-shrink: 0;
  transition: transform 0.15s ease;
}

/* 折叠态：箭头转回右侧 */
.chevron.collapsed {
  transform: rotate(-90deg);
}

/* ========== 通讯录项 ========== */
.contact-item {
  height: 56px;
  padding: 0 12px;
  display: flex;
  align-items: center;
  gap: 12px;
  cursor: pointer;
  border-radius: 8px;
  margin: 0 8px;
  flex-shrink: 0;
  transition: background-color 0.15s ease;
  user-select: none;
  -webkit-user-select: none;
}

.contact-item:hover {
  background: var(--im-surface-2);
}

.contact-item:focus-visible {
  outline: 2px solid var(--im-primary);
  outline-offset: -2px;
}

.contact-item.active {
  background: var(--im-selected);
}

.avatar {
  width: 36px;
  height: 36px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 1rem;
  font-weight: 500;
  flex-shrink: 0;
}

.contact-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.contact-name {
  font-size: 0.929rem;
  font-weight: 500;
  color: var(--im-text-title);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  line-height: 19px;
}

.contact-signature {
  font-size: 0.786rem;
  color: var(--im-text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  line-height: 15px;
}

/* ========== 列分隔线 ========== */
.divider-col {
  width: 1px;
  background: var(--im-border);
  flex-shrink: 0;
}

/* ========== 右栏：灰色底 + 居中资料卡片 ========== */
.detail-area {
  flex: 1;
  min-width: 0;
  background: var(--im-surface-2);
  overflow-y: auto;
  display: flex;
  justify-content: center;
  align-items: flex-start;
}

.detail-card {
  width: 560px;
  max-width: calc(100% - 64px);
  margin: 32px 0 40px;
  background: var(--im-surface);
  border-radius: 12px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.05);
  overflow: hidden;
}

/* 卡片头：头像在左，名称/ID 在右（微信资料页风格） */
.card-head {
  padding: 28px 32px 22px;
  display: flex;
  align-items: center;
  gap: 16px;
}

.card-avatar {
  width: 64px;
  height: 64px;
  border-radius: 10px;
  font-size: 1.571rem;
}

.card-head-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.card-name {
  font-size: 1.286rem;
  font-weight: 700;
  color: var(--im-text-title);
  line-height: 26px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.card-id {
  font-size: 0.857rem;
  color: var(--im-text-muted);
  line-height: 17px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 资料信息行：标签 + 值，顶部细分隔线 */
.card-row {
  min-height: 48px;
  padding: 0 32px;
  display: flex;
  align-items: center;
  gap: 12px;
  border-top: 1px solid var(--im-border);
}

.card-row-label {
  width: 64px;
  flex-shrink: 0;
  font-size: 0.929rem;
  color: var(--im-text-muted);
}

.card-row-value {
  flex: 1;
  min-width: 0;
  font-size: 0.929rem;
  color: var(--im-text-title);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 底部功能区：图标 + 文案纵向按钮，横向均布 */
.card-actions {
  padding: 22px 32px 28px;
  display: flex;
  justify-content: space-around;
  gap: 16px;
  border-top: 1px solid var(--im-border);
}

.action-btn {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  background: transparent;
  border: none;
  padding: 0;
  cursor: pointer;
  font-family: inherit;
}

.action-icon {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  background: var(--im-surface-2);
  color: var(--im-text-secondary);
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background-color 0.15s ease;
}

.action-btn:hover .action-icon {
  background: var(--im-hover-gray);
}

.action-btn.primary .action-icon {
  color: var(--im-primary);
  background: rgba(37, 99, 235, 0.08);
}

.action-btn.primary:hover .action-icon {
  background: rgba(37, 99, 235, 0.14);
}

.action-label {
  font-size: 0.857rem;
  color: var(--im-text-secondary);
}

/* 空态 */
.card-empty {
  padding: 8px 32px 32px;
  border-top: 1px solid var(--im-border);
  text-align: center;
  color: var(--im-text-muted);
  font-size: 0.929rem;
}

.card-empty p {
  margin: 16px 0 0;
}

/* 触屏优化 */
@media (hover: none) and (pointer: coarse) {
  .contact-item {
    min-height: 64px;
  }

  * {
    -webkit-tap-highlight-color: transparent;
  }
}

/* 减少动画偏好 */
@media (prefers-reduced-motion: reduce) {
  .contact-item,
  .action-icon {
    transition: none !important;
  }
}
</style>
