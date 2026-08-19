<script setup>
import { ref, computed, nextTick, onMounted, onBeforeUnmount, watch } from 'vue'
import { localdb } from '../api/localdb'

const emit = defineEmits(['close', 'jump'])

// props：父组件传入会话列表（包含消息）用于检索
const props = defineProps({
  conversations: {
    type: Array,
    default: () => [],
  },
})

// ===== 状态 =====
const query = ref('')                  // 搜索关键字
const filterType = ref('all')          // all | image | file | link
const searchInputEl = ref(null)        // 搜索输入框引用
const resultListEl = ref(null)         // 结果列表容器引用

// 调试钩子：URL hash 含 `?searchkw=xxx` 或 `#search?kw=xxx` 时预填关键字
function readInitialQuery() {
  if (typeof window === 'undefined') return ''
  const raw = window.location.hash || window.location.search
  const m = raw.match(/(?:searchkw|kw)=([^&?#;,]+)/)
  if (m) return decodeURIComponent(m[1])
  return ''
}
query.value = readInitialQuery()

// ===== 过滤标签 =====
const filters = [
  { key: 'all',   label: '全部' },
  { key: 'image', label: '图片' },
  { key: 'file',  label: '文件' },
  { key: 'link',  label: '链接' },
]

function selectFilter(key) {
  filterType.value = key
}

// ===== 关键字高亮 =====
/**
 * 把摘要文本按关键字切成片段，命中片段用 <mark> 包裹以高亮。
 * 注意：标记使用响应式 ref 的样式，避免污染全局。
 */
function splitWithKeyword(text, kw) {
  const safeText = String(text ?? '')
  const safeKw = String(kw ?? '').trim()
  if (!safeKw) return [{ text: safeText, hit: false }]
  const parts = []
  let cursor = 0
  const lower = safeText.toLowerCase()
  const lowerKw = safeKw.toLowerCase()
  while (cursor < safeText.length) {
    const idx = lower.indexOf(lowerKw, cursor)
    if (idx === -1) {
      parts.push({ text: safeText.slice(cursor), hit: false })
      break
    }
    if (idx > cursor) {
      parts.push({ text: safeText.slice(cursor, idx), hit: false })
    }
    parts.push({ text: safeText.slice(idx, idx + safeKw.length), hit: true })
    cursor = idx + safeKw.length
  }
  return parts
}

// ===== 会话类型标签 =====
// 单聊/群聊展示标签（用于结果项的"联系人 · 类型"行）
function chatTypeLabel(c) {
  return c.type === 'group' ? '群聊' : '一对一'
}

// ===== 检索结果 =====
// 本地库全局搜索优先（跨全部会话与全量历史，离线可用）；
// 浏览器调试环境（无本地库）降级为内存会话内搜索。
const allHits = ref([])
const searching = ref(false)

// 过滤标签 → 本地库 type 条件：2 图片 / 3 文件 / 4 链接
const filterTypeMap = { all: 0, image: 2, file: 3, link: 4 }

function formatServerTime(unixSec) {
  if (!unixSec) return ''
  const diff = Math.floor(Date.now() / 1000) - unixSec
  if (diff < 60) return '刚刚'
  if (diff < 3600) return `${Math.floor(diff / 60)}分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)}小时前`
  const d = new Date(unixSec * 1000)
  return `${d.getMonth() + 1}月${d.getDate()}日 ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

// 本地库行 → 命中项（会话展示资料从 props.conversations 按 conv_id 匹配）
function localRowToHit(row) {
  const kw = query.value.trim()
  const convIdStr = String(row.conv_id)
  const conv = (props.conversations || []).find((c) => String(c.convId) === convIdStr)
  let summary = row.content || ''
  // 群聊命中项带发送者名前缀，便于区分发言人
  if (conv && conv.type === 'group' && row.sender_name) summary = `${row.sender_name}: ${summary}`
  // 摘要截取关键字附近片段，避免超长消息铺满结果行
  const lower = summary.toLowerCase()
  const idx = kw ? lower.indexOf(kw.toLowerCase()) : -1
  if (kw && idx > 30) summary = '…' + summary.slice(idx - 20)
  if (summary.length > 120) summary = summary.slice(0, 120) + '…'
  return {
    id: `${convIdStr}:${row.server_id}`,
    conversationId: conv ? conv.id : `conv-${convIdStr}`,
    messageId: row.server_id,
    seq: Number(row.seq) || 0,
    conversationName: conv ? conv.name : `会话 ${convIdStr}`,
    chatType: conv && conv.type === 'group' ? '群聊' : '一对一',
    avatar: conv ? conv.avatar : '?',
    color: conv ? conv.color : '#64748b',
    summary,
    time: formatServerTime(row.created_at),
    kw,
  }
}

// 本地库搜索（主路径）
async function dbSearch() {
  const kw = query.value.trim()
  const rows = await localdb.messages.search(kw, { type: filterTypeMap[filterType.value] || 0, limit: 50 })
  return (rows || []).map(localRowToHit)
}

// 内存兜底搜索（浏览器调试环境无本地库时，仅能搜已加载会话的消息）
function localSearch() {
  const kw = query.value.trim()
  const hits = []
  for (const c of props.conversations) {
    const msgs = c.messages ?? []
    for (const m of msgs) {
      const text = m.text ?? ''
      if (kw && !text.toLowerCase().includes(kw.toLowerCase())) continue
      if (filterType.value === 'image' && m.msgType !== 2) continue
      if (filterType.value === 'file' && m.msgType !== 3) continue
      if (filterType.value === 'link' && !/(https?:\/\/|www\.)/i.test(text)) continue
      hits.push({
        id: `${c.id}:${m.id}`,
        conversationId: c.id,
        messageId: m.id,
        seq: Number(m.seq) || 0,
        conversationName: c.name,
        chatType: chatTypeLabel(c),
        avatar: m.avatar,
        color: m.color,
        summary: text,
        time: m.time || '',
        kw,
      })
    }
  }
  return hits
}

// 触发搜索：本地库优先，无本地库降级内存搜索
async function runSearch() {
  const kw = query.value.trim()
  if (!kw && filterType.value === 'all') {
    allHits.value = []
    return
  }
  searching.value = true
  try {
    if (localdb.available()) {
      allHits.value = await dbSearch()
    } else {
      allHits.value = localSearch()
    }
  } catch {
    allHits.value = localSearch()
  } finally {
    searching.value = false
  }
}

// 输入防抖：避免逐键触发搜索
let debounceTimer = null
watch([query, filterType], () => {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(runSearch, 250)
})
onMounted(() => runSearch())

// 按日期分组（今天 / 昨天 / 更早）
const groupedHits = computed(() => {
  const groups = new Map()
  for (const h of allHits.value) {
    const t = h.time
    let label = '更早'
    if (/^\d{2}:\d{2}$/.test(t)) label = '今天'
    else if (/分钟前|小时前|刚刚/.test(t)) label = '今天'
    else if (/^昨天/.test(t)) label = '昨天'
    else if (/周[一二三四五六日]|上周|上上周/.test(t)) label = '本周'
    else if (/^\d{1,2}月\d{1,2}日/.test(t)) label = '更早'

    if (!groups.has(label)) groups.set(label, [])
    groups.get(label).push(h)
  }
  // 稳定的分组顺序
  const order = ['今天', '昨天', '本周', '更早']
  return order.filter((k) => groups.has(k)).map((k) => ({ label: k, items: groups.get(k) }))
})

const totalHits = computed(() => allHits.value.length)
const hasQuery = computed(() => query.value.trim().length > 0)

// 关闭
function closeModal() {
  emit('close')
}

// 点击结果项：通知父组件跳转并关闭弹窗（携带 seq 供目标消息未加载时回补历史）
function onHitClick(hit) {
  emit('jump', {
    conversationId: hit.conversationId,
    messageId: hit.messageId,
    seq: hit.seq,
    keyword: hit.kw,
  })
  closeModal()
}

// ===== 关闭快捷键：Esc =====
function onGlobalKeydown(e) {
  if (e.key === 'Escape') {
    e.preventDefault()
    closeModal()
  }
}
onMounted(() => {
  window.addEventListener('keydown', onGlobalKeydown)
  nextTick(() => {
    searchInputEl.value?.focus()
  })
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onGlobalKeydown)
})
</script>

<template>
  <!-- 全屏遮罩，点击空白处关闭 -->
  <div class="modal-overlay" @click.self="closeModal">
    <div class="modal" role="dialog" aria-modal="true" aria-label="查找聊天记录">
      <!-- 标题栏：参考"设置"窗口（macOS 风格窗口控制） -->
      <header class="title-bar">
        <div class="title-left">
          <div class="app-icon" aria-label="应用图标">
            <span>W</span>
          </div>
        </div>
        <div class="title-center">WorkChat</div>
        <div class="title-right">
          <button class="window-btn close" aria-label="关闭" @click="closeModal">
            <svg viewBox="0 0 12 12" width="16" height="16">
              <line x1="3" y1="3" x2="9" y2="9" stroke="#1f2329" stroke-width="1.2" stroke-linecap="round" />
              <line x1="9" y1="3" x2="3" y2="9" stroke="#1f2329" stroke-width="1.2" stroke-linecap="round" />
            </svg>
          </button>
        </div>
      </header>

      <!-- 搜索区：搜索框 + 过滤标签 -->
      <section class="search-section">
        <div class="search-box">
          <svg viewBox="0 0 20 20" width="20" height="20" class="search-icon">
            <circle cx="8.67" cy="8.67" r="5.83" fill="none" stroke="currentColor" stroke-width="1.6" />
            <line x1="13.75" y1="13.75" x2="17.5" y2="17.5" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
          </svg>
          <input
            ref="searchInputEl"
            v-model="query"
            class="search-input"
            type="text"
            placeholder="搜索聊天记录关键词…"
            aria-label="搜索聊天记录关键词"
          />
        </div>

        <div class="filter-row">
          <button
            v-for="f in filters"
            :key="f.key"
            class="filter-chip"
            :class="{ active: filterType === f.key }"
            @click="selectFilter(f.key)"
          >
            {{ f.label }}
          </button>
        </div>
      </section>

      <!-- 结果区 -->
      <section class="result-section" ref="resultListEl">
        <!-- 空状态：未输入关键字 -->
        <div v-if="!hasQuery && totalHits === 0" class="empty-state">
          <div class="empty-illu" aria-hidden="true">
            <svg viewBox="0 0 64 64" width="56" height="56">
              <circle cx="26" cy="26" r="14" fill="none" stroke="currentColor" stroke-width="2.5" />
              <line x1="37" y1="37" x2="50" y2="50" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" />
            </svg>
          </div>
          <p class="empty-title">输入关键词开始搜索</p>
          <p class="empty-sub">可以搜索消息内容、文件、图片或链接</p>
        </div>

        <!-- 无匹配 -->
        <div v-else-if="totalHits === 0" class="empty-state">
          <div class="empty-illu" aria-hidden="true">
            <svg viewBox="0 0 64 64" width="56" height="56">
              <circle cx="26" cy="26" r="14" fill="none" stroke="currentColor" stroke-width="2.5" />
              <line x1="37" y1="37" x2="50" y2="50" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" />
            </svg>
          </div>
          <p class="empty-title">没有找到匹配的消息</p>
          <p class="empty-sub">试试别的关键词，或切换消息类型筛选</p>
        </div>

        <!-- 分组结果 -->
        <template v-else>
          <div v-for="group in groupedHits" :key="group.label" class="result-group">
            <div class="group-label">{{ group.label }}</div>
            <button
              v-for="hit in group.items"
              :key="hit.id"
              class="hit-item"
              @click="onHitClick(hit)"
            >
              <div class="hit-avatar" :style="{ background: hit.color }">
                <span>{{ hit.avatar }}</span>
              </div>
              <div class="hit-body">
                <div class="hit-title">
                  <span class="hit-name">{{ hit.conversationName }}</span>
                  <span class="hit-dot">·</span>
                  <span class="hit-type">{{ hit.chatType }}</span>
                </div>
                <div class="hit-summary">
                  <template v-for="(seg, si) in splitWithKeyword(hit.summary, hit.kw)" :key="si">
                    <mark v-if="seg.hit" class="hit-mark">{{ seg.text }}</mark>
                    <span v-else>{{ seg.text }}</span>
                  </template>
                </div>
              </div>
              <span class="hit-time">{{ hit.time }}</span>
            </button>
          </div>
        </template>
      </section>
    </div>
  </div>
</template>

<style scoped>
/* 遮罩 */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: var(--im-overlay);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  padding: 40px;
  animation: overlayIn 0.18s ease;
}

@keyframes overlayIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

/* 弹窗主框：宽度参考"添加好友"弹窗（700px），高度固定较小，内容区可滚动 */
.modal {
  width: 100%;
  max-width: 700px;
  height: 560px;
  max-height: calc(100vh - 80px);
  background: var(--im-surface);
  border-radius: 16px;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.18);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  font-family: var(--im-font-family);
  color: var(--im-text-title);
  animation: modalIn 0.18s cubic-bezier(0.4, 0, 0.2, 1);
}

@keyframes modalIn {
  from { transform: translateY(8px) scale(0.99); opacity: 0; }
  to { transform: translateY(0) scale(1); opacity: 1; }
}

/* ===== 标题栏（38px）参考"设置"窗口 ===== */
.title-bar {
  height: 38px;
  background: var(--im-surface-2);
  display: flex;
  align-items: center;
  padding: 0 12px;
  flex-shrink: 0;
  position: relative;
}

.title-left {
  display: flex;
  align-items: center;
  width: 18px;
}

.app-icon {
  width: 18px;
  height: 18px;
  background: var(--im-primary);
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 11px;
  font-weight: 700;
  font-family: var(--im-font-family);
}

.title-center {
  position: absolute;
  left: 50%;
  transform: translateX(-50%);
  font-size: 13px;
  font-weight: 500;
  color: var(--im-text-title);
}

.title-right {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 8px;
  margin-right: 6px;
}

.window-btn {
  width: 32px;
  height: 32px;
  background: transparent;
  border: none;
  padding: 0;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
  transition: background 0.15s;
}

.window-btn:hover {
  background: rgba(0, 0, 0, 0.05);
}

/* 关闭按钮 hover：红色小圆背景，比按钮更紧凑，避免过大色块 */
.window-btn.close {
  position: relative;
}

.window-btn.close:hover::before {
  content: '';
  position: absolute;
  inset: 4px;
  border-radius: 50%;
  background: var(--im-danger);
}

.window-btn.close:hover svg {
  position: relative;
  z-index: 1;
}

.window-btn.close:hover svg line {
  stroke: #fff;
}

/* ===== 搜索区 ===== */
.search-section {
  padding: 24px 48px 14px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  flex-shrink: 0;
}

.search-box {
  height: 48px;
  padding: 0 16px;
  background: var(--im-surface-2);
  border: 1px solid var(--im-border);
  border-radius: 10px;
  display: flex;
  align-items: center;
  gap: 10px;
  transition: border-color 0.15s ease, background-color 0.15s ease;
}

.search-box:focus-within {
  border-color: var(--im-primary);
  background: var(--im-surface);
}

.search-icon {
  flex-shrink: 0;
  color: var(--im-text-muted);
}

.search-input {
  flex: 1;
  min-width: 0;
  height: 100%;
  border: none;
  background: transparent;
  outline: none;
  font-family: inherit;
  font-size: 1rem;
  color: var(--im-text-title);
}

.search-input::placeholder {
  color: var(--im-text-muted);
}

/* 过滤标签 */
.filter-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.filter-chip {
  height: 32px;
  padding: 0 16px;
  background: var(--im-surface-2);
  color: var(--im-text-secondary);
  border: 1px solid transparent;
  border-radius: 16px;
  font-family: inherit;
  font-size: 0.857rem;
  cursor: pointer;
  transition: background-color 0.12s ease, color 0.12s ease;
}

.filter-chip:hover {
  background: var(--im-hover-gray);
}

.filter-chip.active {
  background: rgba(37, 99, 235, 0.1);
  color: var(--im-primary);
  border-color: rgba(37, 99, 235, 0.18);
}

/* ===== 结果区 ===== */
.result-section {
  flex: 1;
  padding: 8px 48px 24px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 10px;
  overscroll-behavior: contain;
}

.result-section::-webkit-scrollbar {
  width: 6px;
}

.result-section::-webkit-scrollbar-thumb {
  background: transparent;
  border-radius: 3px;
}

.result-section:hover::-webkit-scrollbar-thumb {
  background: rgba(0, 0, 0, 0.15);
}

.result-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.group-label {
  font-size: 0.857rem;
  font-weight: 500;
  color: var(--im-text-secondary);
  padding: 0 4px;
}

.hit-item {
  width: 100%;
  min-height: 67px;
  padding: 12px;
  background: var(--im-surface-2);
  border: 1px solid transparent;
  border-radius: 10px;
  display: flex;
  align-items: center;
  gap: 12px;
  cursor: pointer;
  text-align: left;
  font-family: inherit;
  transition: background-color 0.12s ease, border-color 0.12s ease;
}

.hit-item:hover {
  background: var(--im-input-bg);
  border-color: var(--im-border);
}

.hit-item:focus-visible {
  outline: 2px solid var(--im-primary);
  outline-offset: -1px;
}

.hit-avatar {
  width: 40px;
  height: 40px;
  border-radius: 999px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 1rem;
  font-weight: 500;
  flex-shrink: 0;
}

.hit-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.hit-title {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 1rem;
  font-weight: 500;
  color: var(--im-text-title);
}

.hit-dot {
  color: var(--im-text-muted);
}

.hit-type {
  color: var(--im-text-muted);
  font-weight: 400;
}

.hit-summary {
  font-size: 0.929rem;
  color: var(--im-text-secondary);
  line-height: 19px;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 1;
  line-clamp: 1;
  -webkit-box-orient: vertical;
}

.hit-mark {
  background: rgba(245, 158, 11, 0.18);
  color: #b45309;
  border-radius: 3px;
  padding: 0 2px;
  font-weight: 500;
}

.hit-time {
  font-size: 0.857rem;
  color: var(--im-text-muted);
  flex-shrink: 0;
  align-self: center;
}

/* ===== 空状态 ===== */
.empty-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 40px 24px;
  text-align: center;
  color: var(--im-text-muted);
  gap: 6px;
}

.empty-illu {
  color: var(--im-border);
  margin-bottom: 6px;
}

.empty-title {
  margin: 0;
  font-size: 1rem;
  font-weight: 500;
  color: var(--im-text-secondary);
}

.empty-sub {
  margin: 0;
  font-size: 0.857rem;
  color: var(--im-text-muted);
}

/* ===== 深色模式微调 ===== */
:global([data-theme='dark']) .hit-mark {
  background: rgba(245, 158, 11, 0.22);
  color: #fbbf24;
}

/* ===== 触屏优化 ===== */
@media (hover: none) and (pointer: coarse) {
  .hit-item {
    min-height: 76px;
  }
  .filter-chip {
    min-height: 40px;
  }
}

/* ===== 减少动画 ===== */
@media (prefers-reduced-motion: reduce) {
  .modal,
  .modal-overlay {
    transition: none !important;
    animation: none !important;
  }
}
</style>