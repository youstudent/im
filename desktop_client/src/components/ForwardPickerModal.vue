<script setup>
// 转发目标会话选择弹窗（多选）：逐条转发时选择转发到哪些会话。
// props.conversations 由调用方过滤掉当前会话与占位会话。
import { ref } from 'vue'

const props = defineProps({
  conversations: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
})
const emit = defineEmits(['close', 'confirm'])

const selected = ref([])

function toggle(c) {
  const i = selected.value.indexOf(c.id)
  if (i >= 0) selected.value.splice(i, 1)
  else selected.value.push(c.id)
}

function confirm() {
  if (!selected.value.length || props.loading) return
  emit('confirm', selected.value.slice())
}
</script>

<template>
  <div class="fwd-mask" @click.self="emit('close')">
    <div class="fwd-modal">
      <header class="fwd-head">
        <span class="fwd-title">转发到</span>
        <button class="fwd-close" aria-label="关闭" @click="emit('close')">×</button>
      </header>

      <div class="fwd-body">
        <div v-if="!conversations.length" class="fwd-empty">没有可转发的会话</div>
        <div
          v-for="c in conversations"
          :key="c.id"
          class="fwd-item"
          @click="toggle(c)"
        >
          <div class="fwd-avatar" :style="{ background: c.color }"><span>{{ c.avatar }}</span></div>
          <div class="fwd-info">
            <span class="fwd-name">{{ c.name }}</span>
            <span class="fwd-last">{{ c.lastMessage }}</span>
          </div>
          <span class="fwd-check" :class="{ on: selected.includes(c.id) }"></span>
        </div>
      </div>

      <footer class="fwd-foot">
        <button class="fwd-btn cancel" @click="emit('close')">取消</button>
        <button class="fwd-btn ok" :disabled="!selected.length || loading" @click="confirm">
          {{ loading ? '转发中…' : `转发（${selected.length}）` }}
        </button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.fwd-mask {
  position: fixed;
  inset: 0;
  z-index: 1200;
  background: rgba(0, 0, 0, 0.35);
  display: flex;
  align-items: center;
  justify-content: center;
}

.fwd-modal {
  width: 340px;
  max-height: 70vh;
  display: flex;
  flex-direction: column;
  background: var(--im-surface);
  border: 1px solid var(--im-border);
  border-radius: 12px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.18);
  overflow: hidden;
}

.fwd-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px 10px;
}

.fwd-title {
  font-size: 1rem;
  font-weight: 600;
  color: var(--im-text-title);
}

.fwd-close {
  border: none;
  background: transparent;
  font-size: 1.3rem;
  line-height: 1;
  color: var(--im-text-muted);
  cursor: pointer;
}

.fwd-body {
  flex: 1;
  overflow-y: auto;
  padding: 4px 8px 8px;
}

.fwd-empty {
  padding: 24px 0;
  text-align: center;
  color: var(--im-text-muted);
  font-size: 0.929rem;
}

.fwd-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border-radius: 8px;
  cursor: pointer;
}

.fwd-item:hover {
  background: var(--im-surface-2);
}

.fwd-avatar {
  width: 32px;
  height: 32px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 0.857rem;
  flex-shrink: 0;
}

.fwd-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.fwd-name {
  font-size: 0.929rem;
  color: var(--im-text-title);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fwd-last {
  font-size: 0.786rem;
  color: var(--im-text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fwd-check {
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: 1.5px solid var(--im-border);
  flex-shrink: 0;
}

.fwd-check.on {
  border-color: var(--im-primary);
  background: var(--im-primary);
  box-shadow: inset 0 0 0 3px var(--im-surface);
}

.fwd-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 10px 16px 14px;
  border-top: 1px solid var(--im-border);
}

.fwd-btn {
  padding: 7px 18px;
  border-radius: 8px;
  font-size: 0.929rem;
  cursor: pointer;
  border: 1px solid var(--im-border);
  background: var(--im-surface);
  color: var(--im-text-title);
}

.fwd-btn.ok {
  background: var(--im-primary);
  border-color: var(--im-primary);
  color: #fff;
}

.fwd-btn.ok:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
