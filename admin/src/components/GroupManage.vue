<script setup>
import { ref, computed, nextTick, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { adminApi } from '../api/admin'
import { setToken } from '../api/http'
import { useUi } from '../composables/useUi'

const router = useRouter()
const { openConfirm, showToast } = useUi()

// 登录态失效：清空令牌并跳转登录页
function handleUnauth() {
  setToken('')
  router.replace('/login')
}

const groups = ref([])
const groupsTotal = ref(0)
const groupPage = ref(1)
const groupKeyword = ref('')
const pageSize = 10

const groupPageCount = computed(() => Math.max(1, Math.ceil(groupsTotal.value / pageSize)))

function fmtTime(unixSec) {
  if (!unixSec) return '-'
  const d = new Date(unixSec * 1000)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

async function loadGroups() {
  try {
    const offset = (groupPage.value - 1) * pageSize
    const res = await adminApi.listGroups(offset, pageSize, groupKeyword.value)
    groups.value = res.list || []
    groupsTotal.value = res.total || 0
  } catch (e) {
    if (e.code === 401) handleUnauth()
  }
}

// 群搜索：重置到第 1 页并重新加载
function searchGroups() {
  groupPage.value = 1
  loadGroups()
}
// 清空群搜索
function clearGroupSearch() {
  if (!groupKeyword.value) return
  groupKeyword.value = ''
  groupPage.value = 1
  loadGroups()
}

function deleteGroup(gUid) {
  openConfirm({
    title: '解散群组',
    message: '确定要解散该群吗？解散后群成员将被移除，且无法恢复。',
    confirmText: '确认解散',
    danger: true,
    onConfirm: async () => {
      try {
        await adminApi.deleteGroup(gUid)
        loadGroups()
        showToast('群组已解散', 'ok')
      } catch (e) {
        showToast(e.message || '操作失败', 'error')
      }
    },
  })
}

// ---- 群聊天记录弹框 ----
const recordVisible = ref(false)
const recordLoading = ref(false)
const recordGroup = ref(null)
const recordMessages = ref([])
const recordNextSeq = ref(0)
const recordListRef = ref(null)

// 滚动聊天记录容器到底部（展示最新消息）
function scrollRecordToBottom() {
  if (recordListRef.value) {
    recordListRef.value.scrollTop = recordListRef.value.scrollHeight
  }
}

// 消息类型文案
function msgTypeText(t) {
  return { 2: '图片', 3: '文件', 4: '语音', 5: '视频', 6: '系统消息' }[t] || '文本'
}

// 展示消息内容：已撤回显示撤回提示；媒体显示占位；文本显示原文
function msgBody(m) {
  if (m.status === 1) return '已撤回'
  const t = Number(m.type)
  if (t === 1 || t === 6) return m.content || ''
  return `[${msgTypeText(t)}]`
}

// 是否群主发的消息（群主在右，其他成员在左）
function isOwner(m) {
  return recordGroup.value && Number(m.sender_uid) === Number(recordGroup.value.owner_uid)
}

// 打开聊天记录弹框并加载最近消息
async function viewRecords(g) {
  recordGroup.value = g
  recordVisible.value = true
  recordLoading.value = true
  recordMessages.value = []
  recordNextSeq.value = 0
  try {
    const res = await adminApi.groupMessages(g.g_uid, 0, 50)
    // 服务端按 seq 升序返回（早→晚），聊天窗口最新消息在最下方，直接采用即可
    recordMessages.value = res.list || []
    if (recordMessages.value.length) {
      recordNextSeq.value = recordMessages.value[0].seq // 最早消息的 seq，用于加载更早
    }
    // 打开后自动滚动到底部（展示最新消息）
    await nextTick()
    scrollRecordToBottom()
  } catch (e) {
    if (e.code === 401) handleUnauth()
    showToast(e.message || '加载聊天记录失败', 'error')
  } finally {
    recordLoading.value = false
  }
}

// 加载更多（更早的消息）
async function loadMoreRecords() {
  if (recordLoading.value || !recordGroup.value || recordNextSeq.value <= 0) return
  recordLoading.value = true
  try {
    const res = await adminApi.groupMessages(recordGroup.value.g_uid, recordNextSeq.value, 50)
    // 更早的消息（升序）拼接到当前列表前面，保持整体时间升序（最新在底部）
    const older = res.list || []
    recordMessages.value = older.concat(recordMessages.value)
    if (older.length) {
      recordNextSeq.value = older[0].seq // 这批最早消息的 seq，用于继续向上翻页
    } else {
      recordNextSeq.value = 0 // 没有更早消息了
    }
  } catch (e) {
    if (e.code === 401) handleUnauth()
    showToast(e.message || '加载失败', 'error')
  } finally {
    recordLoading.value = false
  }
}

function closeRecords() {
  recordVisible.value = false
  recordMessages.value = []
  recordGroup.value = null
}

onMounted(loadGroups)
</script>

<template>
  <section class="content">
    <h2>群组管理 <span class="count">共 {{ groupsTotal }} 个</span></h2>
    <div class="toolbar">
      <div class="search-box">
        <input v-model="groupKeyword" class="search-input" placeholder="搜索群名 / 群号"
          @keyup.enter="searchGroups" />
        <button v-if="groupKeyword" class="link-btn" @click="clearGroupSearch">清空</button>
        <button class="btn" @click="searchGroups">搜索</button>
      </div>
    </div>
    <table class="table">
      <thead><tr><th>群号</th><th>群名</th><th>群主</th><th>成员数</th><th>创建时间</th><th>操作</th></tr></thead>
      <tbody>
        <tr v-for="g in groups" :key="g.g_uid">
          <td>{{ g.g_uid }}</td>
          <td>{{ g.name }}</td>
          <td>{{ g.owner_uid }}</td>
          <td>{{ g.member_count }}</td>
          <td>{{ fmtTime(g.created_at) }}</td>
          <td>
            <button class="link-btn" @click="viewRecords(g)">查看记录</button>
            <button class="link-btn danger" @click="deleteGroup(g.g_uid)">解散</button>
          </td>
        </tr>
        <tr v-if="!groups.length"><td colspan="6" class="empty">暂无数据</td></tr>
      </tbody>
    </table>
    <div class="pager">
      <button :disabled="groupPage <= 1" @click="groupPage--; loadGroups()">上一页</button>
      <span>{{ groupPage }} / {{ groupPageCount }}</span>
      <button :disabled="groupPage >= groupPageCount" @click="groupPage++; loadGroups()">下一页</button>
    </div>
  </section>

  <!-- 群聊天记录弹框 -->
  <div v-if="recordVisible" class="modal-mask" @click.self="closeRecords">
    <div class="modal-card record-modal">
      <div class="record-head">
        <h3 class="record-title">群聊记录 · {{ recordGroup ? recordGroup.name : '' }}</h3>
        <button class="record-close" @click="closeRecords">✕</button>
      </div>
      <div ref="recordListRef" class="record-list">
        <div v-if="recordLoading && !recordMessages.length" class="record-empty">加载中…</div>
        <div v-else-if="!recordMessages.length" class="record-empty">暂无聊天记录</div>
        <div v-else>
          <div v-if="recordNextSeq > 0" class="record-load-more">
            <button class="btn" @click="loadMoreRecords" :disabled="recordLoading">
              {{ recordLoading ? '加载中…' : '加载更早消息' }}
            </button>
          </div>
          <div
            v-for="m in recordMessages"
            :key="m.msg_id"
            class="record-msg-row"
            :class="{ mine: isOwner(m) }"
          >
            <div class="record-msg">
              <div class="record-msg-head">
                <span class="record-msg-sender">{{ m.sender_name }}</span>
                <span class="record-msg-time">{{ fmtTime(m.created_at) }}</span>
              </div>
              <div class="record-msg-body" :class="{ recalled: m.status === 1 }">{{ msgBody(m) }}</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 群聊天记录弹框 */
.record-modal {
  width: 640px;
  max-width: calc(100vw - 40px);
  padding: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.record-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border);
  background: var(--surface-2);
}

.record-title {
  margin: 0;
  font-size: 16px;
  color: var(--text);
}

.record-close {
  background: none;
  border: none;
  font-size: 16px;
  color: var(--text-muted);
  cursor: pointer;
  padding: 4px;
  line-height: 1;
}

.record-close:hover { color: var(--text); }

.record-list {
  height: 460px;
  overflow-y: auto;
  padding: 12px 16px;
}

.record-empty {
  text-align: center;
  color: var(--text-muted);
  padding: 60px 0;
  font-size: 13px;
}

.record-load-more {
  text-align: center;
  padding: 6px 0 12px;
}

/* 左右气泡结构 */
.record-msg-row {
  display: flex;
  margin-bottom: 12px;
}

/* 其他成员：靠左 */
.record-msg-row .record-msg {
  max-width: 78%;
}

/* 群主：靠右 */
.record-msg-row.mine {
  justify-content: flex-end;
}

.record-msg-row.mine .record-msg {
  align-items: flex-end;
}

.record-msg {
  display: flex;
  flex-direction: column;
}

.record-msg-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 4px;
  padding: 0 4px;
}

.record-msg-sender {
  font-weight: 600;
  color: var(--text);
  font-size: 12px;
}

.record-msg-time {
  color: var(--text-muted);
  font-size: 11px;
}

.record-msg-body {
  padding: 9px 13px;
  border-radius: 12px;
  background: var(--surface-2);
  border: 1px solid var(--border);
  color: var(--text);
  font-size: 14px;
  line-height: 1.6;
  word-break: break-word;
}

/* 群主气泡：蓝色 */
.record-msg-row.mine .record-msg-body {
  background: var(--primary);
  border-color: var(--primary);
  color: #fff;
}

/* 已撤回 */
.record-msg-body.recalled {
  color: var(--text-muted);
  font-style: italic;
}
.record-msg-row.mine .record-msg-body.recalled {
  background: var(--primary-soft-2);
  border-color: var(--border-strong);
  color: var(--text-muted);
}
</style>
