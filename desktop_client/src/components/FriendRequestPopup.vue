<script setup>
// 好友申请实时弹框：收到 WS friend.request 事件后右下角弹出，
// 展示申请人头像/昵称/验证消息，支持 关闭 / 同意 / 拒绝；
// 关闭后申请仍在通知中心与"添加好友"弹窗中可再次处理。
import { ref, computed } from 'vue'
import { friendApi } from '../api/social'
import { CONTACT_COLORS } from '../utils/palette'

const props = defineProps({
  // { reqId, fromUid, nickname, message }
  req: { type: Object, required: true },
})

const emit = defineEmits(['close', 'handled'])

const busy = ref(false) // 同意/拒绝请求进行中
const result = ref('')  // accepted | rejected（短暂展示结果后自动收起）

// 头像配色：按 uid 稳定取色（与会话列表同一色板）
const avatarColor = computed(() => {
  const uid = Number(props.req.fromUid) || 0
  return CONTACT_COLORS[uid % CONTACT_COLORS.length]
})

const avatarText = computed(() => (props.req.nickname || '?')[0])

async function handle(accept) {
  if (busy.value || result.value) return
  busy.value = true
  try {
    await friendApi.handleRequest(props.req.reqId, accept)
    result.value = accept ? 'accepted' : 'rejected'
    emit('handled', { accepted: accept, req: props.req })
    // 结果短暂可见后自动收起（新会话由 WS conversation.created 事件增量插入）
    setTimeout(() => emit('close'), 900)
  } catch (e) {
    console.warn('[FriendRequestPopup] 处理申请失败:', e?.message || e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="frp-card" role="dialog" aria-label="好友申请">
    <header class="frp-head">
      <span class="frp-title">新的好友申请</span>
      <button class="frp-close" aria-label="关闭" @click="emit('close')">×</button>
    </header>

    <div class="frp-body">
      <div class="frp-avatar" :style="{ background: avatarColor }">
        <span>{{ avatarText }}</span>
      </div>
      <div class="frp-info">
        <div class="frp-name" :title="req.nickname">{{ req.nickname || '未知用户' }}</div>
        <div class="frp-msg" :title="req.message">
          {{ req.message ? '验证消息：' + req.message : '请求添加你为好友' }}
        </div>
      </div>
    </div>

    <!-- 处理后短暂展示结果文案 -->
    <div v-if="result" class="frp-result" :class="result">
      {{ result === 'accepted' ? '已同意，对方已成为你的好友' : '已拒绝该申请' }}
    </div>
    <footer v-else class="frp-actions">
      <button class="frp-btn" :disabled="busy" @click="handle(false)">拒绝</button>
      <button class="frp-btn primary" :disabled="busy" @click="handle(true)">
        {{ busy ? '处理中…' : '同意' }}
      </button>
    </footer>
  </div>
</template>

<style scoped>
.frp-card {
  /* 页面正中央：上下左右居中（transform 需与 frpIn 动画关键帧保持一致） */
  position: fixed;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  z-index: 2000;
  width: 320px;
  background: var(--im-surface);
  border: 1px solid var(--im-border);
  border-radius: 12px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.18);
  overflow: hidden;
  animation: frpIn 0.18s ease;
}

@keyframes frpIn {
  from {
    opacity: 0;
    transform: translate(-50%, -50%) scale(0.96);
  }
  to {
    opacity: 1;
    transform: translate(-50%, -50%) scale(1);
  }
}

.frp-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  background: var(--im-surface-2);
  border-bottom: 1px solid var(--im-border);
}

.frp-title {
  font-size: 0.929rem;
  font-weight: 600;
  color: var(--im-text-title);
}

.frp-close {
  width: 22px;
  height: 22px;
  border: none;
  background: transparent;
  border-radius: 999px;
  font-size: 1.1rem;
  line-height: 1;
  color: var(--im-text-muted);
  cursor: pointer;
}

.frp-close:hover {
  background: var(--im-hover-gray);
}

.frp-body {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 14px;
}

.frp-avatar {
  flex-shrink: 0;
  width: 42px;
  height: 42px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 1.1rem;
}

.frp-info {
  min-width: 0;
}

.frp-name {
  font-size: 0.929rem;
  font-weight: 600;
  color: var(--im-text-title);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.frp-msg {
  margin-top: 2px;
  font-size: 0.857rem;
  color: var(--im-text-secondary);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.frp-result {
  padding: 0 14px 14px;
  font-size: 0.857rem;
}

.frp-result.accepted {
  color: var(--im-primary);
}

.frp-result.rejected {
  color: var(--im-text-muted);
}

.frp-actions {
  display: flex;
  gap: 8px;
  padding: 0 14px 14px;
}

.frp-btn {
  flex: 1;
  padding: 7px 0;
  border: 1px solid var(--im-border);
  background: transparent;
  border-radius: 8px;
  font-size: 0.929rem;
  color: var(--im-text-title);
  cursor: pointer;
}

.frp-btn:hover:not(:disabled) {
  background: var(--im-hover-gray);
}

.frp-btn.primary {
  background: var(--im-primary);
  border-color: var(--im-primary);
  color: #fff;
}

.frp-btn.primary:hover:not(:disabled) {
  opacity: 0.9;
  background: var(--im-primary);
}

.frp-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}
</style>
