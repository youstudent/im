<script setup>
// 邀请成员弹框：加载好友列表（过滤已在群成员），勾选后调用后端邀请接口
import { ref, computed, onMounted } from 'vue'
import { friendApi, groupApi } from '../api/social'
import { MOCK_COLOR_POOL } from '../utils/palette'

const props = defineProps({
  // 目标群 g_uid
  targetId: { required: true },
  // 当前群成员 uid 列表：已在群的好友不显示
  memberUids: { type: Array, default: () => [] },
})

const emit = defineEmits(['close', 'invited'])

const inviteFriends = ref([]) // 可邀请的好友（已过滤在群成员）
const inviteSearch = ref('') // 邀请弹框搜索
const selectedInviteUIDs = ref(new Set()) // 勾选的好友 uid

// 过滤后的可邀请好友
const filteredInviteFriends = computed(() => {
  const kw = inviteSearch.value.trim().toLowerCase()
  if (!kw) return inviteFriends.value
  return inviteFriends.value.filter((f) => (f.name || '').toLowerCase().includes(kw))
})

// 打开邀请弹框时请求好友（force=true 刷新缓存）
onMounted(async () => {
  const inGroup = new Set(props.memberUids)
  try {
    const flist = await friendApi.list(true)
    inviteFriends.value = (flist || [])
      .filter((f) => !inGroup.has(f.uid)) // 已在群的好友不显示
      .map((f, i) => ({
        uid: f.uid,
        name: f.nickname || `用户${f.uid}`,
        avatar: (f.nickname || '?')[0],
        color: MOCK_COLOR_POOL[i % MOCK_COLOR_POOL.length],
      }))
  } catch (e) {
    console.warn('[InviteMembersModal] 加载好友失败:', e?.message || e)
    inviteFriends.value = []
  }
})

function toggleInvite(uid) {
  const s = new Set(selectedInviteUIDs.value)
  if (s.has(uid)) s.delete(uid)
  else s.add(uid)
  selectedInviteUIDs.value = s
}

// 确认邀请：成功后通知父组件刷新群成员
// G7 入群确认：群开启 invite_confirm 时后端返回提示（不直接入群），本地展示"已通知群主/管理员"
const inviting = ref(false)
const inviteMsg = ref('') // 邀请结果提示（成功/待确认/失败）
async function confirmInvite() {
  const uids = [...selectedInviteUIDs.value]
  if (!props.targetId || uids.length === 0 || inviting.value) return
  inviting.value = true
  inviteMsg.value = ''
  try {
    await groupApi.invite(props.targetId, uids)
    emit('invited')
  } catch (e) {
    const msg = e?.message || '邀请失败'
    // 入群确认开启：邀请已转交群主/管理员，弹框保留并展示待确认提示
    if (msg.includes('入群确认')) {
      inviteMsg.value = msg
      selectedInviteUIDs.value = new Set() // 清空勾选，避免重复提交
    } else {
      inviteMsg.value = msg
    }
  } finally {
    inviting.value = false
  }
}
</script>

<template>
  <div class="invite-overlay" @click.self="emit('close')">
    <div class="invite-modal">
      <header class="invite-modal-header">
        <h2 class="invite-modal-title">邀请成员</h2>
        <button class="invite-modal-close" aria-label="关闭" @click="emit('close')">
          <svg viewBox="0 0 16 16" width="16" height="16">
            <path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
          </svg>
        </button>
      </header>

      <div class="invite-modal-body">
        <div class="invite-search-row">
          <input
            v-model="inviteSearch"
            class="invite-search-input"
            type="text"
            placeholder="搜索好友"
          />
        </div>

        <div class="invite-hint">已选 {{ selectedInviteUIDs.size }} 位好友（已加入群的好友不显示）</div>
        <div v-if="inviteMsg" class="invite-msg">{{ inviteMsg }}</div>

        <div v-if="filteredInviteFriends.length === 0" class="invite-empty">
          <p>暂无可邀请的好友</p>
        </div>
        <div v-else class="invite-list">
          <button
            v-for="f in filteredInviteFriends"
            :key="f.uid"
            class="invite-item"
            :class="{ selected: selectedInviteUIDs.has(f.uid) }"
            @click="toggleInvite(f.uid)"
          >
            <div class="invite-avatar" :style="{ background: f.color }">
              <span>{{ f.avatar }}</span>
            </div>
            <span class="invite-name">{{ f.name }}</span>
            <span class="invite-check">
              <svg v-if="selectedInviteUIDs.has(f.uid)" viewBox="0 0 16 16" width="16" height="16">
                <path d="M3 8.5l3.5 3.5L13 4.5" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
            </span>
          </button>
        </div>
      </div>

      <footer class="invite-modal-footer">
        <button class="invite-btn-cancel" @click="emit('close')">取消</button>
        <button
          class="invite-btn-confirm"
          :disabled="inviting || selectedInviteUIDs.size === 0"
          @click="confirmInvite"
        >
          {{ inviting ? '邀请中…' : '邀请' }}
        </button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
@import '../styles/modal-base.css';

.invite-search-row {
  flex-shrink: 0;
}

.invite-search-input {
  width: 100%;
  height: 40px;
  padding: 0 14px;
  background: var(--im-surface);
  border: 1px solid var(--im-border);
  border-radius: 10px;
  font-size: 0.929rem;
  font-family: inherit;
  color: var(--im-text-title);
  outline: none;
}

.invite-search-input:focus {
  border-color: var(--im-primary);
}

.invite-hint {
  font-size: 0.786rem;
  color: var(--im-text-muted);
}

.invite-msg {
  font-size: 0.786rem;
  color: var(--im-primary);
  background: rgba(37, 99, 235, 0.08);
  border-radius: 6px;
  padding: 8px 10px;
  line-height: 18px;
}

.invite-empty {
  padding: 32px 0;
  text-align: center;
  color: var(--im-text-muted);
  font-size: 0.929rem;
}

.invite-list {
  flex: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.invite-item {
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

.invite-item:hover {
  background: var(--im-surface-2);
}

.invite-item.selected {
  background: var(--im-soft-blue);
}

.invite-avatar {
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

.invite-name {
  flex: 1;
  min-width: 0;
  font-size: 0.929rem;
  color: var(--im-text-title);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.invite-check {
  color: var(--im-primary);
  flex-shrink: 0;
}
</style>
