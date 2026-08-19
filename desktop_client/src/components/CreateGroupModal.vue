<script setup>
import { ref, computed } from 'vue'
import { groupApi } from '../api/social'
import { fileApi } from '../api/file'

const emit = defineEmits(['close', 'created'])

// ===== 群头像 =====
// 支持两种方式：上传图片 / 选择预设颜色
const avatarType = ref('color') // color | upload
const avatarUrl = ref('')
const avatarColor = ref('#3b82f6')
const presetColors = ['#3b82f6', '#8b5cf6', '#ec4899', '#ef4444', '#f59e0b', '#10b981', '#0ea5e9', '#a855f7']
const avatarUploading = ref(false)

// ===== 群名称 =====
const groupName = ref('')

// ===== 成员多选 =====
// 通讯录好友（父组件传入）
const props = defineProps({
  friends: {
    type: Array,
    default: () => [],
  },
})
const selectedUIDs = ref(new Set())

const selectedCount = computed(() => selectedUIDs.value.size)

function toggleMember(uid) {
  const s = new Set(selectedUIDs.value)
  if (s.has(uid)) s.delete(uid)
  else s.add(uid)
  selectedUIDs.value = s
}

// 选择头像文件并上传 OSS（获取固定 URL）
async function pickAvatar() {
  const input = document.createElement('input')
  input.type = 'file'
  input.accept = 'image/*'
  input.onchange = async () => {
    const f = input.files && input.files[0]
    if (!f) return
    avatarUploading.value = true
    try {
      const { downloadUrl } = await fileApi.uploadFile(f, 'image')
      avatarUrl.value = downloadUrl
      avatarType.value = 'upload'
    } catch (e) {
      console.warn('[CreateGroupModal] 头像上传失败:', e?.message || e)
    } finally {
      avatarUploading.value = false
    }
  }
  input.click()
}

// ===== 提交创建 =====
const submitting = ref(false)
const errorMsg = ref('')

async function createGroup() {
  const name = groupName.value.trim()
  if (!name) {
    errorMsg.value = '请输入群名称'
    return
  }
  errorMsg.value = ''
  submitting.value = true
  try {
    // 头像：上传类型用 URL；颜色类型用颜色值标记（前端用它渲染占位）
    const avatar = avatarType.value === 'upload' ? avatarUrl.value : avatarColor.value
    const members = [...selectedUIDs.value]
    await groupApi.create(name, members, avatar)
    emit('created', { name, memberCount: members.length + 1 })
  } catch (e) {
    errorMsg.value = e.message || '创建群聊失败'
  } finally {
    submitting.value = false
  }
}

function closeModal() {
  emit('close')
}
</script>

<template>
  <div class="modal-overlay" @click.self="closeModal">
    <div class="modal">
      <header class="modal-header">
        <h2 class="modal-title">创建群聊</h2>
        <button class="close-btn" aria-label="关闭" @click="closeModal">
          <svg viewBox="0 0 16 16" width="16" height="16">
            <path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
          </svg>
        </button>
      </header>

      <div class="modal-body">
        <!-- 群头像 -->
        <section class="section">
          <div class="section-label">群头像</div>
          <div class="avatar-row">
            <div
              class="avatar-preview"
              :style="avatarType === 'upload' ? {} : { background: avatarColor }"
            >
              <img v-if="avatarType === 'upload' && avatarUrl" :src="avatarUrl" alt="群头像" />
              <span v-else class="avatar-letter">{{ (groupName.trim() || '群')[0] }}</span>
            </div>
            <div class="avatar-controls">
              <div class="color-row">
                <button
                  v-for="c in presetColors"
                  :key="c"
                  class="color-swatch"
                  :class="{ active: avatarType === 'color' && avatarColor === c }"
                  :style="{ background: c }"
                  @click="avatarColor = c; avatarType = 'color'"
                ></button>
              </div>
              <button class="btn-upload" :disabled="avatarUploading" @click="pickAvatar">
                <span>{{ avatarUploading ? '上传中…' : (avatarType === 'upload' ? '更换图片' : '上传图片') }}</span>
              </button>
            </div>
          </div>
        </section>

        <!-- 群名称 -->
        <section class="section">
          <div class="section-label">群名称</div>
          <input
            v-model="groupName"
            class="text-field"
            type="text"
            placeholder="请输入群名称（必填）"
            maxlength="20"
          />
        </section>

        <!-- 选择成员 -->
        <section class="section">
          <div class="section-label">选择成员（已选 {{ selectedCount }}）</div>
          <div v-if="!props.friends.length" class="empty-hint">通讯录暂无好友</div>
          <div v-else class="member-list">
            <button
              v-for="f in props.friends"
              :key="f.id"
              class="member-item"
              :class="{ selected: selectedUIDs.has(f.uid) }"
              @click="toggleMember(f.uid)"
            >
              <div class="member-avatar" :style="{ background: f.color }">
                <span>{{ f.avatar }}</span>
              </div>
              <span class="member-name">{{ f.name }}</span>
              <span class="check-mark">
                <svg v-if="selectedUIDs.has(f.uid)" viewBox="0 0 16 16" width="16" height="16">
                  <path d="M3 8.5l3.5 3.5L13 4.5" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
              </span>
            </button>
          </div>
        </section>

        <!-- 错误提示 -->
        <div v-if="errorMsg" class="error-text">{{ errorMsg }}</div>

        <!-- 操作按钮 -->
        <div class="actions">
          <button class="btn-cancel" @click="closeModal">取消</button>
          <button class="btn-create" :disabled="submitting" @click="createGroup">
            <span>{{ submitting ? '创建中…' : '创建' }}</span>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
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

.modal {
  width: 100%;
  max-width: 520px;
  max-height: calc(100vh - 80px);
  background: var(--im-surface);
  border-radius: 16px;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.2);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

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

.modal-body {
  flex: 1;
  min-height: 0;
  padding: 24px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.modal-body::-webkit-scrollbar {
  width: 6px;
}

.modal-body::-webkit-scrollbar-thumb {
  background: rgba(0, 0, 0, 0.15);
  border-radius: 3px;
}

.section {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.section-label {
  font-size: 0.929rem;
  font-weight: 600;
  color: var(--im-text-title);
}

/* ===== 头像 ===== */
.avatar-row {
  display: flex;
  align-items: center;
  gap: 20px;
}

.avatar-preview {
  width: 64px;
  height: 64px;
  border-radius: 999px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 1.571rem;
  font-weight: 500;
  flex-shrink: 0;
  overflow: hidden;
}

.avatar-preview img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.avatar-letter {
  color: #fff;
}

.avatar-controls {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.color-row {
  display: flex;
  gap: 8px;
}

.color-swatch {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 2px solid transparent;
  cursor: pointer;
  padding: 0;
  transition: transform 0.12s;
}

.color-swatch.active {
  border-color: var(--im-text-title);
  transform: scale(1.12);
}

.btn-upload {
  align-self: flex-start;
  height: 32px;
  padding: 0 16px;
  background: var(--im-soft-blue);
  color: var(--im-text-title);
  border: none;
  border-radius: 8px;
  font-size: 0.857rem;
  font-weight: 500;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s;
}

.btn-upload:hover {
  background: var(--im-soft-blue-hover);
}

.btn-upload:disabled {
  opacity: 0.6;
  cursor: default;
}

/* ===== 文本输入 ===== */
.text-field {
  height: 44px;
  padding: 0 14px;
  background: var(--im-surface);
  border: 1px solid var(--im-border);
  border-radius: 10px;
  font-size: 1rem;
  font-family: inherit;
  color: var(--im-text-title);
  outline: none;
  transition: border-color 0.15s;
}

.text-field:focus {
  border-color: var(--im-primary);
}

.text-field::placeholder {
  color: var(--im-text-secondary);
}

/* ===== 成员多选 ===== */
.member-list {
  max-height: 200px;
  overflow-y: auto;
  border: 1px solid var(--im-border);
  border-radius: 10px;
  padding: 4px;
}

.member-list::-webkit-scrollbar {
  width: 6px;
}

.member-list::-webkit-scrollbar-thumb {
  background: rgba(0, 0, 0, 0.15);
  border-radius: 3px;
}

.member-item {
  width: 100%;
  height: 48px;
  padding: 0 12px;
  display: flex;
  align-items: center;
  gap: 12px;
  background: transparent;
  border: none;
  border-radius: 8px;
  cursor: pointer;
  font-family: inherit;
  text-align: left;
  transition: background 0.12s;
}

.member-item:hover {
  background: var(--im-surface-2);
}

.member-item.selected {
  background: var(--im-soft-blue);
}

.member-avatar {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 0.857rem;
  font-weight: 500;
  flex-shrink: 0;
}

.member-name {
  flex: 1;
  min-width: 0;
  font-size: 0.929rem;
  color: var(--im-text-title);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.check-mark {
  color: var(--im-primary);
  flex-shrink: 0;
}

.empty-hint {
  padding: 16px;
  text-align: center;
  font-size: 0.929rem;
  color: var(--im-text-muted);
  border: 1px dashed var(--im-border);
  border-radius: 10px;
}

/* ===== 错误提示 ===== */
.error-text {
  font-size: 0.857rem;
  color: var(--im-danger);
}

/* ===== 操作按钮 ===== */
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  padding-top: 4px;
}

.btn-cancel {
  height: 38px;
  padding: 0 20px;
  background: var(--im-soft-gray);
  color: var(--im-text-title);
  border: none;
  border-radius: 8px;
  font-size: 0.929rem;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s;
}

.btn-cancel:hover {
  background: var(--im-soft-gray-hover);
}

.btn-create {
  height: 38px;
  padding: 0 28px;
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

.btn-create:hover {
  background: var(--im-primary-hover);
}

.btn-create:disabled {
  opacity: 0.6;
  cursor: default;
}
</style>
