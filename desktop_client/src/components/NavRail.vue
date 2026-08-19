<script setup>
// 共享左侧导航栏
// 通过 emit('navigate', key) 通知父组件切换页面
// 当前激活项由父组件通过 prop `active` 传入
defineProps({
  active: {
    type: String,
    default: 'messages', // messages | contacts | settings | notifications
  },
  // 聊天未读总数（消息导航按钮上的红色气泡）
  badge: {
    type: Number,
    default: 0,
  },
  // 新的好友申请提醒（"新建/添加好友"按钮上的红点）
  addFriendBadge: {
    type: Boolean,
    default: false,
  },
})

const emit = defineEmits(['navigate', 'create', 'open-settings'])

// 点击底部设置按钮：打开独立的设置窗口
function openSettings() {
  emit('open-settings')
}
</script>

<template>
  <nav class="nav-rail" aria-label="主导航">
    <div class="me" aria-label="我的头像">
      <span>我</span>
    </div>

    <!-- 消息导航 -->
    <button
      class="nav-btn"
      :class="{ active: active === 'messages' }"
      aria-label="消息"
      :aria-current="active === 'messages' ? 'page' : undefined"
      @click="emit('navigate', 'messages')"
    >
      <svg viewBox="0 0 24 24" width="24" height="24" class="icon">
        <path d="M4 5a4 4 0 0 1 4-4h8a4 4 0 0 1 4 4v8a4 4 0 0 1-4 4H9l-4 4v-4a4 4 0 0 1-1-2V5z"
          fill="none" :stroke="active === 'messages' ? '#fff' : '#646a73'" stroke-width="1.6" stroke-linejoin="round" />
      </svg>
      <span v-if="badge > 0" class="badge">{{ badge > 99 ? '99+' : badge }}</span>
    </button>

    <!-- 联系人导航 -->
    <button
      class="nav-btn"
      :class="{ active: active === 'contacts' }"
      aria-label="联系人"
      :aria-current="active === 'contacts' ? 'page' : undefined"
      @click="emit('navigate', 'contacts')"
    >
      <svg viewBox="0 0 24 24" width="24" height="24" class="icon">
        <circle cx="9" cy="8" r="3.2" fill="none" stroke="currentColor" stroke-width="1.6" />
        <path d="M3 19c0-3 2.7-5 6-5s6 2 6 5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
        <path d="M15.5 6.5h5M18 4v5" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
      </svg>
    </button>

    <!-- 通知导航 -->
    <button
      class="nav-btn"
      :class="{ active: active === 'notifications' }"
      aria-label="通知"
      :aria-current="active === 'notifications' ? 'page' : undefined"
      @click="emit('navigate', 'notifications')"
    >
      <svg viewBox="0 0 24 24" width="24" height="24" class="icon">
        <path d="M6 16V11a6 6 0 0 1 12 0v5l1.5 2H4.5L6 16z"
          fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
        <path d="M10 21h4" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
      </svg>
    </button>

    <div class="nav-spacer"></div>

    <!-- 新建按钮（底部圆形 FAB）：点击弹出添加好友，有新申请时显示红点 -->
    <button class="nav-btn fab" aria-label="新建" @click="emit('create')">
      <svg viewBox="0 0 24 24" width="24" height="24">
        <path d="M12 5v14M5 12h14" stroke="#fff" stroke-width="2" stroke-linecap="round" />
      </svg>
      <span v-if="addFriendBadge" class="red-dot"></span>
    </button>

    <!-- 设置按钮（位于 + 按钮下方，点击弹出独立的设置窗口）
         图标：齿轮状 -->
    <button class="nav-btn settings-btn" aria-label="设置" @click="openSettings">
      <svg viewBox="0 0 24 24" width="22" height="22">
        <!-- 中心圆 -->
        <circle cx="12" cy="12" r="3" fill="none" stroke="currentColor" stroke-width="1.6" />
        <!-- 齿轮外圈：8 条圆角齿 -->
        <path
          d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41"
          stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
      </svg>
    </button>
  </nav>
</template>

<style scoped>
.nav-rail {
  width: 72px;
  background: var(--im-surface-2);
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 12px 0;
  gap: 12px;
  flex-shrink: 0;
}

.me {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: var(--im-primary);
  color: #fff;
  font-size: 1.071rem;
  font-weight: 500;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.nav-btn {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  background: transparent;
  border: none;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
  flex-shrink: 0;
  transition: background 0.15s;
  padding: 0;
  color: var(--im-text-secondary);
}

.nav-btn:hover {
  background: rgba(0, 0, 0, 0.04);
}

.nav-btn.active {
  background: var(--im-primary);
}

.nav-btn.active svg path,
.nav-btn.active svg line,
.nav-btn.active svg circle {
  stroke: #fff;
}

/* 加号按钮：默认展示悬停态（略深浅灰背景 + 深灰图标），hover 再加深 */
.nav-btn.fab {
  background: var(--im-soft-gray);
  border-radius: 999px;
}

.nav-btn.fab:hover {
  background: var(--im-hover-gray);
}

.nav-btn.fab svg path {
  stroke: currentColor;
}

/* 设置按钮：位于 + 号下方，样式与其他 nav 按钮一致（无背景） */
.nav-btn.settings-btn {
  background: transparent;
  border-radius: 12px;
}

.nav-btn.settings-btn:hover {
  background: rgba(0, 0, 0, 0.04);
}

.badge {
  position: absolute;
  top: -2px;
  right: -2px;
  min-width: 20px;
  height: 20px;
  padding: 0 6px;
  background: var(--im-danger);
  color: #fff;
  border-radius: 999px;
  font-size: 0.786rem;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  line-height: 1;
  box-shadow: 0 0 0 2px var(--im-surface-2);
  z-index: 1;
}

/* 新的好友申请红点 */
.red-dot {
  position: absolute;
  top: 6px;
  right: 8px;
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: var(--im-danger);
  border: 2px solid #fff;
  box-sizing: content-box;
  z-index: 1;
}

.nav-spacer {
  flex: 1;
  min-height: 1px;
}

/* 触屏优化 */
@media (hover: none) and (pointer: coarse) {
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
  .nav-btn {
    transition: none !important;
  }
}
</style>