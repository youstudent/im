<script setup>
// 群聊资料面板（参照设计稿「主窗口-群聊」）：
// 群头像/群名编辑/公告/群文件/成员列表/邀请/群设置/群昵称/转让群主/退出群聊
// gp 为 useGroupPanel 的 reactive 包装对象，状态与方法均由其提供
import { computed } from 'vue'
import MemberPickerModal from './MemberPickerModal.vue'

const props = defineProps({
  gp: { type: Object, required: true },
  muteDnd: { type: Boolean, default: false },
  isNoPersist: { type: Boolean, default: false },
})

const emit = defineEmits(['update:muteDnd', 'open-search', 'toggle-no-persist'])

// 当前登录 uid：转让群主候选列表排除自己
function myUid() {
  try {
    return String(JSON.parse(localStorage.getItem('workchat:me') || '{}').uid || '')
  } catch {
    return ''
  }
}
const transferCandidates = computed(() =>
  (props.gp.groupMeta.members || []).filter((m) => m.uid != null && String(m.uid) !== myUid())
)
</script>

<template>
  <!-- 资料头部：群头像(成员组合) + 群名称 + 群号 + 成员胶囊 -->
  <div class="profile-header group">
    <div class="avatar-group big" aria-hidden="true">
      <template v-for="(tile, ti) in gp.groupMeta.avatarTiles" :key="ti">
        <span
          class="group-tile"
          :style="{ background: tile }"
          :class="`pos-${ti}`"
        ></span>
      </template>
    </div>
    <div class="profile-name">
      <span>{{ gp.groupDisplayName }}</span>
      <!-- 群名编辑：仅群主/管理员可见 -->
      <button v-if="gp.isGroupAdmin && !gp.editingGroupName" class="group-edit-btn" title="修改群名" @click="gp.startEditGroupName()">
        <svg viewBox="0 0 16 16" width="13" height="13">
          <path d="M11.3 2.3a1 1 0 0 1 1.4 0l1 1a1 1 0 0 1 0 1.4l-7.9 7.9-2.5.7.7-2.5 7.3-7.5z" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linejoin="round" />
        </svg>
      </button>
    </div>
    <!-- 群名编辑态：输入框 + 保存/取消 -->
    <div v-if="gp.isGroupAdmin && gp.editingGroupName" class="group-name-edit">
      <input
        class="remark-input"
        v-model="gp.groupNameDraft"
        maxlength="20"
        placeholder="输入群名"
        @keydown.enter.prevent="gp.saveGroupSettings()"
        @keydown.esc="gp.editingGroupName = false"
      />
      <button class="remark-btn primary" :disabled="gp.savingGroupInfo" @click="gp.saveGroupSettings()">保存</button>
      <button class="remark-btn" :disabled="gp.savingGroupInfo" @click="gp.editingGroupName = false">取消</button>
    </div>
    <div class="profile-id">群号：{{ gp.groupMeta.groupId }}</div>
    <span class="online-pill group-pill">{{ gp.groupMeta.memberCount }} 位成员</span>
  </div>

  <!-- 操作行：邀请成员 / 群设置 -->
  <div class="action-row">
    <button class="btn-primary" @click="gp.openInviteModal()">
      <span>邀请成员</span>
    </button>
    <button class="btn-outline" @click="gp.openGroupSettings()">
      <span>群设置</span>
    </button>
  </div>

  <div class="divider-line"></div>

  <!-- 我在本群的昵称（微信同款）：任何成员可设，展示优先级 备注 > 群昵称 > 用户昵称 -->
  <div class="group-section">
    <div class="section-title">
      <svg viewBox="0 0 16 16" width="16" height="16">
        <circle cx="8" cy="5.5" r="2.6" fill="none" stroke="currentColor" stroke-width="1.3" />
        <path d="M3.2 13.2c0-2.3 2.2-3.8 4.8-3.8s4.8 1.5 4.8 3.8" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" />
      </svg>
      <span>我在本群的昵称</span>
      <button v-if="!gp.editingMyNickname" class="section-more" @click="gp.startEditMyNickname()">编辑 ›</button>
    </div>
    <template v-if="gp.editingMyNickname">
      <!-- 编辑行：输入框与保存/取消同行展示 -->
      <div class="nickname-edit-row">
        <input
          class="remark-input"
          v-model="gp.myNicknameDraft"
          maxlength="32"
          placeholder="输入群内昵称（留空恢复昵称）"
          @keydown.enter.prevent="gp.saveMyNickname()"
          @keydown.esc="gp.editingMyNickname = false"
        />
        <button class="remark-btn primary" :disabled="gp.savingNickname" @click="gp.saveMyNickname()">保存</button>
        <button class="remark-btn" :disabled="gp.savingNickname" @click="gp.editingMyNickname = false">取消</button>
      </div>
    </template>
    <p v-else class="announcement-text">{{ gp.groupInfo.myNickname || '未设置（使用我的昵称）' }}</p>
  </div>

  <!-- 群公告摘要：群主/管理员可编辑 -->
  <div class="group-section">
    <div class="section-title">
      <svg viewBox="0 0 16 16" width="16" height="16">
        <path d="M2 8c0-1.1.9-2 2-2h8a2 2 0 0 1 2 2v3.5a2 2 0 0 1-2 2H6l-2.6 1.8V13.5H4A2 2 0 0 1 2 11.5V8z" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round" />
        <path d="M6 8.2h4M6 9.8h3" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
      </svg>
      <span>群公告</span>
      <button v-if="gp.isGroupAdmin && !gp.editingAnnouncement" class="section-more" @click="gp.startEditAnnouncement()">编辑 ›</button>
    </div>
    <template v-if="gp.isGroupAdmin && gp.editingAnnouncement">
      <textarea
        class="announcement-edit"
        v-model="gp.announcementDraft"
        maxlength="500"
        rows="4"
        placeholder="输入群公告"
      ></textarea>
      <div class="announcement-actions">
        <button class="remark-btn primary" :disabled="gp.savingGroupInfo" @click="gp.saveGroupSettings()">保存</button>
        <button class="remark-btn" :disabled="gp.savingGroupInfo" @click="gp.editingAnnouncement = false">取消</button>
      </div>
    </template>
    <p v-else class="announcement-text">{{ gp.groupMeta.announcement || '暂无公告' }}</p>
  </div>

  <!-- 图片与文件（G9/S8）：已取消展示 -->

  <!-- 群成员列表：支持搜索 + 上下滚动 -->
  <div class="group-section member-section">
    <div class="section-title">
      <svg viewBox="0 0 16 16" width="16" height="16">
        <circle cx="6" cy="6" r="2.4" fill="none" stroke="currentColor" stroke-width="1.3" />
        <path d="M2.5 13c0-2 1.6-3.2 3.5-3.2S9.5 11 9.5 13" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" />
        <path d="M10 4.5h3.5M11.75 2.75v3.5" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" />
      </svg>
      <span>群成员 ({{ gp.groupMeta.memberCount }})</span>
    </div>

    <!-- 成员搜索框 -->
    <div class="member-search">
      <svg viewBox="0 0 16 16" width="14" height="14" class="member-search-icon">
        <circle cx="6.67" cy="6.67" r="4.67" fill="none" stroke="currentColor" stroke-width="1.3" />
        <line x1="10.67" y1="10.67" x2="13.33" y2="13.33" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" />
      </svg>
      <input
        v-model="gp.memberSearch"
        class="member-search-input"
        type="text"
        :placeholder="`搜索 ${gp.groupMeta.memberCount} 位成员`"
        aria-label="搜索群成员"
      />
      <button v-if="gp.memberSearch" class="member-search-clear" aria-label="清空搜索" @click="gp.memberSearch = ''">
        <svg viewBox="0 0 12 12" width="12" height="12">
          <line x1="3" y1="3" x2="9" y2="9" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
          <line x1="9" y1="3" x2="3" y2="9" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
        </svg>
      </button>
    </div>

    <!-- 搜索结果状态 -->
    <div v-if="gp.memberSearch && gp.filteredMembers.length === 0" class="member-empty">
      <p>未找到匹配的群成员</p>
    </div>

    <!-- 可滚动的成员列表：角色标签（群主/管理员）+ 有管理权限者 hover 显示设管理员/移除按钮 -->
    <div v-else class="member-scroll">
      <div class="member-grid">
        <div
          v-for="(member, mi) in gp.filteredMembers"
          :key="mi"
          class="member-cell"
        >
          <div class="member-avatar" :style="{ background: member.color }">
            <span>{{ member.avatar }}</span>
          </div>
          <span class="member-name" :title="gp.memberDisplayName(member)">
            {{ gp.memberDisplayName(member) }}
            <em v-if="member.role === 0" class="role-tag owner">群主</em>
            <em v-else-if="member.role === 1" class="role-tag admin">管理员</em>
            <em v-if="member.mutedUntil > Date.now()" class="role-tag muted">已禁言</em>
          </span>
          <!-- 成员管理操作（hover 显示）：群主可设/撤管理员与移除；管理员仅可移除普通成员；禁言（G8）两者均可对普通成员 -->
          <div v-if="gp.canOperateMember(member)" class="member-ops">
            <button
              v-if="gp.isGroupOwner"
              class="member-op-btn"
              :title="member.role === 1 ? '撤销管理员' : '设为管理员'"
              @click.stop="gp.setMemberRole(member, member.role === 1 ? 2 : 1)"
            >
              <svg viewBox="0 0 16 16" width="12" height="12">
                <path d="M8 1.8l1.8 3.9 4.2.5-3.1 2.9.8 4.1L8 11.2l-3.7 2 .8-4.1L2 6.2l4.2-.5z" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linejoin="round" />
              </svg>
            </button>
            <button
              class="member-op-btn"
              :title="member.mutedUntil > Date.now() ? '解除禁言' : '禁言'"
              @click.stop="gp.muteMember(member, member.mutedUntil > Date.now() ? 0 : Date.now() + 24 * 60 * 60 * 1000)"
            >
              <svg viewBox="0 0 16 16" width="12" height="12">
                <path d="M3 7v4a5 5 0 0 0 10 0V7" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" />
                <path d="M3 8a5 5 0 0 1 10 0" fill="none" stroke="currentColor" stroke-width="1.3" />
                <line x1="8" y1="2" x2="8" y2="4" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" />
              </svg>
            </button>
            <button class="member-op-btn danger" title="移除成员" @click.stop="gp.removeMember(member)">
              <svg viewBox="0 0 16 16" width="12" height="12">
                <path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
              </svg>
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>

  <div class="profile-footer">
    <div class="divider-line"></div>

    <!-- 查找聊天记录：行结构/样式与单聊完全一致，与免打扰/不落盘同组连续展示 -->
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
    <div class="info-row clickable" @click="gp.askLeaveGroup()">
      <span class="danger-text">{{ gp.leavingGroup ? '退出中…' : '退出群聊' }}</span>
    </div>
    <!-- 转让群主：仅群主可见（后端鉴权兑底），转让后原群主自动变普通成员 -->
    <div v-if="gp.isGroupOwner" class="info-row clickable" @click="gp.openTransferModal()">
      <span class="info-value">转让群主</span>
      <span class="section-more">›</span>
    </div>
  </div>

  <!-- 转让群主：成员选择弹窗（排除自己） -->
  <MemberPickerModal
    v-if="gp.showTransferModal"
    :members="transferCandidates"
    title="选择新群主"
    confirm-text="确认转让"
    :loading="gp.transferring"
    @close="gp.showTransferModal = false"
    @confirm="gp.confirmTransfer($event)"
  />
</template>

<style scoped>
@import '../styles/profile-panel.css';

/* ===== 群聊资料面板 ===== */
/* 群成员头像组合（4 宫格） */
.avatar-group {
  position: relative;
  border-radius: 999px;
  overflow: hidden;
  background: var(--im-primary);
  flex-shrink: 0;
}

.avatar-group.big {
  width: 72px;
  height: 72px;
  display: grid;
  grid-template-columns: 1fr 1fr;
  grid-template-rows: 1fr 1fr;
  gap: 2px;
  padding: 4px;
  background: var(--im-surface-2);
  border: 1px solid var(--im-border);
}

.group-tile {
  display: block;
  width: 100%;
  height: 100%;
  border-radius: 999px;
}

.profile-header.group {
  padding-top: 20px;
}

.group-pill {
  color: var(--im-text-secondary);
}

/* 分组区块 */
.group-section {
  padding: 12px 16px;
}

.section-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 0.929rem;
  font-weight: 600;
  color: var(--im-text-title);
  margin-bottom: 8px;
}

.section-title svg {
  color: var(--im-primary);
  flex-shrink: 0;
}

.section-more {
  margin-left: auto;
  font-size: 0.857rem;
  font-weight: 400;
  color: var(--im-text-muted);
  cursor: pointer;
  border: none;
  background: none;
  padding: 0;
}

.section-more:hover {
  color: var(--im-primary);
}

/* 群公告摘要 */
.announcement-text {
  margin: 0;
  padding: 10px 12px;
  background: var(--im-surface-2);
  border-radius: 8px;
  font-size: 0.857rem;
  line-height: 18px;
  color: var(--im-text-secondary);
  word-break: break-word;
  white-space: pre-wrap;
}

/* 群设置编辑（群名/公告）：仅群主/管理员可见的入口与编辑区 */
.group-edit-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  margin-left: 6px;
  border: none;
  border-radius: 5px;
  background: none;
  color: var(--im-text-muted);
  cursor: pointer;
  vertical-align: middle;
}

.group-edit-btn:hover {
  color: var(--im-primary);
  background: var(--im-surface-2);
}

.group-name-edit {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
  width: 100%;
}

.announcement-edit {
  width: 100%;
  box-sizing: border-box;
  padding: 10px 12px;
  font-size: 0.857rem;
  line-height: 18px;
  color: var(--im-text-title);
  background: var(--im-surface-2);
  border: 1px solid var(--im-border, #e2e8f0);
  border-radius: 8px;
  resize: vertical;
  outline: none;
  font-family: inherit;
}

.announcement-edit:focus {
  border-color: var(--im-primary);
}

.announcement-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 8px;
}

/* 我在本群的昵称编辑行：输入框自适应拉伸，保存/取消同行右置 */
.nickname-edit-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* 群文件入口 */
.file-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.file-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 8px;
  border-radius: 8px;
  cursor: pointer;
}

.file-item:hover {
  background: var(--im-surface-2);
}

.file-ico {
  flex-shrink: 0;
}

.file-meta {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}

.file-name {
  font-size: 0.929rem;
  color: var(--im-text-title);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.file-size {
  font-size: 0.786rem;
  color: var(--im-text-muted);
}

/* 群成员网格：一行两个（群主/管理员优先展示，其余按入群时间） */
.member-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 8px;
}

.member-cell {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding: 8px 4px;
  border-radius: 8px;
  cursor: pointer;
  position: relative;
}

.member-cell:hover {
  background: var(--im-surface-2);
}

.member-avatar {
  width: 40px;
  height: 40px;
  border-radius: 999px;
  color: #fff;
  font-size: 1.071rem;
  font-weight: 500;
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
}

/* 成员搜索框 */
.member-search {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 10px;
  margin-bottom: 8px;
  background: var(--im-surface-2);
  border-radius: 8px;
  border: 1px solid transparent;
  transition: border-color 0.15s ease;
}

.member-search:focus-within {
  border-color: var(--im-primary);
  background: var(--im-surface);
}

.member-search-icon {
  flex-shrink: 0;
}

.member-search-input {
  flex: 1;
  min-width: 0;
  border: none;
  background: transparent;
  outline: none;
  font-family: inherit;
  font-size: 0.929rem;
  color: var(--im-text-title);
}

.member-search-input::placeholder {
  color: var(--im-text-muted);
}

.member-search-clear {
  width: 20px;
  height: 20px;
  border: none;
  background: transparent;
  border-radius: 999px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  flex-shrink: 0;
}

.member-search-clear:hover {
  background: var(--im-hover-gray);
}

/* 成员列表滚动区 */
.member-scroll {
  max-height: 216px;
  overflow-y: auto;
  overscroll-behavior: contain;
  margin-right: -4px;
  padding-right: 4px;
}

.member-scroll::-webkit-scrollbar {
  width: 6px;
}

.member-scroll::-webkit-scrollbar-thumb {
  background: transparent;
  border-radius: 3px;
}

.member-scroll:hover::-webkit-scrollbar-thumb {
  background: rgba(0, 0, 0, 0.15);
}

/* 搜索无结果 */
.member-empty {
  padding: 24px 12px;
  text-align: center;
}

.member-empty p {
  margin: 0;
  font-size: 0.929rem;
  color: var(--im-text-muted);
}

.member-name {
  font-size: 0.786rem;
  color: var(--im-text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
}

/* 角色标签：群主金色 / 管理员橙色（微信风格） */
.role-tag {
  font-style: normal;
  font-size: 0.643rem;
  line-height: 1;
  padding: 2px 4px;
  border-radius: 4px;
  margin-left: 2px;
  vertical-align: 1px;
}

.role-tag.owner {
  color: #b45309;
  background: rgba(245, 158, 11, 0.15);
}

.role-tag.admin {
  color: #c2612c;
  background: rgba(249, 115, 22, 0.12);
}

/* G8 禁言标签：灰色底，与角色标签区分 */
.role-tag.muted {
  color: var(--im-text-secondary);
  background: var(--im-surface-2);
}

:global([data-theme='dark']) .role-tag.owner {
  color: #fbbf24;
}

:global([data-theme='dark']) .role-tag.admin {
  color: #fb923c;
}

:global([data-theme='dark']) .role-tag.muted {
  color: #94a3b8;
}

/* 成员管理操作：默认隐藏，hover 成员卡片时右上角浮现 */
.member-ops {
  position: absolute;
  top: 2px;
  right: 2px;
  display: none;
  gap: 3px;
}

.member-cell:hover .member-ops {
  display: flex;
}

.member-op-btn {
  width: 20px;
  height: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--im-border);
  border-radius: 6px;
  background: var(--im-surface);
  color: var(--im-text-secondary);
  cursor: pointer;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
}

.member-op-btn:hover {
  color: var(--im-primary);
  border-color: var(--im-primary);
}

.member-op-btn.danger:hover {
  color: var(--im-danger, #ef4444);
  border-color: var(--im-danger, #ef4444);
}
</style>
