<script setup>
// 群设置弹窗：群名/群公告（管理员可编辑，普通成员只读）
import { ref } from 'vue'
import { groupApi } from '../api/social'

const props = defineProps({
  // 目标群 g_uid
  groupUid: { required: true },
  initialName: { type: String, default: '' },
  initialAnnouncement: { type: String, default: '' },
  // 群主或管理员才可编辑，普通成员只读
  isAdmin: { type: Boolean, default: false },
})

const emit = defineEmits(['close', 'saved', 'failed'])

const gsName = ref(props.initialName)
const gsAnnouncement = ref(props.initialAnnouncement)
const gsSaving = ref(false)

async function save() {
  if (!props.groupUid || gsSaving.value) return
  const name = gsName.value.trim()
  const announcement = gsAnnouncement.value.trim()
  if (!name) {
    emit('failed', '群名不能为空')
    return
  }
  gsSaving.value = true
  try {
    await groupApi.update(props.groupUid, name, announcement)
    emit('saved', { name, announcement })
  } catch (e) {
    emit('failed', e.message || '群设置保存失败')
  } finally {
    gsSaving.value = false
  }
}
</script>

<template>
  <div class="invite-overlay" @click.self="emit('close')">
    <div class="invite-modal">
      <header class="invite-modal-header">
        <h2 class="invite-modal-title">群设置</h2>
        <button class="invite-modal-close" aria-label="关闭" @click="emit('close')">
          <svg viewBox="0 0 16 16" width="16" height="16">
            <path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
          </svg>
        </button>
      </header>

      <div class="invite-modal-body gs-body">
        <div class="gs-field">
          <label class="gs-label">群名称</label>
          <input
            v-if="isAdmin"
            v-model="gsName"
            class="gs-input"
            maxlength="20"
            placeholder="输入群名称"
          />
          <div v-else class="gs-readonly">{{ gsName }}</div>
        </div>
        <div class="gs-field">
          <label class="gs-label">群公告</label>
          <textarea
            v-if="isAdmin"
            v-model="gsAnnouncement"
            class="gs-textarea"
            maxlength="500"
            rows="5"
            placeholder="输入群公告"
          ></textarea>
          <div v-else class="gs-readonly">{{ gsAnnouncement || '暂无公告' }}</div>
        </div>
        <p v-if="!isAdmin" class="gs-tip">仅群主或管理员可修改群设置</p>
      </div>

      <footer class="invite-modal-footer">
        <button class="invite-btn-cancel" @click="emit('close')">关闭</button>
        <button v-if="isAdmin" class="invite-btn-confirm" :disabled="gsSaving" @click="save">
          {{ gsSaving ? '保存中…' : '保存' }}
        </button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
@import '../styles/modal-base.css';

.gs-body {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.gs-field {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.gs-label {
  font-size: 0.857rem;
  font-weight: 500;
  color: var(--im-text-secondary);
}

.gs-input,
.gs-textarea {
  width: 100%;
  box-sizing: border-box;
  padding: 9px 12px;
  font-size: 0.929rem;
  font-family: inherit;
  color: var(--im-text-title);
  background: var(--im-surface-2);
  border: 1px solid var(--im-border);
  border-radius: 8px;
  outline: none;
  transition: border-color 0.15s ease, background-color 0.15s ease;
}

.gs-textarea {
  resize: vertical;
  line-height: 20px;
}

.gs-input:focus,
.gs-textarea:focus {
  border-color: var(--im-primary);
  background: var(--im-surface);
}

.gs-readonly {
  padding: 9px 12px;
  font-size: 0.929rem;
  line-height: 20px;
  color: var(--im-text-title);
  background: var(--im-surface-2);
  border-radius: 8px;
  word-break: break-word;
  white-space: pre-wrap;
}

.gs-tip {
  margin: 0;
  font-size: 0.786rem;
  color: var(--im-text-muted);
}
</style>
