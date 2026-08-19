<script setup>
import { ref, computed, nextTick, onMounted, onBeforeUnmount, watch } from 'vue'
import { localdb } from '../api/localdb'
import { messageApi } from '../api/message'

const emit = defineEmits(['close', 'jump'])

// 当前登录用户 uid（区分命中消息是“我”还是好友发送）
const myUid = (() => {
  try {
    return String(JSON.parse(localStorage.getItem('workchat:me') || '{}').uid || '')
  } catch {
    return ''
  }
})()

// props：父组件传入会话列表（包含消息）用于检索；
// convId：仅搜索该会话（当前会话内查找聊天记录）
const props = defineProps({
  conversations: {
    type: Array,
    default: () => [],
  },
  convId: {
    type: [String, Number],
    default: '',
  },
})

// 当前搜索范围的会话（用于标题/占位提示展示）
const scopedConv = computed(() =>
  (props.conversations || []).find((c) => String(c.convId) === String(props.convId)) || null
)

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

// ===== 过滤标签（已取消链接分类） =====
const filters = [
  { key: 'all',   label: '全部' },
  { key: 'image', label: '图片' },
  { key: 'file',  label: '文件' },
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

// ===== 摘要分段：先按链接（http(s):// 或 www. 开头）切分，链接段蓝色展示，
// 非链接段再做关键字高亮切分（参考微信样式） =====
const URL_RE = /(https?:\/\/[^\s<>"']+|www\.[^\s<>"']+)/g
function splitSegments(text, kw) {
  const safeText = String(text ?? '')
  const segs = []
  let last = 0
  URL_RE.lastIndex = 0
  for (const m of safeText.matchAll(URL_RE)) {
    if (m.index > last) segs.push(...splitWithKeyword(safeText.slice(last, m.index), kw))
    segs.push({ text: m[0], hit: false, link: true })
    last = m.index + m[0].length
  }
  if (last < safeText.length) segs.push(...splitWithKeyword(safeText.slice(last), kw))
  if (!segs.length) segs.push({ text: safeText, hit: false })
  return segs
}

// ===== 会话类型标签 =====
// 单聊/群聊展示标签（用于结果项的"联系人 · 类型"行）
function chatTypeLabel(c) {
  return c.type === 'group' ? '群聊' : '一对一'
}

// ===== 检索结果 =====
// 本地库搜索优先（当前会话全量历史，离线可用）；
// 浏览器调试环境（无本地库）降级为内存会话内搜索。
// 结果滚动分页加载：首页 PAGE_SIZE 条，滚近底部自动加载下一页。
const allHits = ref([])
const searching = ref(false)
const hasMore = ref(false)     // 是否还有下一页
const loadingMore = ref(false) // 正在加载下一页（防重复触发）
const PAGE_SIZE = 50
let pageOffset = 0
let searchToken = 0 // 关键字/筛选变更时作废旧分页回调，避免旧页追加到新结果后

// 过滤标签 → 本地库 type 条件：2 图片 / 3 文件（链接分类已取消）
const filterTypeMap = { all: 0, image: 2, file: 3 }

function formatServerTime(unixSec) {
  if (!unixSec) return ''
  const diff = Math.floor(Date.now() / 1000) - unixSec
  if (diff < 60) return '刚刚'
  if (diff < 3600) return `${Math.floor(diff / 60)}分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)}小时前`
  const d = new Date(unixSec * 1000)
  return `${d.getMonth() + 1}月${d.getDate()}日 ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

// extra 解析容错（本地库行的 extra 为 JSON 字符串）
function parseExtra(extra) {
  if (!extra) return {}
  try {
    return typeof extra === 'string' ? JSON.parse(extra) : extra
  } catch {
    return {}
  }
}

// 图片命中项：解析本地文件缓存地址（wcfile://）；未命中会后台下载，下次搜索直接命中。
// 回填时按 id 从响应式数组取代理对象写入，直接改原始对象不会触发视图更新
function hydrateHitThumb(hit) {
  if (!hit.imageUrl || !localdb.available()) return
  localdb.fileCache.resolve(hit.imageUrl, hit.imageKey || '', hit.imageName || '').then((r) => {
    if (!r || !r.hit || !r.cacheUrl) return
    const target = allHits.value.find((h) => h.id === hit.id)
    if (target) target.thumbUrl = r.cacheUrl
  }).catch(() => {})
}

// 本地库行 → 命中项（会话展示资料从 props.conversations 按 conv_id 匹配；
// 携带发送者标识区分我/好友消息；图片消息用缩略图 + [图片] 占位，不展示原始 URL）
function localRowToHit(row) {
  const kw = query.value.trim()
  const convIdStr = String(row.conv_id)
  const conv = (props.conversations || []).find((c) => String(c.convId) === convIdStr)
  const msgType = Number(row.type) || 1
  const extra = parseExtra(row.extra)
  const isMine = row.sender_uid != null && String(row.sender_uid) !== '' && String(row.sender_uid) === myUid
  // 图片地址：优先 extra.url，兜底 content（旧消息可能直接把 URL 存在 content）
  const imageUrl = msgType === 2 ? (extra.url || (/^https?:\/\//.test(row.content || '') ? row.content : '')) : ''
  let summary = row.content || ''
  if (msgType === 2) {
    summary = '[图片]'
  } else if (msgType === 3) {
    summary = extra.name ? `[文件] ${extra.name}` : '[文件]'
  } else {
    // 摘要截取关键字附近片段，避免超长消息铺满结果行
    const lower = summary.toLowerCase()
    const idx = kw ? lower.indexOf(kw.toLowerCase()) : -1
    if (kw && idx > 30) summary = '…' + summary.slice(idx - 20)
    if (summary.length > 120) summary = summary.slice(0, 120) + '…'
  }
  const hit = {
    id: `${convIdStr}:${row.server_id}`,
    conversationId: conv ? conv.id : `conv-${convIdStr}`,
    messageId: row.server_id,
    seq: Number(row.seq) || 0,
    conversationName: conv ? conv.name : `会话 ${convIdStr}`,
    chatType: conv && conv.type === 'group' ? '群聊' : '一对一',
    avatar: conv ? conv.avatar : '?',
    color: conv ? conv.color : '#64748b',
    msgType,
    isMine,
    // 发送者展示：自己显示“我”，好友/群成员显示服务端昵称（兜底会话名）
    senderLabel: isMine ? '我' : (row.sender_name || (conv ? conv.name : (row.sender_uid ? `用户 ${row.sender_uid}` : ''))),
    imageUrl,
    imageKey: extra.key || '',
    imageName: extra.name || '',
    thumbUrl: '',
    summary,
    time: formatServerTime(row.created_at),
    kw,
  }
  hydrateHitThumb(hit)
  return hit
}

// 分页拉取：本地库支持 offset 翻页；内存兜底一次性返回（无分页）
async function fetchPage(offset) {
  if (localdb.available()) {
    const kw = query.value.trim()
    const rows = (await localdb.messages.search(kw, {
      type: filterTypeMap[filterType.value] || 0,
      limit: PAGE_SIZE,
      offset,
      convId: props.convId,
    })) || []
    return { hits: rows.map(localRowToHit), more: rows.length >= PAGE_SIZE }
  }
  return { hits: localSearch(), more: false }
}

// 内存兜底搜索（浏览器调试环境无本地库时，仅能搜当前会话已加载的消息）
function localSearch() {
  const kw = query.value.trim()
  const hits = []
  const convs = (props.conversations || []).filter((c) => String(c.convId) === String(props.convId))
  for (const c of convs) {
    const msgs = c.messages ?? []
    for (const m of msgs) {
      const text = m.text ?? ''
      if (kw) {
        // 图片/文件消息关键字匹配文件名与 URL；文本匹配正文
        const isMedia = m.msgType === 2 || m.msgType === 3
        const haystack = isMedia ? `${m.extra?.name || ''} ${m.extra?.url || ''} ${text}` : text
        if (!haystack.toLowerCase().includes(kw.toLowerCase())) continue
      }
      if (filterType.value === 'image' && m.msgType !== 2) continue
      if (filterType.value === 'file' && m.msgType !== 3) continue
      const isMine = m.type === 'out'
      const imageUrl = m.msgType === 2
        ? (m.extra?.cacheUrl || m.extra?.url || (/^https?:\/\//.test(text) ? text : ''))
        : ''
      hits.push({
        id: `${c.id}:${m.id}`,
        conversationId: c.id,
        messageId: m.id,
        seq: Number(m.seq) || 0,
        conversationName: c.name,
        chatType: chatTypeLabel(c),
        avatar: m.avatar,
        color: m.color,
        msgType: m.msgType || 1,
        isMine,
        senderLabel: isMine ? '我' : (m.senderName || c.name),
        imageUrl,
        thumbUrl: '',
        summary: m.msgType === 2
          ? '[图片]'
          : m.msgType === 3
            ? (m.extra?.name ? `[文件] ${m.extra.name}` : '[文件]')
            : text,
        time: m.time || '',
        kw,
      })
    }
  }
  return hits
}

// 触发搜索（重置到第一页）：本地库优先，无本地库降级内存搜索；
// 空关键字也执行（默认浏览模式：展示当前会话全部记录，滚动分页加载）
async function runSearch() {
  const token = ++searchToken
  searching.value = true
  hasMore.value = false
  pageOffset = 0
  try {
    const { hits, more } = await fetchPage(0)
    if (token !== searchToken) return
    allHits.value = hits
    hasMore.value = more
    pageOffset = hits.length
  } catch {
    if (token !== searchToken) return
    allHits.value = localSearch()
    hasMore.value = false
  } finally {
    if (token === searchToken) searching.value = false
  }
}

// 滚动分页：滚近底部时追加下一页（按 id 去重，防新消息到达导致行移重复）
async function loadMore() {
  if (loadingMore.value || searching.value || !hasMore.value) return
  const token = searchToken
  loadingMore.value = true
  try {
    const { hits, more } = await fetchPage(pageOffset)
    if (token !== searchToken) return
    const known = new Set(allHits.value.map((h) => h.id))
    allHits.value = allHits.value.concat(hits.filter((h) => !known.has(h.id)))
    hasMore.value = more
    pageOffset += hits.length
  } catch {
    hasMore.value = false
  } finally {
    if (token === searchToken) loadingMore.value = false
  }
}

function onResultScroll() {
  const el = resultListEl.value
  if (!el || !hasMore.value || loadingMore.value) return
  if (el.scrollTop + el.clientHeight >= el.scrollHeight - 80) loadMore()
}

// 输入防抖：避免逐键触发搜索
let debounceTimer = null
watch([query, filterType], () => {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(runSearch, 250)
})

// ===== 当前会话历史回填 =====
// 本地库只存已同步过的消息：自己发送的消息发送时即落库，而好友消息只有
// 在线接收或历史拉取过才会落库。若只搜本地库，从未拉取的较早好友消息会
// 缺失（表现为“只加载了我发送的消息”）。打开弹窗时从服务端双向补齐当前
// 会话缺失历史（落库后重搜）；离线时静默降级为仅搜本地已有记录。
let backfilled = false

// 服务端消息 → 本地库行（与 MainWindow.toDbMessage 同规则，回填 upsert 用）
function toDbMessage(m, convId) {
  return {
    server_id: String(m.id),
    conv_id: String(convId || m.conv_id),
    seq: Number(m.seq) || 0,
    sender_uid: m.sender_uid != null ? String(m.sender_uid) : null,
    sender_name: m.sender_name || '',
    type: Number(m.type) || 1,
    content: m.content ?? '',
    extra: typeof m.extra === 'string' ? m.extra : m.extra ? JSON.stringify(m.extra) : '',
    status: Number(m.status) || 0,
    created_at: Number(m.created_at) || 0,
  }
}

async function backfillConvHistory() {
  const convIdStr = String(props.convId || '')
  if (backfilled || !convIdStr || convIdStr === 'none' || !localdb.available()) return
  backfilled = true
  try {
    // 本地已有消息的 seq 窗口（list 最多返 200 条，minSeq 可能偏新，作为向前游标仍安全）
    const rows = (await localdb.messages.list(convIdStr, { limit: 200 })) || []
    let maxSeq = 0
    let minSeq = 0
    for (const r of rows) {
      const s = Number(r.seq) || 0
      if (s > 0) {
        maxSeq = Math.max(maxSeq, s)
        minSeq = minSeq ? Math.min(minSeq, s) : s
      }
    }
    let changed = false

    // 1) 向后补齐：本地为空先拉最新一页；否则本地水位落后时才增量拉取（追平则免网络请求）
    if (maxSeq === 0) {
      const page = await messageApi.getHistory(convIdStr, { limit: 100 })
      if (page && page.length) {
        await localdb.messages.upsert(page.map((m) => toDbMessage(m, convIdStr)))
        changed = true
        for (const m of page) {
          const s = Number(m.seq) || 0
          if (s > 0) {
            maxSeq = Math.max(maxSeq, s)
            minSeq = minSeq ? Math.min(minSeq, s) : s
          }
        }
      }
    } else {
      const convs = (await localdb.conversations.list()) || []
      const convRow = convs.find((c) => String(c.id) === convIdStr)
      const waterMark = Number(convRow && convRow.last_synced_seq) || 0
      if (!(waterMark > 0 && maxSeq >= waterMark)) {
        let afterSeq = maxSeq
        for (let i = 0; i < 10; i++) { // 上限 10 页 = 1000 条
          const page = await messageApi.getHistory(convIdStr, { afterSeq, limit: 100 })
          if (!page || !page.length) break
          await localdb.messages.upsert(page.map((m) => toDbMessage(m, convIdStr)))
          changed = true
          const mx = page.reduce((a, m) => Math.max(a, Number(m.seq) || 0), afterSeq)
          maxSeq = mx
          if (page.length < 100 || mx <= afterSeq) break
          afterSeq = mx
        }
      }
    }

    // 2) 向前补齐：比本地最旧 seq 更早的历史翻页拉取（首次全量回填后 minSeq=1，后续打开不再请求）
    let beforeSeq = minSeq
    for (let i = 0; i < 30 && beforeSeq > 1; i++) {
      const page = await messageApi.getHistory(convIdStr, { beforeSeq, limit: 100 })
      if (!page || !page.length) break
      await localdb.messages.upsert(page.map((m) => toDbMessage(m, convIdStr)))
      changed = true
      const mn = page.reduce((a, m) => {
        const s = Number(m.seq) || 0
        return s > 0 ? Math.min(a, s) : a
      }, beforeSeq)
      if (page.length < 100 || mn >= beforeSeq || mn <= 1) break
      beforeSeq = mn
    }

    if (maxSeq > 0) localdb.conversations.updateSyncSeq(convIdStr, maxSeq)
    // 3) 回填有新数据时重置到第一页重搜（保留用户当前关键字/筛选）
    if (changed) await runSearch()
  } catch {
    // 网络不可达：保持本地库搜索结果，不打断使用
  }
}

onMounted(() => {
  runSearch()
  backfillConvHistory()
})

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
        <div class="title-center">{{ scopedConv ? scopedConv.name : 'WorkChat' }} · 查找聊天记录</div>
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
            :placeholder="scopedConv ? `在「${scopedConv.name}」中搜索…` : '搜索当前会话聊天记录…'"
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

      <!-- 结果区：滚近底部自动加载下一页 -->
      <section class="result-section" ref="resultListEl" @scroll="onResultScroll">
        <!-- 空态：无关键字时当前会话无记录；有关键字时无匹配 -->
        <div v-if="totalHits === 0" class="empty-state">
          <div class="empty-illu" aria-hidden="true">
            <svg viewBox="0 0 64 64" width="56" height="56">
              <circle cx="26" cy="26" r="14" fill="none" stroke="currentColor" stroke-width="2.5" />
              <line x1="37" y1="37" x2="50" y2="50" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" />
            </svg>
          </div>
          <p class="empty-title">{{ hasQuery ? '没有找到匹配的消息' : '暂无聊天记录' }}</p>
          <p class="empty-sub">{{ hasQuery ? '试试别的关键词，或切换消息类型筛选' : '当前会话还没有已保存的消息' }}</p>
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
                  <span class="hit-name">{{ hit.senderLabel }}</span>
                  <span class="hit-dot">·</span>
                  <span class="hit-type">{{ hit.chatType }}</span>
                </div>
                <div class="hit-summary" :class="{ 'has-thumb': hit.msgType === 2 && (hit.thumbUrl || hit.imageUrl) }">
                  <!-- 图片消息：缩略图 + [图片]，不展示原始链接 -->
                  <template v-if="hit.msgType === 2">
                    <img
                      v-if="hit.thumbUrl || hit.imageUrl"
                      class="hit-thumb"
                      :src="hit.thumbUrl || hit.imageUrl"
                      alt="图片缩略图"
                      loading="lazy"
                    />
                    <span>{{ hit.summary }}</span>
                  </template>
                  <template v-else>
                    <template v-for="(seg, si) in splitSegments(hit.summary, hit.kw)" :key="si">
                      <mark v-if="seg.hit" class="hit-mark">{{ seg.text }}</mark>
                      <span v-else-if="seg.link" class="hit-link">{{ seg.text }}</span>
                      <span v-else>{{ seg.text }}</span>
                    </template>
                  </template>
                </div>
              </div>
              <span class="hit-time">{{ hit.time }}</span>
            </button>
          </div>

          <!-- 滚动分页状态：加载中 / 已加载全部 -->
          <div v-if="loadingMore || hasMore" class="load-more">
            <span v-if="loadingMore">正在加载更多记录…</span>
            <span v-else>向下滚动加载更多</span>
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

/* 摘要中的链接：微信风格蓝色文本 */
.hit-link {
  color: var(--im-primary, #576b95);
}

/* 图片命中项：缩略图 + [图片] 占位 */
.hit-summary.has-thumb {
  display: flex;
  align-items: center;
  gap: 8px;
}

.hit-thumb {
  width: 44px;
  height: 44px;
  border-radius: 6px;
  object-fit: cover;
  flex-shrink: 0;
  background: var(--im-surface);
  border: 1px solid var(--im-border);
}

/* 滚动分页底部状态 */
.load-more {
  padding: 6px 4px 2px;
  text-align: center;
  font-size: 0.857rem;
  color: var(--im-text-muted);
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