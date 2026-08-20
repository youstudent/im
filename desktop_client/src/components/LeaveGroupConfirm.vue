<script setup>
// 退出群聊二次确认弹窗（危险操作风格）
defineProps({
  // 群名称（确认文案展示）
  groupName: { type: String, default: '' },
  // 退群进行中（禁用确认按钮）
  leaving: { type: Boolean, default: false },
})

const emit = defineEmits(['confirm', 'cancel'])
</script>

<template>
  <div class="confirm-overlay" @click.self="emit('cancel')">
    <div class="confirm-modal" role="dialog" aria-modal="true" aria-label="退出群聊确认">
      <div class="confirm-icon danger">
        <svg viewBox="0 0 24 24" width="26" height="26">
          <path d="M9 4h6a1 1 0 0 1 1 1v1h3a1 1 0 0 1 1 1v12a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V7a1 1 0 0 1 1-1h3V5a1 1 0 0 1 1-1z" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
          <path d="M10 11l4 4m0-4l-4 4" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
        </svg>
      </div>
      <h3 class="confirm-title">退出群聊</h3>
      <p class="confirm-text">确定退出「{{ groupName }}」吗？</p>
      <p class="confirm-sub">退出后将不再接收该群的消息，本地会话与聊天记录也会被清除。</p>
      <div class="confirm-actions">
        <button class="confirm-btn cancel" @click="emit('cancel')">取消</button>
        <button class="confirm-btn danger" :disabled="leaving" @click="emit('confirm')">
          {{ leaving ? '退出中…' : '退出群聊' }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.confirm-overlay {
  position: fixed;
  inset: 0;
  background: var(--im-overlay);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1100;
  padding: 40px;
  animation: overlayIn 0.16s ease;
}

@keyframes overlayIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

.confirm-modal {
  width: 100%;
  max-width: 380px;
  background: var(--im-surface);
  border-radius: 16px;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.2);
  padding: 28px 28px 24px;
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  animation: confirmIn 0.18s cubic-bezier(0.34, 1.4, 0.64, 1);
}

@keyframes confirmIn {
  from { transform: scale(0.92) translateY(6px); opacity: 0; }
  to { transform: scale(1) translateY(0); opacity: 1; }
}

.confirm-icon {
  width: 52px;
  height: 52px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 14px;
}

.confirm-icon.danger {
  background: rgba(239, 68, 68, 0.1);
  color: var(--im-danger, #ef4444);
}

.confirm-title {
  margin: 0 0 8px;
  font-size: 1.143rem;
  font-weight: 700;
  color: var(--im-text-title);
}

.confirm-text {
  margin: 0;
  font-size: 0.929rem;
  color: var(--im-text-title);
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.confirm-sub {
  margin: 6px 0 0;
  font-size: 0.857rem;
  line-height: 19px;
  color: var(--im-text-muted);
}

.confirm-actions {
  display: flex;
  gap: 12px;
  margin-top: 22px;
  width: 100%;
}

.confirm-btn {
  flex: 1;
  height: 40px;
  border-radius: 9px;
  font-size: 0.929rem;
  font-weight: 500;
  font-family: inherit;
  cursor: pointer;
  transition: background-color 0.15s ease, border-color 0.15s ease;
}

.confirm-btn.cancel {
  background: var(--im-surface-2);
  border: 1px solid var(--im-border);
  color: var(--im-text-title);
}

.confirm-btn.cancel:hover {
  background: var(--im-hover-gray);
}

.confirm-btn.danger {
  background: var(--im-danger, #ef4444);
  border: 1px solid var(--im-danger, #ef4444);
  color: #fff;
}

.confirm-btn.danger:hover {
  background: #dc2626;
  border-color: #dc2626;
}

.confirm-btn.danger:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}
</style>
