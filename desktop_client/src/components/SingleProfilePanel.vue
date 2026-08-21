<script setup>
// 单聊资料面板：头像/名称/备注编辑/查找记录/免打扰/不落盘
import { ref } from 'vue'

const props = defineProps({
  // 展示资料 { id, name, avatar, color }
  meta: { type: Object, required: true },
  // 好友备注信息 { remark, nickname }，非好友为 null
  friendInfo: { type: Object, default: null },
  // 当前备注（父组件维护的好友备注缓存派生）
  remark: { type: String, default: '' },
  remarkSaving: { type: Boolean, default: false },
  muteDnd: { type: Boolean, default: false },
  isNoPersist: { type: Boolean, default: false },
  // 保存备注函数 prop（父组件实现，返回 Promise<boolean>）：
  // 相比 emit 返回值更可靠，成功时退出编辑态隐藏保存/取消按钮
  onSaveRemark: { type: Function, default: null },
})

const emit = defineEmits([
  'update:muteDnd', 'send-message', 'open-search', 'toggle-no-persist', 'start-voice-call',
])

// 备注编辑为面板局部状态：切换会话时由父组件 :key 重挂载自动复位
const editingRemark = ref(false)
const remarkDraft = ref('')

function startEditRemark() {
  remarkDraft.value = props.remark
  editingRemark.value = true
}

function cancelEditRemark() {
  editingRemark.value = false
}

async function saveRemark() {
  if (!props.onSaveRemark) return
  // 等待父组件保存结果：成功 → 退出编辑态（保存/取消按钮隐藏）；失败 → 保留编辑态与草稿（父组件已 toast 提示）
  const ok = await props.onSaveRemark(remarkDraft.value)
  if (ok) editingRemark.value = false
}
</script>

<template>
  <div class="profile-header">
    <div class="avatar big" :style="{ background: meta.color }">
      <span>{{ meta.avatar }}</span>
    </div>
    <div class="profile-name">{{ meta.name }}</div>
    <div class="profile-id">WorkChat ID：{{ meta.id === 'lin' ? 'lin_wan' : meta.id }}</div>
  </div>

  <div class="action-row">
    <button class="btn-primary" @click="emit('send-message')">
      <span>发消息</span>
    </button>
    <button class="btn-outline" @click="emit('start-voice-call')">
      <span>语音通话</span>
    </button>
  </div>

  <div class="divider-line"></div>

  <!-- 好友备注：点击可编辑，保存后全会话展示名同步更新（仅好友展示） -->
  <div class="info-row remark-row" v-if="friendInfo">
    <span class="info-label">备注</span>
    <template v-if="editingRemark">
      <input
        class="remark-input"
        v-model="remarkDraft"
        maxlength="32"
        placeholder="输入备注名"
        @keydown.enter.prevent="saveRemark"
        @keydown.esc="cancelEditRemark"
      />
      <button class="remark-btn primary" :disabled="remarkSaving" @click="saveRemark">保存</button>
      <button class="remark-btn" :disabled="remarkSaving" @click="cancelEditRemark">取消</button>
    </template>
    <span v-else class="info-value remark-value" title="点击修改备注" @click="startEditRemark">
      {{ remark || '设置备注' }}
    </span>
  </div>
  <div class="info-row">
    <span class="info-label">标签</span>
    <div class="tag-group">
      <span class="tag">产品</span>
      <span class="tag">同事</span>
    </div>
  </div>
  <div class="info-row clickable" @click="emit('open-search')">
    <div class="info-left">
      <svg viewBox="0 0 16 16" width="16" height="16" class="row-ico">
        <circle cx="6.67" cy="6.67" r="4.67" fill="none" stroke="currentColor" stroke-width="1.3" />
        <line x1="10.67" y1="10.67" x2="13.33" y2="13.33" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" />
      </svg>
      <span>查找聊天记录</span>
    </div>
    <svg viewBox="0 0 16 16" width="16" height="16" class="chevron row-ico">
      <path d="M6 4l4 4-4 4" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
  </div>

  <!-- 图片与文件（S8）：已取消展示 -->
  <div class="info-row">
    <span class="info-value">消息免打扰</span>
    <button class="toggle" :class="{ on: muteDnd }" @click="emit('update:muteDnd', !muteDnd)">
      <span class="toggle-dot"></span>
    </button>
  </div>

  <div class="info-row">
    <span class="info-value">聊天记录不落盘</span>
    <button class="toggle" :class="{ on: isNoPersist }" @click="emit('toggle-no-persist')">
      <span class="toggle-dot"></span>
    </button>
  </div>

  <div class="divider-line"></div>

  <div class="info-row clickable">
    <span class="danger-text">清空聊天记录</span>
    <svg viewBox="0 0 16 16" width="16" height="16" class="chevron">
      <path d="M6 4l4 4-4 4" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
  </div>
</template>

<style scoped>
@import '../styles/profile-panel.css';
</style>
