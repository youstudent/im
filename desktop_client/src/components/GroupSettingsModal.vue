<script setup>
// 群设置弹窗：群名/群公告（管理员可编辑，普通成员只读）
// 第三期（P2）：入群确认（G7）/全员禁言（G8）仅群主/管理员可编辑；保存到通讯录（G10）任何成员可编辑
import { ref } from 'vue'
import { groupApi } from '../api/social'

const props = defineProps({
  // 目标群 g_uid
  groupUid: { required: true },
  initialName: { type: String, default: '' },
  initialAnnouncement: { type: String, default: '' },
  // 群主或管理员才可编辑，普通成员只读
  isAdmin: { type: Boolean, default: false },
  // G7 入群确认 / G8 全员禁言 / G10 保存到通讯录（0/1）
  initialInviteConfirm: { type: Number, default: 0 },
  initialMuteAll: { type: Number, default: 0 },
  initialSaved: { type: Number, default: 1 },
})

const emit = defineEmits(['close', 'saved', 'failed'])

const gsName = ref(props.initialName)
const gsAnnouncement = ref(props.initialAnnouncement)
const gsInviteConfirm = ref(props.initialInviteConfirm === 1)
const gsMuteAll = ref(props.initialMuteAll === 1)
const gsSaved = ref(props.initialSaved !== 0)
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
    // 群名/公告（管理员）：走 update 接口
    if (props.isAdmin) {
      await groupApi.update(props.groupUid, name, announcement)
    }
    // 入群确认 / 全员禁言（管理员）：走 settings 接口
    if (props.isAdmin) {
      await groupApi.updateSettings(props.groupUid, {
        invite_confirm: gsInviteConfirm.value ? 1 : 0,
        mute_all: gsMuteAll.value ? 1 : 0,
      })
    }
    // 保存到通讯录（任何成员）：走 saved 接口
    await groupApi.setSaved(props.groupUid, gsSaved.value)
    emit('saved', {
      name,
      announcement,
      inviteConfirm: gsInviteConfirm.value ? 1 : 0,
      muteAll: gsMuteAll.value ? 1 : 0,
      saved: gsSaved.value ? 1 : 0,
    })
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

        <!-- G10 保存到通讯录：任何成员可编辑 -->
        <div class="gs-field gs-switch-row">
          <div class="gs-switch-text">
            <label class="gs-label">保存到通讯录</label>
            <p class="gs-switch-tip">关闭后，该群不在通讯录的群聊列表展示</p>
          </div>
          <button
            class="gs-switch"
            :class="{ on: gsSaved }"
            role="switch"
            :aria-checked="gsSaved"
            @click="gsSaved = !gsSaved"
          >
            <span class="gs-switch-knob"></span>
          </button>
        </div>

        <!-- G7 入群确认 / G8 全员禁言：仅群主/管理员可编辑 -->
        <template v-if="isAdmin">
          <div class="gs-field gs-switch-row">
            <div class="gs-switch-text">
              <label class="gs-label">群聊邀请确认</label>
              <p class="gs-switch-tip">开启后，成员邀请新好友需群主/管理员同意</p>
            </div>
            <button
              class="gs-switch"
              :class="{ on: gsInviteConfirm }"
              role="switch"
              :aria-checked="gsInviteConfirm"
              @click="gsInviteConfirm = !gsInviteConfirm"
            >
              <span class="gs-switch-knob"></span>
            </button>
          </div>
          <div class="gs-field gs-switch-row">
            <div class="gs-switch-text">
              <label class="gs-label">全员禁言</label>
              <p class="gs-switch-tip">开启后，仅群主/管理员可发言</p>
            </div>
            <button
              class="gs-switch"
              :class="{ on: gsMuteAll }"
              role="switch"
              :aria-checked="gsMuteAll"
              @click="gsMuteAll = !gsMuteAll"
            >
              <span class="gs-switch-knob"></span>
            </button>
          </div>
        </template>
        <p v-else class="gs-tip">仅群主或管理员可修改群设置与全员禁言/入群确认</p>
      </div>

      <footer class="invite-modal-footer">
        <button class="invite-btn-cancel" @click="emit('close')">关闭</button>
        <button class="invite-btn-confirm" :disabled="gsSaving" @click="save">
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

/* 开关行（保存到通讯录 / 入群确认 / 全员禁言） */
.gs-switch-row {
  flex-direction: row;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 12px;
  background: var(--im-surface-2);
  border-radius: 8px;
}

.gs-switch-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.gs-switch-tip {
  margin: 0;
  font-size: 0.786rem;
  color: var(--im-text-muted);
}

.gs-switch {
  flex-shrink: 0;
  width: 40px;
  height: 22px;
  padding: 0;
  border: none;
  border-radius: 11px;
  background: var(--im-border);
  cursor: pointer;
  transition: background-color 0.15s ease;
  position: relative;
}

.gs-switch.on {
  background: var(--im-primary);
}

.gs-switch-knob {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: #fff;
  transition: left 0.15s ease;
}

.gs-switch.on .gs-switch-knob {
  left: 20px;
}
</style>
