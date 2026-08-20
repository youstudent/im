<script setup>
// 群成员选择弹窗（单选）：转让群主时选择新群主。
// props.members 由调用方过滤掉不可选成员（如自己）；点击成员行即触发确认。
import { ref } from 'vue'

const props = defineProps({
  members: { type: Array, default: () => [] },
  title: { type: String, default: '选择成员' },
  confirmText: { type: String, default: '确定' },
  loading: { type: Boolean, default: false },
})
const emit = defineEmits(['close', 'confirm'])

const selectedUid = ref(null)

function toggle(m) {
  selectedUid.value = selectedUid.value === m.uid ? null : m.uid
}

function confirm() {
  if (selectedUid.value == null || props.loading) return
  const m = props.members.find((x) => x.uid === selectedUid.value)
  if (m) emit('confirm', m)
}
</script>

<template>
  <div class="picker-mask" @click.self="emit('close')">
    <div class="picker-modal">
      <header class="picker-head">
        <span class="picker-title">{{ title }}</span>
        <button class="picker-close" aria-label="关闭" @click="emit('close')">×</button>
      </header>

      <div class="picker-body">
        <div v-if="!members.length" class="picker-empty">没有可选的成员</div>
        <div
          v-for="m in members"
          :key="m.uid"
          class="picker-item"
          :class="{ selected: selectedUid === m.uid }"
          @click="toggle(m)"
        >
          <div class="picker-avatar" :style="{ background: m.color }"><span>{{ m.avatar }}</span></div>
          <!-- 展示名优先级（备注 > 群昵称 > 用户昵称）已在成员 name 中计算 -->
          <span class="picker-name">{{ m.name }}</span>
          <span class="picker-check" :class="{ on: selectedUid === m.uid }"></span>
        </div>
      </div>

      <footer class="picker-foot">
        <button class="picker-btn cancel" @click="emit('close')">取消</button>
        <button class="picker-btn ok" :disabled="selectedUid == null || loading" @click="confirm">
          {{ loading ? '处理中…' : confirmText }}
        </button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.picker-mask {
  position: fixed;
  inset: 0;
  z-index: 1200;
  background: rgba(0, 0, 0, 0.35);
  display: flex;
  align-items: center;
  justify-content: center;
}

.picker-modal {
  width: 320px;
  max-height: 70vh;
  display: flex;
  flex-direction: column;
  background: var(--im-surface);
  border: 1px solid var(--im-border);
  border-radius: 12px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.18);
  overflow: hidden;
}

.picker-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px 10px;
}

.picker-title {
  font-size: 1rem;
  font-weight: 600;
  color: var(--im-text-title);
}

.picker-close {
  border: none;
  background: transparent;
  font-size: 1.3rem;
  line-height: 1;
  color: var(--im-text-muted);
  cursor: pointer;
}

.picker-body {
  flex: 1;
  overflow-y: auto;
  padding: 4px 8px 8px;
}

.picker-empty {
  padding: 24px 0;
  text-align: center;
  color: var(--im-text-muted);
  font-size: 0.929rem;
}

.picker-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border-radius: 8px;
  cursor: pointer;
}

.picker-item:hover {
  background: var(--im-surface-2);
}

.picker-avatar {
  width: 30px;
  height: 30px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 0.857rem;
  flex-shrink: 0;
}

.picker-name {
  flex: 1;
  min-width: 0;
  font-size: 0.929rem;
  color: var(--im-text-title);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.picker-check {
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: 1.5px solid var(--im-border);
  flex-shrink: 0;
}

.picker-check.on {
  border-color: var(--im-primary);
  background: var(--im-primary);
  box-shadow: inset 0 0 0 3px var(--im-surface);
}

.picker-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 10px 16px 14px;
  border-top: 1px solid var(--im-border);
}

.picker-btn {
  padding: 7px 18px;
  border-radius: 8px;
  font-size: 0.929rem;
  cursor: pointer;
  border: 1px solid var(--im-border);
  background: var(--im-surface);
  color: var(--im-text-title);
}

.picker-btn.ok {
  background: var(--im-primary);
  border-color: var(--im-primary);
  color: #fff;
}

.picker-btn.ok:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
