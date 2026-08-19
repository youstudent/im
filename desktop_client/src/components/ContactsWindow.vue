<script setup>
import { ref, computed, onActivated } from 'vue'
import { friendApi, groupApi } from '../api/social'
import CreateGroupModal from './CreateGroupModal.vue'

const emit = defineEmits(['send-message', 'group-created'])

// 创建群聊弹窗
const showCreateGroup = ref(false)

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
    return { type: 'group', name: group.name, nickname: '', remark: '', avatar: group.avatar, color: group.color, workchatId: String(group.gUid || group.id) }
  }
  const fallback = friends.value[0]
  return fallback
    ? { type: 'friend', name: fallback.name, nickname: fallback.nickname || '', remark: fallback.remark || '', avatar: fallback.avatar, color: fallback.color, workchatId: fallback.workchatId }
    : { type: 'empty', name: '暂无联系人', nickname: '', remark: '', avatar: '?', color: '#cbd5e1', workchatId: '' }
})

function selectItem(id) {
  selectedDetailId.value = id
}

function sendMessage(id) {
  emit('send-message', id)
}

// 创建群聊成功后：关闭弹窗、刷新群列表、通知父组件
async function onGroupCreated(info) {
  showCreateGroup.value = false
  await loadRealData()
  emit('group-created', info)
}
</script>

<template>
  <div class="window">
    <main class="body">
      <!-- 通讯录主区 -->
      <section class="contacts-main">
        <!-- 好友分组 -->
        <div class="group-title">
          <span>好友 ({{ friends.length }})</span>
        </div>

        <!-- 好友项 -->
        <div
          v-for="f in friends"
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
          <button class="btn-outline-sm" @click.stop="sendMessage(f.id)">发消息</button>
        </div>

        <!-- 群组分组 -->
        <div class="group-title group-title-row">
          <span>我的群组 ({{ groups.length }})</span>
          <button class="btn-create-group" @click="showCreateGroup = true">
            <svg viewBox="0 0 16 16" width="14" height="14">
              <path d="M8 3v10M3 8h10" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
            </svg>
            <span>创建群聊</span>
          </button>
        </div>

        <!-- 群组项 -->
        <div
          v-for="g in groups"
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
          <button class="btn-outline-sm" @click.stop="sendMessage(g.id)">进入</button>
        </div>
      </section>

      <!-- 列分隔线 -->
      <div class="divider-col"></div>

      <!-- 联系人详情面板：头像 + 昵称/备注/ID 信息行 -->
      <aside class="detail-panel">
        <div class="detail-header">
          <div class="avatar big" :style="{ background: currentDetail.color }">
            <span>{{ currentDetail.avatar }}</span>
          </div>
          <div class="detail-name">{{ currentDetail.name }}</div>
          <div class="detail-id">
            {{ currentDetail.type === 'group' ? 'WorkChat 群号：' + (currentDetail.workchatId || '') : 'WorkChat ID：' + currentDetail.workchatId }}
          </div>
        </div>

        <!-- 好友信息行：昵称/备注（有备注才展示）；群聊无此概念 -->
        <template v-if="currentDetail.type === 'friend'">
          <div class="detail-divider"></div>
          <div class="detail-info-row">
            <span class="detail-info-label">昵称</span>
            <span class="detail-info-value">{{ currentDetail.nickname || '—' }}</span>
          </div>
          <div v-if="currentDetail.remark" class="detail-info-row">
            <span class="detail-info-label">备注</span>
            <span class="detail-info-value">{{ currentDetail.remark }}</span>
          </div>
        </template>
      </aside>
    </main>

    <!-- 创建群聊弹窗 -->
    <CreateGroupModal
      v-if="showCreateGroup"
      :friends="friends"
      @close="showCreateGroup = false"
      @created="onGroupCreated"
    />
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

/* ========== 通讯录主区 ========== */
.contacts-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  /* 顶部留白，替代已移除的自定义顶部导航栏 */
  padding-top: 8px;
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

/* 群组标题行：标题 + 创建群聊按钮 */
.group-title-row {
  justify-content: space-between;
}

.btn-create-group {
  height: 28px;
  padding: 0 10px;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  background: var(--im-primary);
  color: #fff;
  border: none;
  border-radius: 6px;
  font-size: 0.857rem;
  font-weight: 500;
  cursor: pointer;
  font-family: inherit;
  flex-shrink: 0;
  transition: background 0.15s;
}

.btn-create-group:hover {
  background: var(--im-primary-hover);
}

/* ========== 通讯录项 ========== */
.contact-item {
  height: 64px;
  padding: 0 16px;
  display: flex;
  align-items: center;
  gap: 12px;
  cursor: pointer;
  border-radius: 8px;
  margin: 0 8px;
  flex-shrink: 0;
  transition: background-color 0.15s ease;
}

.contact-item:hover {
  background: var(--im-surface-2);
}

.contact-item:focus-visible {
  outline: 2px solid var(--im-primary);
  outline-offset: -2px;
}

.contact-item.active {
  background: var(--im-soft-blue);
}

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
}

.avatar.big {
  width: 72px;
  height: 72px;
  font-size: 1.857rem;
}

.contact-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.contact-name {
  font-size: 1rem;
  font-weight: 700;
  color: var(--im-text-title);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  line-height: 20px;
}

.contact-signature {
  font-size: 0.857rem;
  color: var(--im-text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  line-height: 17px;
}

.btn-outline-sm {
  height: 28px;
  padding: 0 10px;
  background: var(--im-surface-2);
  color: var(--im-text-label);
  border: none;
  border-radius: 6px;
  font-size: 0.857rem;
  font-weight: 500;
  cursor: pointer;
  flex-shrink: 0;
  font-family: inherit;
}

.btn-outline-sm:hover {
  background: var(--im-hover-gray-active);
}

/* ========== 列分隔线 ========== */
.divider-col {
  width: 1px;
  background: var(--im-border);
  flex-shrink: 0;
}

/* ========== 详情面板 ========== */
.detail-panel {
  width: 320px;
  background: var(--im-surface);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  overflow-y: auto;
}

.detail-header {
  padding: 24px 16px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
}

.detail-name {
  margin-top: 4px;
  font-size: 1.286rem;
  font-weight: 700;
  color: var(--im-text-title);
  line-height: 26px;
}

.detail-id {
  font-size: 0.857rem;
  color: var(--im-text-muted);
  line-height: 17px;
}

/* 详情面板信息行（昵称/备注/ID） */
.detail-divider {
  height: 1px;
  margin: 4px 16px 8px;
  background: var(--im-border);
  flex-shrink: 0;
}

.detail-info-row {
  min-height: 44px;
  padding: 0 16px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.detail-info-label {
  width: 44px;
  flex-shrink: 0;
  font-size: 0.929rem;
  color: var(--im-text-muted);
}

.detail-info-value {
  flex: 1;
  min-width: 0;
  font-size: 0.929rem;
  color: var(--im-text-title);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 触屏优化 */
@media (hover: none) and (pointer: coarse) {
  .contact-item {
    min-height: 72px;
  }

  .nav-btn {
    width: 44px;
    height: 44px;
  }

  * {
    -webkit-tap-highlight-color: transparent;
  }
}

/* 减少动画偏好 */
@media (prefers-reduced-motion: reduce) {
  .contact-item,
  .nav-btn {
    transition: none !important;
  }
}
</style>
