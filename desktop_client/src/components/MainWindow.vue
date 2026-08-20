<script setup>
import { ref, reactive, computed, watch, nextTick, onMounted, onBeforeUnmount, onActivated } from 'vue'
import SearchChatHistoryModal from './SearchChatHistoryModal.vue'
import CreateGroupModal from './CreateGroupModal.vue'
import CallWindow from './CallWindow.vue'
import MessageBubble from './MessageBubble.vue'
import EmojiPanel from './EmojiPanel.vue'
import SingleProfilePanel from './SingleProfilePanel.vue'
import GroupProfilePanel from './GroupProfilePanel.vue'
import InviteMembersModal from './InviteMembersModal.vue'
import LeaveGroupConfirm from './LeaveGroupConfirm.vue'
import GroupSettingsModal from './GroupSettingsModal.vue'
import { messageApi } from '../api/message'
import { wsClient } from '../api/ws'
import { friendApi, groupApi } from '../api/social'
import { localdb } from '../api/localdb'
import { notifyNewMessage } from '../api/notify'
import {
  MSG_TYPE, SEND_TIMEOUT_MS, formatUnread, formatFileSize, formatMsgTime, formatConvTime,
  formatTimeDivider, groupMessagesByDay,
} from '../utils/format'
import {
  messagePreview, convLastPreview, isHiddenLeaveMsg, toDbMessage, toConvItem,
  createMessageMapper, isAudioMsg, isVideoMsg,
} from '../utils/message'
import { CONTACT_COLORS } from '../utils/palette'
import { useToast } from '../composables/useToast'
import { useVoiceCall } from '../composables/useVoiceCall'
import { useVoicePlayer } from '../composables/useVoicePlayer'
import { useGroupPanel } from '../composables/useGroupPanel'
import { useMediaPreview } from '../composables/useMediaPreview'
import { useStagedFiles } from '../composables/useStagedFiles'
import { useMessageMenu } from '../composables/useMessageMenu'

// 父组件传入的弹窗开关控制 + 跳转回调
const props = defineProps({
  showSearchHistory: {
    type: Boolean,
    default: false,
  },
  // 待打开会话 id：通讯录"发消息"后由父组件传入，据此切换到对应会话
  openConversation: {
    type: String,
    default: null,
  },
})
const emit = defineEmits(['update:showSearchHistory', 'close-search', 'update:chat-badge', 'request-add-friend', 'group-created'])

const message = ref('')
const muteDnd = ref(true)

// ---- "查找聊天记录"弹窗 ----
// 通过 props.showSearchHistory 控制；点击结果项后通知父组件跳转；仅搜当前会话
function openSearchHistory() {
  emit('update:showSearchHistory', true)
}
function closeSearchHistory() {
  emit('update:showSearchHistory', false)
}

// 右侧联系人信息面板：默认隐藏，点击聊天头部"更多"按钮可切换显隐
// 支持通过 URL hash `#profile=1` 在启动时强制展开（用于截图验证）
const showProfile = ref(
  typeof window !== 'undefined' &&
    /[#&]profile=1/.test(window.location.hash)
)
function toggleProfile() {
  showProfile.value = !showProfile.value
}

// 资料面板"发消息"：关闭当前资料面板并聚焦输入框，直接进入输入状态
function sendMessageAction() {
  showProfile.value = false
  nextTick(() => {
    inputFieldEl.value?.focus()
  })
}

// ---- 会话数据 ----
const conversations = ref([])

// 总未读消息数（用于左侧导航栏聊天按钮的气泡）
const totalUnread = computed(() =>
  conversations.value.reduce((sum, c) => sum + (c.unread || 0), 0)
)
watch(totalUnread, (n) => {
  emit('update:chat-badge', n)
  // 任务栏图标角标：未读数同步给主进程（Windows 覆盖图标 / macOS Dock 徽章，0 清除）
  try {
    window.electronAPI?.badge?.set(n)
  } catch {}
}, { immediate: true })

// ---- 状态 ----
const activeId = ref(conversations.value[0]?.id || '') // 当前选中的会话 id
const chatLoading = ref(false) // 历史消息加载指示器
const transitionState = ref('') // 消息区过渡：'' | 'leaving' | 'entering'
const listEl = ref(null) // 会话列表容器，供键盘导航滚动

// 当前会话（由 activeId 派生）；无选中会话时返回空对象兜底，避免模板访问字段报错
const emptyContact = { id: '', name: '', avatar: '?', color: '#64748b', online: false, messages: [] }
const currentContact = computed(
  () => conversations.value.find((c) => c.id === activeId.value) ?? emptyContact
)
// 是否已选中会话（未选中时展示"选择会话开始聊天"占位，不默认选中第一个）
const hasActiveContact = computed(() => !!currentContact.value.id)

// 是否为群聊会话（群聊中对方消息需展示发送者昵称）
const isGroupChat = computed(() => currentContact.value.type === 'group')

// 当前会话的消息列表
const currentMessages = computed(() => currentContact.value.messages ?? [])

// 微信风格：消息按天分组展示（每天独立 + 组内时间分隔 / 连续合并）
const currentMsgGroups = computed(() => groupMessagesByDay(currentMessages.value))

// 是否有历史消息
const hasMessages = computed(() => currentMessages.value.length > 0)

// 资料面板 / 聊天头部展示的名称、状态
const contactMeta = computed(() => ({
  id: currentContact.value.id,
  name: currentContact.value.name,
  avatar: currentContact.value.avatar,
  color: currentContact.value.color,
  online: currentContact.value.online,
}))

// 是否为群聊（根据会话 type 判断，资料面板布局切换用）
const isGroup = computed(() => currentContact.value.type === 'group')

// ---- 真实后端接入状态 ----
// 记录服务端会话的 conv_id → 本地面板 id 映射
const realConvMap = ref({})
// 对端已读游标缓存：conv_id → 对方已读到的最新 seq
// 用于会话切换/历史重新加载后恢复"我发出的消息是否已被对方读"。
const readCursorMap = ref({})
// 是否已接入真实后端
const useRealBackend = ref(false)

// 不落盘（敏感）会话集合：从本地库读取并在开关切换时维护；
// 主进程仓储层已强制拦截落库，此处用于 UI 状态与媒体缓存/冗余调用拦截。
const noPersistSet = ref(new Set())

// 当前会话是否已开启不落盘
const isNoPersist = computed(() => {
  const c = currentContact.value
  if (!c) return false
  const convId = realConvMap.value[c.id] || c.convId
  return convId ? noPersistSet.value.has(String(convId)) : false
})

// 切换不落盘开关：开启时确认并清除该会话已落盘记录
async function toggleNoPersist() {
  const c = currentContact.value
  if (!c) return
  const convId = realConvMap.value[c.id] || c.convId
  if (!convId) return
  const next = !isNoPersist.value
  if (next && !confirm('开启后该会话的消息不再保存到本地磁盘，且将删除该会话已保存的本地聊天记录。是否开启？')) return
  await localdb.conversations.setNoPersist(String(convId), next)
  if (next) noPersistSet.value.add(String(convId))
  else noPersistSet.value.delete(String(convId))
}

// ---- 输入框（先声明：消息操作菜单等 composable 需要引用）----
// 微信风格：输入框自适应高度（最多约 6 行）
const inputFieldEl = ref(null)
const inputBoxEl = ref(null)
const MAX_INPUT_ROWS = 6

// 输入框高度自由拖动：上限总高度，下限保持两行底高
const inputBoxHeight = ref(0) // 用户拖动后的固定高度（0 = 未拖动，走内容自适应）
const INPUT_BOX_PADDING_Y = 24 // .input-box 上下内边距之和（12px * 2）
const MIN_INPUT_BOX_HEIGHT = 68 // 两行 + 内边距
const MAX_INPUT_BOX_HEIGHT = 280 // 拖动总高度上限

function autoResizeInput() {
  const el = inputFieldEl.value
  if (!el) return
  // 已拖动过：固定为用户设定的高度，内容超出时内部滚动
  if (inputBoxHeight.value) {
    el.style.height = Math.max(22, inputBoxHeight.value - INPUT_BOX_PADDING_Y) + 'px'
    el.style.overflowY = 'auto'
    return
  }
  el.style.height = 'auto'
  // 通过行高推算最大高度：约 6 行文本
  const lineHeight = parseFloat(getComputedStyle(el).lineHeight) || 22
  const maxHeight = lineHeight * MAX_INPUT_ROWS
  el.style.height = Math.min(el.scrollHeight, maxHeight) + 'px'
  el.style.overflowY = el.scrollHeight > maxHeight ? 'auto' : 'hidden'
}

// 按住手柄上下拖动调整输入框高度（向上拖变高），实时夹在上下限内
function startInputResize(e) {
  e.preventDefault()
  const box = inputBoxEl.value
  if (!box) return
  const startY = e.clientY
  const startH = box.offsetHeight
  const onMove = (ev) => {
    const h = Math.min(MAX_INPUT_BOX_HEIGHT, Math.max(MIN_INPUT_BOX_HEIGHT, startH + (startY - ev.clientY)))
    inputBoxHeight.value = h
    autoResizeInput()
  }
  const onUp = () => {
    document.removeEventListener('mousemove', onMove)
    document.removeEventListener('mouseup', onUp)
    document.body.style.userSelect = ''
    document.body.style.cursor = ''
  }
  document.body.style.userSelect = 'none'
  document.body.style.cursor = 'row-resize'
  document.addEventListener('mousemove', onMove)
  document.addEventListener('mouseup', onUp)
}

// ---- toast 与功能 composable 接线 ----
const { toastState, showToast } = useToast()
const voicePlayer = useVoicePlayer({ showToast })
// reactive 包装：模板中直接以 gp.xxx / staged.xxx 访问（自动解包内部 ref）
const call = reactive(useVoiceCall({
  currentContact, hasActiveContact, isGroupChat, conversations, activeId,
  realConvMap, noPersistSet, showToast, scrollToBottom, reorderConversations,
}))
const gp = reactive(useGroupPanel({
  currentContact, conversations, activeId, showProfile, realConvMap, noPersistSet, showToast,
}))
const media = reactive(useMediaPreview({
  currentContact, realConvMap, noPersistSet, showToast, scrollToBottom,
  stopVoice: voicePlayer.stopVoice,
}))
const staged = reactive(useStagedFiles({
  activeId, hasActiveContact, currentContact, realConvMap, showToast, sendMessage,
}))
const msgMenuApi = reactive(useMessageMenu({
  realConvMap, activeId, message, inputFieldEl, autoResizeInput, refreshConvPreview,
}))

// ---- 好友备注 ----
// 好友备注信息（target_uid → { remark, nickname }）：buildContactMap 时同步填充，
// 供资料面板备注展示/编辑；群聊无此概念
const friendRemarkMap = ref({})
// 当前单聊对方的备注信息（非好友/群聊返回 null）
const currentFriendInfo = computed(() => {
  const c = currentContact.value
  if (!c || c.type === 'group' || !c.targetId) return null
  return friendRemarkMap.value[String(c.targetId)] || null
})
const currentRemark = computed(() => (currentFriendInfo.value && currentFriendInfo.value.remark) || '')
const remarkSaving = ref(false)

// 保存好友备注：成功后同步更新缓存与会话项展示名（备注优先，清空则回落昵称）；
// 返回是否成功（子面板据此决定是否退出编辑态）
async function saveRemark(draft) {
  const targetUid = currentContact.value.targetId
  if (!targetUid || remarkSaving.value) return false
  const remark = (draft || '').trim()
  remarkSaving.value = true
  try {
    await friendApi.setRemark(targetUid, remark)
    const info = currentFriendInfo.value
    const nickname = (info && info.nickname) || ''
    friendRemarkMap.value[String(targetUid)] = { remark, nickname }
    const displayName = remark || nickname
    if (displayName) {
      conversations.value.forEach((c) => {
        if (c.type !== 'group' && String(c.targetId) === String(targetUid)) c.name = displayName
        // 群聊已渲染消息：发送者即该好友时同步刷新展示名（备注优先，清空回落昵称）
        if (c.type === 'group' && c.messages && c.messages.length) {
          c.messages.forEach((m) => {
            if (m.senderUid && String(m.senderUid) === String(targetUid)) m.senderName = displayName
          })
        }
      })
    }
    return true
  } catch (e) {
    alert(e.message || '备注保存失败')
    return false
  } finally {
    remarkSaving.value = false
  }
}

// 群聊发送者展示名：发送者是我的好友时优先展示我给他的备注，无备注回落服务端昵称
function friendDisplayName(uid, fallback) {
  const info = uid ? friendRemarkMap.value[String(uid)] : null
  return (info && info.remark) || fallback || ''
}

function meUid() {
  try {
    const me = JSON.parse(localStorage.getItem('workchat:me') || '{}')
    return me.uid || 0
  } catch {
    return 0
  }
}

// 消息映射器：服务端/本地库消息 → 面板消息结构（头像/颜色按服务端 conv_id 匹配会话）
const { mapServerMessage, mapLocalMessage } = createMessageMapper({
  meUid,
  friendDisplayName,
  convPeer: (convId) => {
    // 统一字符串比较：conv_id 来源多样（WS JSON/接口字符串/本地库），避免类型不一致导致匹配失败显示 '?'
    const c = conversations.value.find((x) => String(realConvMap.value[x.id]) === String(convId))
    return c ? { avatar: c.avatar, color: c.color } : { avatar: '?', color: '#64748b' }
  },
})

// ---- 会话列表搜索 ----
const convSearch = ref('')
const convSearchEl = ref(null)

// 按关键字过滤会话：匹配名称、最近消息
const filteredConversations = computed(() => {
  const kw = convSearch.value.trim().toLowerCase()
  if (!kw) return conversations.value
  return conversations.value.filter(
    (c) =>
      c.name.toLowerCase().includes(kw) ||
      (c.lastMessage ?? '').toLowerCase().includes(kw)
  )
})

// 搜索快捷键：Ctrl/Cmd + K 聚焦搜索框；Esc 清空
function focusConvSearch() {
  convSearchEl.value?.focus()
}

function onConvSearchKeydown(e) {
  if (e.key === 'Escape') {
    convSearch.value = ''
    convSearchEl.value?.blur()
  } else if (e.key === 'Enter' && filteredConversations.value.length > 0) {
    // 回车直接选中第一条匹配项
    e.preventDefault()
    selectConversation(filteredConversations.value[0].id)
    convSearch.value = ''
    convSearchEl.value?.blur()
  }
}

// 全局快捷键：Ctrl/Cmd + K 聚焦搜索
function onGlobalKeydown(e) {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault()
    focusConvSearch()
  }
}
onMounted(() => window.addEventListener('keydown', onGlobalKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onGlobalKeydown))

// ---- KeepAlive 滚动位置保持 ----
// 当通过左侧菜单切换到其它页面再返回时，KeepAlive 会保留组件实例和 DOM，
// 但被 display:none 隐藏后再显示会丢失滚动位置，因此这里手动保存/恢复。
// 通过监听滚动事件持续记录最新位置，避免依赖 deactivated 时的时序问题。
let savedMsgScroll = 0 // 保存消息区的滚动位置
let scrollHandler = null
let pendingReadAck = false // 当前激活会话：收到新消息但用户未滑到底部，待滚到底部后补发已读回执

function attachScrollWatcher() {
  if (scrollHandler) return
  const el = document.querySelector('.messages')
  if (!el) return
  scrollHandler = () => {
    savedMsgScroll = el.scrollTop
    // 滚动到顶部附近时触发向上加载更早消息
    if (el.scrollTop <= 40) loadMoreHistory()
    // 用户滚动到底部：若之前有"未滑到底部收到的新消息"，此刻补发已读回执（仅单聊）
    if (pendingReadAck && isNearBottom()) {
      pendingReadAck = false
      const contact = currentContact.value
      if (contact) sendReadReceipt(contact)
    }
    // 更新"回到底部"按钮显示状态
    if (showScrollToBottom.value && isNearBottom()) {
      showScrollToBottom.value = false
    }
  }
  el.addEventListener('scroll', scrollHandler, { passive: true })
}

function restoreScroll() {
  const el = document.querySelector('.messages')
  if (!el) return
  if (savedMsgScroll > 0) {
    el.scrollTop = savedMsgScroll
  }
}

// 向上分页加载更早消息（滚动到顶部触发）
let loadingMore = false // 防抖：防止滚动触发重复加载
async function loadMoreHistory() {
  const contact = currentContact.value
  if (!contact || !contact.convId) return
  if (loadingMore) return
  if (!contact._hasMore) return // 没有更多历史了
  loadingMore = true
  try {
    await loadConversationMessages(contact, { prepend: true })
  } finally {
    loadingMore = false
  }
}

onMounted(attachScrollWatcher)

onActivated(() => {
  // 恢复滚动位置，需在 DOM 布局稳定后执行
  requestAnimationFrame(() => restoreScroll())
})

onBeforeUnmount(() => {
  if (scrollHandler) {
    const el = document.querySelector('.messages')
    if (el) el.removeEventListener('scroll', scrollHandler)
    scrollHandler = null
  }
  voicePlayer.stopVoice()
})

// ---- 查找聊天记录：跳转 + 高亮 ----
// 点击搜索结果时：切换会话、滚动到目标消息、临时高亮
const highlightMessageId = ref(null)
let highlightTimer = null

// 目标消息翻页回补后仍未找到时的兜底：从服务端拉取以目标 seq 为中心的窗口
// （目标及之前 40 条 + 之后尽量拉满 100 条）整体替换当前列表，落库并重置翻页游标。
async function loadTargetWindow(contact, seqNum) {
  if (!contact || !contact.convId) return false
  const convIdStr = String(contact.convId)
  try {
    const before = (await messageApi.getHistory(contact.convId, { beforeSeq: seqNum + 1, limit: 40 })) || []
    let after = []
    try {
      // 目标之后的新消息尽量拉满（上限 100）：多数会话可直接覆盖到最新，避免列表截断
      after = (await messageApi.getHistory(contact.convId, { afterSeq: seqNum, limit: 100 })) || []
    } catch {}
    const msgs = before.concat(after).filter((m) => !isHiddenLeaveMsg(m, meUid()))
    if (!msgs.length) return false
    // 网络数据落本地库（去重），后续翻页/搜索直接命中
    if (!noPersistSet.value.has(convIdStr)) {
      localdb.messages.upsert(msgs.map((m) => toDbMessage(m, convIdStr)))
    }
    const known = new Set()
    contact.messages = msgs
      .filter((m) => (known.has(String(m.id)) ? false : (known.add(String(m.id)), true)))
      .map((m) => mapServerMessage(m, contact.convId))
    contact.oldestSeq = contact.messages.find((m) => m.seq)?.seq || 0
    contact._hasMore = Number(contact.oldestSeq) > 1 // 窗口之前可能还有更早历史
    realConvMap.value[contact.id] = contact.convId
    hydrateMediaCache(contact.messages, contact.convId)
    return true
  } catch {
    return false
  }
}

async function jumpToMessage({ conversationId, messageId, seq }) {
  const contact = conversations.value.find((c) => c.id === conversationId)
  if (!contact) return
  const seqNum = Number(seq) || 0
  const hasTarget = () =>
    (contact.messages || []).some((m) => String(m.id) === String(messageId))
  // 1) 切会话：仅在不同会话时切换（同会话跳转会重载消息列表并把滚动重置到底部）；
  // switchConversation 的 Promise 在历史加载完成后才 resolve，消除过早滚动竞态
  if (activeId.value !== conversationId) {
    await switchConversation(conversationId)
  }
  // 2) 目标不在已加载范围（搜索命中的可能是很早的历史）：逐页向上回补
  // （本地库优先，无网络请求；打开搜索弹窗时已回填过全量历史，通常本地即可命中）
  if (seqNum > 0 && !hasTarget()) {
    for (let i = 0; i < 100; i++) {
      if (hasTarget()) break
      if (!contact._hasMore || !(Number(contact.oldestSeq) > 0)) break
      if (seqNum >= Number(contact.oldestSeq)) break
      await loadConversationMessages(contact, { prepend: true })
    }
  }
  // 3) 仍未找到（本地缺失/分页耗尽）：按目标 seq 拉取居中窗口替换列表
  if (!hasTarget() && seqNum > 0) {
    await loadTargetWindow(contact, seqNum)
  }
  await nextTick()
  // 4) 即时居中定位（手动计算 scrollTop，比 scrollIntoView 平滑滚动稳定，
  // 不会被图片异步加载撑高内容带偏）+ 临时高亮；媒体加载后再校正一次
  const centerRow = () => {
    const scroller = document.querySelector('.messages')
    const row = document.querySelector(`.message-row[data-msg-id="${messageId}"]`)
    if (!scroller || !row) return false
    const top = row.getBoundingClientRect().top - scroller.getBoundingClientRect().top + scroller.scrollTop
    scroller.scrollTop = top - Math.max(0, (scroller.clientHeight - row.offsetHeight) / 2)
    return true
  }
  setTimeout(() => {
    if (!centerRow()) return
    highlightMessageId.value = messageId
    if (highlightTimer) clearTimeout(highlightTimer)
    highlightTimer = setTimeout(() => {
      highlightMessageId.value = null
    }, 2200)
    // 图片/缓存换源可能改变行高：延迟再校正一次，保证目标始终居中
    setTimeout(centerRow, 300)
  }, 80)
}

// ---- 切换会话 ----
function selectConversation(id) {
  if (id === activeId.value) return // 已选中，无需切换
  switchConversation(id)
}

// 通讯录"发消息"/"进入"跳转：父组件传入 openConversation 时，切换到对应会话。
// 通讯录传的是对方 uid（单聊）/ 群 g_uid（群聊），而会话列表项 id 为 `conv-${conv_id}`，
// 需先按 targetId 映射；尚无会话（从未聊过）时创建占位会话并选中。
watch(
  () => props.openConversation,
  (id) => {
    if (!id) return
    const key = String(id)
    const target = conversations.value.find((c) => String(c.targetId) === key)
    if (target) {
      if (target.id === activeId.value) return // 已选中同一会话，无需切换
      switchConversation(target.id)
      return
    }
    openPlaceholderConversation(key)
  }
)

// 通讯录跳转目标尚无会话（从未聊过）：从联系人缓存构建占位会话并选中。
// 发消息后后端 GetOrCreateConversation 自动建真实会话，conversation.created 事件
// 触发会话列表重载，占位会话在 applyConvList 中被无缝替换为真实会话。
async function openPlaceholderConversation(targetKey) {
  const contactMap = await buildContactMap()
  const info = contactMap.get(targetKey)
  if (!info) return // 非好友/群：不创建
  if (conversations.value.some((c) => String(c.targetId) === targetKey)) return
  const placeholder = {
    id: `new-${targetKey}`,
    name: info.name,
    avatar: info.avatar,
    color: info.color,
    online: false,
    type: info.type || null,
    lastMessage: '',
    time: '',
    lastMsgTime: 0,
    unread: 0,
    targetId: Number(targetKey) || targetKey,
    convId: 0, // 尚无真实会话：发送消息时后端按 target_id 自动创建
    peerReadSeq: 0,
    messages: [],
    oldestSeq: 0,
    _hasMore: false,
  }
  conversations.value.unshift(placeholder)
  switchConversation(placeholder.id)
}

// 切换到指定会话，带淡出->加载->淡入过渡。
// 返回的 Promise 在历史加载完成（开始淡入）后 resolve：供 jumpToMessage 等
// 需要精确等待加载完成的调用方 await；历史 bug 是加载在 setTimeout 内执行，
// await 不到导致跳转时过早滚动、列表重载后位置失效。
async function switchConversation(id) {
  voicePlayer.stopVoice() // 切会话停止当前语音播放
  const target = conversations.value.find((c) => c.id === id)
  if (!target) return

  // 关闭可能打开的消息操作菜单
  msgMenuApi.closeMsgMenu()

  // 1) 淡出当前消息区
  transitionState.value = 'leaving'
  await nextTick()

  await new Promise((resolve) => {
    // 短暂延迟，让离开过渡可见
    setTimeout(async () => {
      // 2) 更新选中会话并清零未读（同步清零本地库，保持未读一致）
      activeId.value = id
      target.unread = 0
      if (target.convId) localdb.conversations.setUnread(String(target.convId), 0)

      // 3) 进入“加载中”状态
      chatLoading.value = true
      await nextTick()

      // 4) 加载该会话历史：本地库秒开 + 条件同步（本地已追平服务端水位则免网络请求）
      await loadConversationMessages(target)

      // 5) 加载完成，淡入新消息
      chatLoading.value = false
      transitionState.value = 'entering'
      await nextTick()
      // 重置待确认已读标志（新会话）
      pendingReadAck = false
      // 首次加载默认滚动到底部（展示最新消息）
      scrollToBottom()
      // 打开会话即视为已读该会话全部消息：直接向对方发送已读回执（仅单聊）。
      // 不能依赖 isNearBottom() 判定——scrollToBottom 内部用 nextTick 异步设置滚动位置，
      // 此处同步调用 isNearBottom() 会读到切换前的残留 scrollTop，导致在消息较多时误判
      // 为“不在底部”而漏发已读回执（对端因此收不到已读提示）。切换会话本身即查看最新消息。
      sendReadReceipt(target)
      // 群聊：从后端加载真实群成员
      if (target.type === 'group') gp.loadLiveGroupMembers()
      setTimeout(() => {
        transitionState.value = ''
      }, 220)
      // 通知等待方：历史已加载完成，可以安全定位/滚动
      resolve()
    }, 160)
  })
}

// 加载指定会话的历史消息并填充到 target.messages（离线优先：本地库先渲染，网络拉取后合并落库）
// options.prepend=true 表示向上翻页加载更早消息（插入顶部，保持滚动位置）
async function loadConversationMessages(target, { prepend = false } = {}) {
  if (!target || !target.convId) return
  if (!Array.isArray(target.messages)) target.messages = []
  const convIdStr = String(target.convId)

  // 首页本地优先：先读本地库立即渲染（离线可用/秒开）
  let localMaxSeq = 0
  if (prepend) {
    // 向上翻页本地优先：网络拉取与 WS 推送均实时落库，本地已有更早消息时
    // 直接读本地库分页，不请求后端；本地耗尽才落入下方网络分支补齐（拉取后同样落库）。
    // 不落盘会话本地库恒返回空，自动走网络；浏览器调试环境 localdb 降级为空同理。
    const localRows = (
      target.oldestSeq > 0
        ? await localdb.messages.list(convIdStr, { beforeSeq: target.oldestSeq, limit: 10 })
        : []
    ).filter((r) => !isHiddenLeaveMsg(r, meUid()))
    if (localRows.length) {
      const scrollEl = document.querySelector('.messages')
      const prevHeight = scrollEl ? scrollEl.scrollHeight : 0
      const older = localRows.map((r) => mapLocalMessage(r, target.convId))
      hydrateMediaCache(older, target.convId)
      target.messages = older.concat(target.messages)
      await nextTick()
      if (scrollEl) {
        const delta = scrollEl.scrollHeight - prevHeight
        if (delta > 0) scrollEl.scrollTop += delta
      }
      target.oldestSeq = target.messages.find((m) => m.seq)?.seq || 0
      target._hasMore = localRows.length >= 10
      return
    }
    // 最旧消息已是 seq 1：必然没有更早历史，停止翻页（避免一次无效网络请求）
    if (target.oldestSeq > 0 && target.oldestSeq <= 1) {
      target._hasMore = false
      return
    }
    // 本地无更早消息（尚未同步过）：落入下方网络分支补齐
  }
  if (!prepend) {
    const localRows = (await localdb.messages.list(convIdStr, { limit: 10 })).filter((r) => !isHiddenLeaveMsg(r, meUid()))
    if (localRows.length) {
      // 传服务端 conv_id（而非面板 id）：mapServerMessage 据此匹配会话头像/颜色，传错会显示 '?'
      target.messages = localRows.map((r) => mapLocalMessage(r, target.convId))
      const last = target.messages[target.messages.length - 1]
      target.lastMessage = convLastPreview(last)
      target.time = formatConvTime(last.createdAt || target.lastMsgTime)
      target.lastMsgTime = last.createdAt || target.lastMsgTime || 0
      target.oldestSeq = target.messages.find((m) => m.seq)?.seq || 0
      target._hasMore = localRows.length >= 10
      realConvMap.value[target.id] = target.convId
      restoreReadState(target)
      hydrateMediaCache(target.messages, target.convId)
      // 本地已同步到的最大 seq：后续只增量拉取更新的消息
      localMaxSeq = localRows.reduce((mx, r) => Math.max(mx, Number(r.seq) || 0), 0)
    }
    // 条件同步（服务端减压，见 docs/桌面端消息同步减压优化方案.md 阶段一）：
    // 本地有数据且已追平服务端水位（last_synced_seq）时，不发任何网络请求。
    // 方向安全：水位落后（如离线漏推）时自动落入下方增量拉取补齐；
    // 不落盘会话本地库恒返回空（localRows 为空），照旧走网络。
    if (localRows.length > 0 && target.syncSeq > 0 && localMaxSeq >= target.syncSeq) {
      return
    }
  }

  try {
    // 增量模式：本地已有数据时只拉 seq > localMaxSeq 的新消息；否则拉最新 10 条
    const incremental = !prepend && localMaxSeq > 0
    let msgs
    if (incremental) {
      // 循环增量拉取：单批上限 100 条，超出则翻页拉完，防离线久了增量被截断丢消息（修复审计 P0）
      msgs = []
      let afterSeq = localMaxSeq
      for (let i = 0; i < 10; i++) { // 上限 10 页 = 1000 条，避免异常时死循环
        const page = await messageApi.getHistory(target.convId, { afterSeq, limit: 100 })
        if (!page || !page.length) break
        msgs = msgs.concat(page)
        afterSeq = page.reduce((mx, m) => Math.max(mx, Number(m.seq) || 0), afterSeq)
        if (page.length < 100) break
      }
    } else {
      const params = { limit: 10 }
      if (prepend && target.oldestSeq > 0) {
        params.beforeSeq = target.oldestSeq
      }
      msgs = await messageApi.getHistory(target.convId, params)
    }
    // 退群系统消息仅群主可见：非群主直接丢弃（不落库不渲染）
    msgs = (msgs || []).filter((m) => !isHiddenLeaveMsg(m, meUid()))
    // 网络数据落本地库（按 conv_id + server_id 去重）并推进同步游标；不落盘会话跳过
    if (msgs && msgs.length && !noPersistSet.value.has(convIdStr)) {
      localdb.messages.upsert(msgs.map((m) => toDbMessage(m, convIdStr)))
      const maxSeq = msgs.reduce((mx, m) => Math.max(mx, Number(m.seq) || 0), 0)
      if (maxSeq > 0) localdb.conversations.updateSyncSeq(convIdStr, maxSeq)
    }
    // 水位同步到内存会话项：事件插入的项（syncSeq=0）/旧列表项（无水位字段）
    // 拉取过一次后即具备条件同步资格，下次打开不再请求（缺口自愈）
    if (!prepend) {
      const fetchMaxSeq = (msgs || []).reduce((mx, m) => Math.max(mx, Number(m.seq) || 0), 0)
      if (fetchMaxSeq > 0) target.syncSeq = Math.max(Number(target.syncSeq) || 0, fetchMaxSeq)
    }
    const mapped = (msgs || []).map((m) => mapServerMessage(m, target.convId))
    if (prepend) {
      // 向前翻页：新消息插入列表顶部（mapped 为升序），并保持滚动位置
      if (mapped.length) {
        const scrollEl = document.querySelector('.messages')
        const prevHeight = scrollEl ? scrollEl.scrollHeight : 0
        target.messages = mapped.concat(target.messages)
        // 等待 DOM 更新后计算新增高度，滚动位置后移以保持视口内容不跳动
        await nextTick()
        if (scrollEl) {
          const delta = scrollEl.scrollHeight - prevHeight
          if (delta > 0) scrollEl.scrollTop += delta
        }
      }
    } else if (incremental) {
      // 增量合并：本地已有 + 服务端新增（按 id 去重），不影响向上翻页状态
      if (mapped.length) {
        const nearBottom = isNearBottom()
        const known = new Set(target.messages.map((m) => m.id))
        const fresh = mapped.filter((m) => !known.has(m.id))
        if (fresh.length) {
          target.messages = target.messages.concat(fresh)
          hydrateMediaCache(fresh, target.convId)
          await nextTick()
          if (nearBottom) scrollToBottom()
        }
      }
    } else {
      // 首屏无本地数据：网络结果 + 本地未同步消息（pending/failed，网络不会返回）合并按时间排序展示，
      // 保证发送失败消息处于真实时间位置而非堆在末尾；
      // localId 仅在未同步行上保留（发送成功回填后置空），据此识别本地行
      const localOnly = target.messages.filter((m) => !!m.localId)
      target.messages = mapped
        .concat(localOnly)
        .sort((a, b) => (Number(a.createdAt) || 0) - (Number(b.createdAt) || 0))
    }
    if (target.messages.length) {
      const last = target.messages[target.messages.length - 1]
      // 会话摘要用与后端一致的预览：图片/文件消息不显示原始 URL；
      // 最后一条若是撤回消息，显示"你/对方撤回了一条消息"（与撤回逻辑一致，避免被撤回原文出现在列表）
      target.lastMessage = convLastPreview(last)
      target.time = formatConvTime(last.createdAt || target.lastMsgTime)
      // 优先用后端会话时间，消息时间为 0 时兜底保留
      target.lastMsgTime = last.createdAt || target.lastMsgTime || 0
      // 记录最旧消息 seq，用于下次向上翻页
      target.oldestSeq = target.messages.find((m) => m.seq)?.seq || 0
      if (!incremental) target._hasMore = mapped.length >= 10
    }
    realConvMap.value[target.id] = target.convId
    // 恢复该会话已读状态（按对端已读游标），避免切换会话后已读未读丢失
    restoreReadState(target)
    // 媒体消息解析本地缓存地址（不落盘会话跳过）
    hydrateMediaCache(target.messages, target.convId)
  } catch (e) {
    console.warn('[MainWindow] 加载历史消息失败:', e?.message || e)
    if (prepend) {
      // 网络失败：离线兜底读本地库更早的消息（同样过滤非群主不可见的退群消息）
      const rows = (
        await localdb.messages.list(convIdStr, { beforeSeq: target.oldestSeq, limit: 10 })
      ).filter((r) => !isHiddenLeaveMsg(r, meUid()))
      if (rows.length) {
        const scrollEl = document.querySelector('.messages')
        const prevHeight = scrollEl ? scrollEl.scrollHeight : 0
        target.messages = rows.map((r) => mapLocalMessage(r, target.convId)).concat(target.messages)
        await nextTick()
        if (scrollEl) {
          const delta = scrollEl.scrollHeight - prevHeight
          if (delta > 0) scrollEl.scrollTop += delta
        }
        target.oldestSeq = target.messages.find((m) => m.seq)?.seq || 0
        target._hasMore = rows.length >= 10
      } else {
        target._hasMore = false // 本地也没有更早历史，停止继续向上翻
      }
    }
  }
}

// ---- 键盘导航 ----
// 监听列表区方向键：↑/↓ 移动选中并聚焦，Home/End 跳到首尾
function handleListKeydown(e) {
  const index = conversations.value.findIndex((c) => c.id === activeId.value)
  let nextIndex = index
  if (e.key === 'ArrowDown') nextIndex = Math.min(index + 1, conversations.value.length - 1)
  else if (e.key === 'ArrowUp') nextIndex = Math.max(index - 1, 0)
  else if (e.key === 'Home') nextIndex = 0
  else if (e.key === 'End') nextIndex = conversations.value.length - 1
  else return

  e.preventDefault()
  const target = conversations.value[nextIndex]
  switchConversation(target.id)
  // 让新选中的项进入可视区
  nextTick(() => {
    const el = listEl.value?.querySelector(`[data-id="${target.id}"]`)
    el?.focus()
    el?.scrollIntoView({ block: 'nearest' })
  })
}

// 文件缓存：把图片/文件消息解析为本地缓存地址（wcfile://）。
// 命中直接换源；未命中后台下载入缓存，完成后回填 cacheUrl，后续展示不再重复下载。
// 不落盘（敏感）会话的媒体不进文件缓存。
function hydrateMediaCache(msgs, convId) {
  // 不落盘（敏感）会话的媒体不进文件缓存，但语音时长仍可直接探测远端地址
  const useCache = localdb.available() && !(convId && noPersistSet.value.has(String(convId)))
  for (const m of msgs || []) {
    const isMedia = m.msgType === MSG_TYPE.IMAGE || m.msgType === MSG_TYPE.FILE || m.msgType === MSG_TYPE.VOICE
    const url = m.extra && m.extra.url
    if (!isMedia || !url) continue
    if (!useCache) {
      voicePlayer.probeVoiceDuration(m)
      continue
    }
    if (m.extra.cacheUrl) {
      voicePlayer.probeVoiceDuration(m)
      continue
    }
    localdb.fileCache.resolve(url, m.extra.key || '', m.extra.name || '').then((r) => {
      if (r && r.hit && r.cacheUrl && m.extra) m.extra.cacheUrl = r.cacheUrl
      // 缓存就绪后再探测时长：避免对远端地址重复下载 metadata
      voicePlayer.probeVoiceDuration(m)
    })
  }
}

// ---- 发送消息 ----
// 发送文本消息：无暂存附件时仅发文本；有暂存时文本与附件一起发出
async function send() {
  const value = message.value.trim()
  const stagedList = staged.stagedFiles.slice()
  if (!value && !stagedList.length) return
  closeEmojiPanel()
  if (value) {
    await sendMessage(MSG_TYPE.TEXT, value, null)
    message.value = ''
    autoResizeInput()
  }
  if (stagedList.length) {
    // 先清空当前会话暂存区再逐个发送，防止异步期间重复提交
    delete staged.stagedFilesMap[activeId.value]
    for (const item of stagedList) staged.sendStagedMedia(item)
  }
}

// 统一发送消息（type: 文本/图片/文件）
// extra 为元数据对象（图片/文件 URL、文件名等），会 JSON 序列化存入消息 extra 字段
// options.prepared：自定义乐观消息字段（媒体暂存发送时注入本地预览/上传中状态）；
// options.deferSend：媒体场景——content/extra 需等上传完成后才有真实值，
// 此时只做乐观渲染与落库，返回带 __send 方法的 optimistic，由调用方上传成功后触发服务端发送
async function sendMessage(type, content, extra, options = {}) {
  const contact = currentContact.value
  if (!contact) return

  if (useRealBackend.value) {
    const msgId = Date.now() + Math.floor(Math.random() * 1000)
    const targetId = contact.targetId
    const convId = realConvMap.value[contact.id] || 0
    const convType = contact.type === 'group' ? 2 : 1
    const extraStr = extra ? JSON.stringify(extra) : ''

    // 乐观渲染
    contact.messages = contact.messages ?? []
    const optimistic = {
      id: `tmp-${msgId}`,
      type: 'out',
      msgType: type,
      avatar: '我',
      color: '#2563eb',
      text: content,
      extra: extra || {},
      status: 0,
      readAt: '发送中…',
      createdAt: Math.floor(Date.now() / 1000),
      time: '刚刚',
      server: true,
      isPending: true, // 未同步标记：发送成功回填后置 false
      ...(options.prepared || {}),
    }
    // 乐观消息先落本地库（离线发送队列 sync_state=pending）；不落盘会话不入队；
    // deferSend 阶段 content/extra 尚为占位，本地库记录推迟到真实发送时写入（同步携带真实 URL）
    let localId = null
    if (!options.deferSend && !noPersistSet.value.has(String(convId))) {
      localId = await localdb.messages.appendPending({
        conv_id: String(convId),
        sender_uid: String(meUid()),
        sender_name: '',
        type,
        content,
        extra: extraStr,
        created_at: optimistic.createdAt,
      })
    }
    optimistic.localId = localId || null
    contact.messages.push(optimistic)
    // 会话列表摘要：统一走 messagePreview（音频后缀的文件识别为 [语音]，与气泡/后端一致）
    contact.lastMessage = messagePreview(optimistic)
    contact.lastMsgTime = optimistic.createdAt
    contact.time = formatConvTime(optimistic.createdAt)
    scrollToBottom()
    reorderConversations()

    // 服务端发送：WS 优先 + HTTP 兜底 + 回显替换 + 超时守卫；deferSend 时由调用方稍后触发
    const __send = async ({ content: c2, extra: ex2 } = {}) => {
      const sendContent = c2 ?? content
      const sendExtraStr = ex2 ? JSON.stringify(ex2) : extraStr
      if (c2 !== undefined) optimistic.text = c2
      if (ex2) optimistic.extra = ex2
      // 补写本地库 pending 记录（deferSend 首阶段未写入）
      if (!optimistic.localId && !noPersistSet.value.has(String(convId))) {
        optimistic.localId = await localdb.messages.appendPending({
          conv_id: String(convId),
          sender_uid: String(meUid()),
          sender_name: '',
          type,
          content: sendContent,
          extra: sendExtraStr,
          created_at: optimistic.createdAt,
        })
      }
      // 发送超时守卫：30s 内未获确认（WS 回显或 HTTP 响应）展示为发送失败；
      // 本地库保持 pending：迟到的服务端回显仍可 claimPending 去重，WS 重连后离线队列会自动重发
      setTimeout(() => {
        if (!optimistic.isPending) return
        optimistic.readAt = '发送失败'
        optimistic.isPending = false
      }, SEND_TIMEOUT_MS)
      try {
        // WS 发送：返回是否真正发出（断线瞬间 sendFrame 会返回 false，不再静默丢帧）
        let sent = false
        if (wsClient.isConnected()) {
          sent = wsClient.sendMessage(msgId, convId, targetId, convType, type, sendContent, sendExtraStr)
        }
        if (!sent) {
          // WS 未连接或断线瞬间未发出：HTTP 兜底（修复审计 P0）
          const dto = await messageApi.send({
            conv_id: String(convId),
            target_id: Number(targetId),
            conv_type: convType,
            type: type,
            msg_id: String(msgId),
            content: sendContent,
            extra: sendExtraStr,
          })
          // 用服务端返回的真实消息替换乐观消息，并回填本地库同步状态
          // 传服务端 conv_id（dto.conv_id）：保持与历史/WS 路径一致，避免头像匹配失败
          const real = mapServerMessage(dto, dto.conv_id || convId)
          optimistic.id = String(dto.id)
          optimistic.time = real.time
          optimistic.readAt = ''
          optimistic.isPending = false
          if (optimistic.localId) {
            localdb.messages.setSyncState(optimistic.localId, 'synced', {
              serverId: String(dto.id),
              seq: Number(dto.seq) || 0,
            })
            optimistic.localId = null
          }
        }
      } catch (e) {
        optimistic.readAt = '发送失败'
        optimistic.isPending = false
        // 发送失败：本地队列置 failed（重连后可重试）
        if (optimistic.localId) localdb.messages.setSyncState(optimistic.localId, 'failed')
      }
    }
    // 媒体暂存场景：只先本地渲染，真实发送由调用方在上传完成后触发
    if (options.deferSend) {
      optimistic.__send = __send
      return optimistic
    }
    await __send()
    return
  }

  // mock 兜底
  contact.messages = contact.messages ?? []
  const mockMsg = {
    id: `${contact.id}-out-${Date.now()}`,
    type: 'out',
    msgType: type,
    avatar: '我',
    color: '#2563eb',
    text: content,
    extra: extra || {},
    readAt: '刚刚 已读',
    time: '刚刚',
  }
  contact.messages.push(mockMsg)
  contact.lastMessage = messagePreview(mockMsg)
}

// ---- 表情面板（状态在此，面板 UI 为 EmojiPanel 组件）----
const showEmojiPanel = ref(false)
const emojiPanelRef = ref(null)

function toggleEmojiPanel() {
  showEmojiPanel.value = !showEmojiPanel.value
}

function closeEmojiPanel() {
  showEmojiPanel.value = false
}

// 点击表情：插入到输入框（保持光标位置）
function insertEmoji(emoji) {
  const el = inputFieldEl.value
  if (el) {
    const start = el.selectionStart ?? message.value.length
    const end = el.selectionEnd ?? message.value.length
    message.value = message.value.slice(0, start) + emoji + message.value.slice(end)
    // 恢复光标并聚焦
    nextTick(() => {
      el.focus()
      const pos = start + emoji.length
      el.setSelectionRange(pos, pos)
    })
  } else {
    message.value += emoji
  }
  autoResizeInput()
}

// 点击面板外部关闭（emojiPanelRef 指向组件实例，取 $el 判定包含关系）
function onEmojiPanelOutside(e) {
  const el = emojiPanelRef.value?.$el
  if (showEmojiPanel.value && el && !el.contains(e.target)) {
    showEmojiPanel.value = false
  }
}
onMounted(() => document.addEventListener('mousedown', onEmojiPanelOutside))
onBeforeUnmount(() => document.removeEventListener('mousedown', onEmojiPanelOutside))

function scrollToBottom() {
  nextTick(() => {
    const el = document.querySelector('.messages')
    if (el) el.scrollTop = el.scrollHeight
    showScrollToBottom.value = false
  })
}

// ===== WS 连接状态（聊天头部展示）=====
const wsStatus = ref('disconnected') // 'connected' | 'connecting' | 'disconnected'

function onWsStatus(state) {
  wsStatus.value = state
  // 重连成功：重发离线发送队列（pending）
  if (state === 'connected') flushPendingQueue()
}

// 重发离线发送队列：取本地 pending 消息逐条经 WS 重发。
// 服务端回显到达后由 persistWsIncoming 按内容认领 pending 行并回填 synced，避免重复。
async function flushPendingQueue() {
  const pendings = await localdb.messages.listPending()
  if (!pendings.length) return
  for (const p of pendings) {
    const contact = conversations.value.find((c) => String(c.convId) === String(p.conv_id))
    if (!contact) continue // 会话不在当前列表（新账号/已删除），跳过留待下次
    const convType = contact.type === 'group' ? 2 : 1
    const newMsgId = Date.now() + Math.floor(Math.random() * 1000)
    wsClient.sendMessage(newMsgId, p.conv_id, contact.targetId, convType, p.type, p.content, p.extra || '')
  }
}

const wsStatusText = computed(() => {
  switch (wsStatus.value) {
    case 'connected':
      return '已连接'
    case 'connecting':
      return '连接中'
    default:
      return '已断开'
  }
})

const wsStatusClass = computed(() => {
  switch (wsStatus.value) {
    case 'connected':
      return 'is-connected'
    case 'connecting':
      return 'is-connecting'
    default:
      return 'is-disconnected'
  }
})

// ===== 回到底部按钮（接收消息时若不在底部则显示）=====
const showScrollToBottom = ref(false)

// 判断消息区是否在底部附近（阈值 60px）
function isNearBottom() {
  const el = document.querySelector('.messages')
  if (!el) return true
  return el.scrollHeight - el.scrollTop - el.clientHeight <= 60
}

// 点击"回到底部"按钮：平滑滚动到底部
function jumpToBottom() {
  const el = document.querySelector('.messages')
  if (!el) return
  el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
  showScrollToBottom.value = false
}

// 处理消息区滚动：更新"回到底部"按钮显示状态
// 注：滚动监听由 attachScrollWatcher 统一接管，此处保留供模板按需绑定
function onMessagesScroll() {
  savedMsgScroll = document.querySelector('.messages')?.scrollTop || 0
  if (showScrollToBottom.value && isNearBottom()) {
    showScrollToBottom.value = false
  }
}

// WS 推送：新消息入队
async function onWsMessage(msg) {
  // 退群系统消息仅群主可见：非群主直接忽略（不渲染不落库）
  if (isHiddenLeaveMsg(msg, meUid())) return
  const convEntry = Object.entries(realConvMap.value).find(([, convId]) => String(convId) === String(msg.conv_id))
  if (!convEntry) return
  const cid = convEntry[0]
  const contact = conversations.value.find((x) => x.id === cid)
  if (!contact) return
  const isMine = msg.sender_uid === meUid()
  const isActive = activeId.value === cid
  // 传服务端 conv_id（而非面板 id cid）：mapServerMessage 据此匹配会话头像/颜色，
  // 传面板 id 会导致实时接收消息头像永远显示 '?'
  const mapped = mapServerMessage(msg, msg.conv_id)

  // 媒体消息解析本地缓存地址（不落盘会话跳过）
  hydrateMediaCache([mapped], msg.conv_id)

  // 新消息提醒：他人发来的普通消息（含群聊）；自己回显与撤回帧不提醒。
  // 提示音/桌面通知由设置页开关控制（notify.js 内部判断）
  if (!isMine && msg.status !== 1) {
    const title =
      contact.type === 'group' && mapped.senderName ? `${contact.name}：${mapped.senderName}` : contact.name
    notifyNewMessage(title, messagePreview(mapped))
    // 任务栏图标交互：窗口未聚焦（最小化/切到其它应用）时闪烁任务栏按钮，聚焦后自动停止
    if (typeof document !== 'undefined' && !document.hasFocus()) {
      try {
        window.electronAPI?.badge?.flash()
      } catch {}
    }
  }

  // 未读推进去重：仅对“真正新增”的他人消息（seq 超出客户端已知水位）计未读。
  // 登录后离线队列补推的消息 seq 均在水位内，服务端列表未读已计入它们，
  // 再累加会导致未读双倍于气泡数（统计错误）；自己的回显（离线重发）也不计未读。
  const seqNum = Number(msg.seq) || 0
  const countUnread = !isMine && msg.status !== 1 && seqNum > 0 && seqNum > (Number(contact.syncSeq) || 0)

  // 本地落库：乐观回显回填 pending 行，其余 upsert；非活跃会话按 countUnread 推进未读；不落盘会话跳过
  persistWsIncoming(msg, mapped, contact, isMine, isActive, countUnread)

  // 撤回通知（status=1）：把本地对应消息标记为已撤回，不新增气泡、不触发未读/滚动。
  // 保留 text/extra 原文：撤回提示行由模板按 status===1 渲染（不显示原文），并支持"重新编辑"。
  if (msg.status === 1) {
    const existing = contact.messages.find((x) => x.id === String(msg.id) || x.id === 'tmp-' + String(msg.id))
    if (existing) {
      existing.status = 1
      existing.readAt = ''
    }
    // 同步更新会话列表最后消息预览（若被撤回的是最新一条，显示"你/对方撤回了一条消息"）
    refreshConvPreview(cid)
    return
  }

  // 统一更新会话列表元信息（最后消息 + 时间），确保发送/接收后都刷新
  contact.lastMessage = messagePreview(mapped)
  contact.time = formatConvTime(mapped.createdAt)
  contact.lastMsgTime = mapped.createdAt || Math.floor(Date.now() / 1000)

  // 消息去重：同一条消息（全局 id，非 tmp 乐观消息）已存在时不重复追加，
  // 避免弱网重发/离线补发导致重复气泡。tmp 乐观消息仍走下方替换逻辑。
  if (contact.messages.some((x) => x.id === String(msg.id))) {
    return
  }

  if (isActive && isMine) {
    // 替换乐观消息：按消息 ID 匹配（乐观消息 id='tmp-<msgId>'，服务端回显的 msg.id 即前端发送的 msgId）。
    // 不能用内容匹配——服务端对含敏感词的消息会过滤替换内容（如 "你傻X" → "你***"），
    // 回显内容与乐观消息的原始内容不同，内容匹配会失败 → 走 else 追加一条，造成"一条消息显示两条"。
    const idx = contact.messages.findIndex((x) => x.id === 'tmp-' + String(msg.id))
    if (idx >= 0) {
      contact.messages[idx].id = mapped.id
      contact.messages[idx].time = mapped.time
      contact.messages[idx].text = mapped.text // 用服务端回显（已过滤敏感词）的内容覆盖乐观消息
      contact.messages[idx].readAt = ''
      contact.messages[idx].isPending = false
      contact.messages[idx].localId = null // 本地库已由 persistWsIncoming 回填 synced
    } else {
      // 无乐观消息匹配（如离线重发后的回显）：替换同内容 pending 气泡，避免重复
      const pIdx = isMine ? contact.messages.findIndex((x) => x.isPending && x.text === mapped.text) : -1
      if (pIdx >= 0) {
        contact.messages[pIdx] = mapped
      } else {
        contact.messages.push(mapped)
      }
      if (isActive) scrollToBottom()
    }
  } else if (isActive) {
    // 收到对方消息：如果不在底部则显示"回到底部"按钮，不自动滚动
    contact.messages.push(mapped)
    if (isNearBottom()) {
      // 用户正看着最新消息 → 立即回已读回执（仅单聊）
      sendReadReceipt(contact)
      scrollToBottom()
    } else {
      // 用户在浏览历史，新消息未滑到底部 → 暂不标记已读，待滚到底部后补发
      pendingReadAck = true
      showScrollToBottom.value = true
    }
  } else {
    // 非活跃会话：替换同内容 pending 气泡（离线重发回显），避免重复
    const pIdx = isMine ? contact.messages.findIndex((x) => x.isPending && x.text === mapped.text) : -1
    if (pIdx >= 0) {
      contact.messages[pIdx] = mapped
    } else {
      contact.messages.push(mapped)
    }
    // 未读按水位去重（防离线补推双重计数），见 countUnread 计算处
    if (countUnread) contact.unread++
  }
  // 收到新消息后，将会话列表按最后消息时间重新排序（最新置顶）
  reorderConversations()
}

// WS 消息落本地库：
// - 撤回帧（status=1）/普通他人消息：upsert（按 conv_id + server_id 去重）
// - 自己的乐观消息回显：回填 pending 行（tmp 匹配按 localId，离线重发回显按内容认领）
// - 非活跃会话：仅当 countUnread（seq 超出已知水位）时推进本地库未读，防离线补推双重计数
async function persistWsIncoming(msg, mapped, contact, isMine, isActive, countUnread) {
  const convIdStr = String(msg.conv_id)
  // 不落盘会话：不写入任何内容（主进程亦已拦截，双保险）
  if (noPersistSet.value.has(convIdStr)) return
  // 推进同步水位（条件同步的判据）：普通消息/撤回帧（同 seq）/自己回显均计入，MAX 幂等不回退
  const seqNum = Number(msg.seq) || 0
  if (seqNum > 0) localdb.conversations.updateSyncSeq(convIdStr, seqNum)
  // 内存会话项水位同步推进（仅接收帧：服务端只推进接收方视图水位，与之对齐；
  // 会话项若来自旧列表/事件插入，收到新消息后也能自愈，避免每次打开重拉）
  if (!isMine && seqNum > 0 && contact) contact.syncSeq = Math.max(Number(contact.syncSeq) || 0, seqNum)
  if (msg.status === 1) {
    // 撤回：同步更新本地行状态
    localdb.messages.upsert([toDbMessage(msg, convIdStr)])
    return
  }
  if (isMine) {
    const tmpRow = contact.messages.find((x) => x.id === 'tmp-' + String(msg.id))
    if (tmpRow && tmpRow.localId) {
      localdb.messages.setSyncState(tmpRow.localId, 'synced', {
        serverId: String(msg.id),
        seq: Number(msg.seq) || 0,
      })
    } else {
      // 可能是离线重发后的回显：优先认领 pending 行，避免重复
      const claimed = await localdb.messages.claimPending(convIdStr, msg.content)
      if (claimed) {
        localdb.messages.setSyncState(claimed.local_id, 'synced', {
          serverId: String(msg.id),
          seq: Number(msg.seq) || 0,
        })
      } else {
        localdb.messages.upsert([toDbMessage(msg, convIdStr)])
      }
    }
  } else {
    localdb.messages.upsert([toDbMessage(msg, convIdStr)])
  }
  // 更新会话摘要；未读仅在 countUnread 时推进（活跃会话切换时已清零，不重复累加）
  const preview = messagePreview(mapped)
  if (isActive) {
    localdb.conversations.upsert([
      { id: convIdStr, last_msg: preview, last_msg_time: mapped.createdAt, unread: 0 },
    ])
  } else if (countUnread) {
    localdb.conversations.bump(convIdStr, preview, mapped.createdAt)
  } else {
    // 不计未读（离线补推/自己回显）但摘要仍需更新；部分行 upsert 经 COALESCE 保留已有 unread
    localdb.conversations.upsert([{ id: convIdStr, last_msg: preview, last_msg_time: mapped.createdAt }])
  }
}

// 按最后消息时间倒序重排会话列表（最新在最上）
function reorderConversations() {
  const list = conversations.value
  list.sort((a, b) => (b.lastMsgTime || 0) - (a.lastMsgTime || 0))
}

function onWsRead(data) {
  // 对方已读：把该会话自己发出的、seq <= 对方已读游标 的消息标记为已读
  const convEntry = Object.entries(realConvMap.value).find(([, convId]) => convId === data.conv_id)
  if (!convEntry) return
  // 更新对端已读游标缓存（会话切换/历史重载后据此恢复）
  if (data.conv_id) {
    readCursorMap.value[data.conv_id] = Number(data.seq) || 0
  }
  const contact = conversations.value.find((x) => x.id === convEntry[0])
  if (!contact) return
  const readSeq = Number(data.seq) || 0
  contact.messages.forEach((m) => {
    if (m.type === 'out' && m.readAt !== '发送失败' && (!m.seq || m.seq <= readSeq)) {
      m.readAt = '已读'
    }
  })
}

// 恢复会话已读状态：按对端已读游标，把"自己发出且 seq <= 游标"的消息标为已读。
// 会话切换重新加载历史后调用，避免已读未读丢失。
function restoreReadState(contact) {
  if (!contact) return
  const convId = realConvMap.value[contact.id] || contact.convId
  const readSeq = readCursorMap.value[convId] || 0
  if (!readSeq) return
  contact.messages.forEach((m) => {
    if (m.type === 'out' && m.readAt !== '发送失败' && m.seq && m.seq <= readSeq) {
      m.readAt = '已读'
    }
  })
}

// 发送已读回执：通知对方"我已读到该会话最新 seq"。
// 单聊：触发对端把消息标记为已读；群聊：仅更新自己在该会话的已读游标（readSeq），
// 使刷新后服务端计算的未读数正确清零（后端对群聊不回读/不广播对端）。
// WS 未连接或后端未接入时跳过。
function sendReadReceipt(contact) {
  if (!contact) return
  if (!wsClient.isConnected()) return
  const convId = realConvMap.value[contact.id] || contact.convId
  if (!convId) return
  const seq = contact.messages.reduce((max, m) => {
    const s = Number(m.seq) || 0
    return s > max ? s : max
  }, 0)
  if (seq > 0) wsClient.sendRead(convId, seq)
}

// 刷新某个会话列表项的最后消息预览（撤回后可能变化）
function refreshConvPreview(cid) {
  const contact = conversations.value.find((x) => x.id === cid)
  if (!contact || !contact.messages.length) return
  // 取最新一条消息（时间最新）
  const lastMsg = [...contact.messages].reverse().find((m) => m.isPending !== true)
  if (!lastMsg) return
  contact.lastMessage = convLastPreview(lastMsg)
  contact.time = formatConvTime(lastMsg.createdAt)
  contact.lastMsgTime = lastMsg.createdAt || Math.floor(Date.now() / 1000)
  reorderConversations()
}

// ===== 会话列表搜索框旁“+”菜单：发起群聊 / 添加朋友 / 写笔记 =====
const showPlusMenu = ref(false)
const plusMenuEl = ref(null) // 菜单容器：内部点击不关闭

function togglePlusMenu(e) {
  if (e && e.stopPropagation) e.stopPropagation()
  showPlusMenu.value = !showPlusMenu.value
}

// 菜单外点击关闭（document 监听，按钮自身已阻止冒泡）
function onDocClickPlusMenu(e) {
  if (!showPlusMenu.value) return
  if (plusMenuEl.value && plusMenuEl.value.contains(e.target)) return
  showPlusMenu.value = false
}

// 发起群聊：复用建群弹窗，打开前拉取最新好友列表供选成员
const showCreateGroup = ref(false)
const createGroupFriends = ref([])
async function openCreateGroup() {
  showPlusMenu.value = false
  try {
    const flist = await friendApi.list(true)
    createGroupFriends.value = (flist || []).map((f, i) => ({
      id: String(f.uid),
      uid: f.uid,
      name: f.remark || f.nickname || `用户${f.uid}`,
      avatar: f.avatar ? f.avatar[0] : (f.nickname || '?')[0],
      color: CONTACT_COLORS[i % CONTACT_COLORS.length],
    }))
  } catch {
    createGroupFriends.value = []
  }
  showCreateGroup.value = true
}

// 建群成功：关弹窗 + 刷新会话列表（新群立即可见）+ 通知父组件
async function onPlusGroupCreated(info) {
  showCreateGroup.value = false
  try {
    await loadRealData()
  } catch {}
  emit('group-created', info)
}

// 添加朋友：由 App.vue 统一持有 AddFriendModal，发事件打开
function openAddFriendFromMenu() {
  showPlusMenu.value = false
  emit('request-add-friend')
}

// 写笔记：功能暂未实现，保留入口
function openWriteNote() {
  showPlusMenu.value = false
  showToast('笔记功能即将上线，敬请期待', 'info')
}

// 事件驱动的会话增量插入（阶段二减压）：
// conversation.created 事件体携带 conv_id（字符串）与 target 信息，直接本地构建会话项，不再全量重载。
// 信息不足（如旧版事件无 conv_id）时兜底 loadRealData 全量。
async function insertConvFromEvent(data) {
  const convId = data && data.conv_id ? String(data.conv_id) : ''
  if (!convId || convId === '0') return loadRealData()
  const id = `conv-${convId}`
  if (conversations.value.some((c) => c.id === id)) return
  const isGroupConv = Number(data.type) === 2 || data.g_uid != null
  const targetId = isGroupConv ? data.g_uid : data.target_id
  // 替换同 target 的占位会话（通讯录跳转时无会话创建）
  const phIdx = conversations.value.findIndex((c) => String(c.id).startsWith('new-') && String(c.targetId) === String(targetId))
  if (phIdx >= 0) conversations.value.splice(phIdx, 1)
  let contactMap = await buildContactMap()
  let info = contactMap.get(String(targetId))
  if (!info) {
    // 新会话的 target 不在缓存中：好友/群缓存必然过期（如对方刚通过好友请求、
    // 自己刚接受请求但 clearCache 晚于本事件到达），强制刷新一次再查，避免头像兜底 '?'。
    contactMap = await buildContactMap(true)
    info = contactMap.get(String(targetId))
  }
  info = info || {}
  conversations.value.push({
    id,
    name: info.name || data.target_name || (isGroupConv ? `群 ${targetId}` : `用户 ${targetId}`),
    avatar: info.avatar || (data.target_name || '?')[0],
    color: info.color || '#64748b',
    online: false,
    type: isGroupConv ? 'group' : info.type || null,
    lastMessage: '',
    time: '',
    lastMsgTime: 0,
    unread: 0,
    targetId,
    convId,
    peerReadSeq: 0,
    syncSeq: 0,
    messages: [],
    oldestSeq: 0,
    _hasMore: false,
  })
  realConvMap.value[id] = convId
  // 落本地库：下次启动从本地秒开
  localdb.conversations.upsert([
    { id: convId, type: isGroupConv ? 2 : 1, target_id: String(targetId), last_msg: '', last_msg_time: 0, unread: 0, peer_read_seq: 0, last_synced_seq: 0 },
  ])
}

// 处理 WS social 帧（好友/群/会话事件）
function onWsSocial(body) {
  if (!body || !body.event) return
  if (body.event === 'friend.accepted') {
    // 发起方收到对方通过：本地好友缓存已过期，失效后由后续流程拉取最新列表
    friendApi.clearFriendCache()
    repairUnknownAvatars()
    return
  }
  if (body.event === 'conversation.created') {
    // 新会话：增量插入，不再全量重载（阶段二减压）
    insertConvFromEvent(body.data)
    return
  }
  if (body.event === 'group.updated') {
    // 群名/公告被管理员修改：刷新群会话展示名；失效资料缓存使下次打开拉最新
    const d = body.data || {}
    if (d.g_uid == null) return
    conversations.value.forEach((c) => {
      if (c.type === 'group' && String(c.targetId) === String(d.g_uid) && d.name) c.name = d.name
    })
    gp.groupMembersCache.delete(d.g_uid)
    gp.groupMembersCache.delete(Number(d.g_uid))
    const cur = currentContact.value
    if (cur && cur.type === 'group' && String(cur.targetId) === String(d.g_uid)) {
      gp.groupInfo = {
        ...gp.groupInfo,
        name: d.name || gp.groupInfo.name,
        announcement: d.announcement != null ? d.announcement : gp.groupInfo.announcement,
      }
    }
    return
  }
  if (body.event === 'group.left') {
    // 退群者（其他设备同步）：清理本地会话与消息
    const d = body.data || {}
    const convIdStr = d.conv_id ? String(d.conv_id) : ''
    const cid = convIdStr ? `conv-${convIdStr}` : ''
    conversations.value = conversations.value.filter((x) => x.id !== cid)
    if (convIdStr) {
      localdb.messages.removeByConv(convIdStr)
      localdb.conversations.remove(convIdStr)
      noPersistSet.value.delete(convIdStr)
    }
    if (d.g_uid != null) gp.groupMembersCache.delete(Number(d.g_uid))
    if (cid && activeId.value === cid) activeId.value = ''
    return
  }
  // group.invite 等：会话项由 conversation.created 事件插入，无需全量重载
}

// 修复头像为 '?' 的会话项（插入时联系人缓存过期所致）：强制刷新联系人缓存后回填
async function repairUnknownAvatars() {
  if (!conversations.value.some((c) => c.avatar === '?')) return
  const contactMap = await buildContactMap(true)
  conversations.value.forEach((c) => {
    if (c.avatar !== '?' || c.targetId == null) return
    const info = contactMap.get(String(c.targetId))
    if (!info) return
    c.name = info.name
    c.avatar = info.avatar
    c.color = info.color
  })
}

// 处理 AddFriendModal 通过 window 事件触发的刷新
function onCustomFriendAdded() {
  // 添加好友成功：会话项由 WS conversation.created 事件增量插入（接受方 handleRequest
  // 的 clearCache 可能晚于事件到达，产生 '?' 头像）；WS 在线时仅修复头像，断开则兜底全量重载。
  if (!wsClient.isConnected()) return loadRealData()
  repairUnknownAvatars()
}

// 从通讯录缓存构建 target → 展示资料 的映射（好友/群），用于会话列表展示。
// 缓存为空时主动请求一次（兜底刷新后未访问通讯录页导致头像缺失的场景）。
async function buildContactMap(force = false) {
  const map = new Map()
  try {
    // 仅当好友缓存从未初始化时请求后端；缓存已初始化（含空列表）则直接复用。
    // 缓存持久化到 localStorage，刷新后仍保留，因此消息页刷新不会重复请求 /friends。
    // force=true：联系人关系刚变化（新好友/新群），缓存必然过期，强制拉取最新列表。
    const needFetch = force || !friendApi.isFriendCacheLoaded()
    const friends = await friendApi.list(needFetch)
    if (friends) {
      friends.forEach((f, i) => {
        // 展示名备注优先（与微信一致），无备注回落昵称
        map.set(String(f.uid), {
          name: f.remark || f.nickname || `用户${f.uid}`,
          avatar: (f.avatar ? f.avatar[0] : (f.nickname || '?')[0]),
          color: CONTACT_COLORS[i % CONTACT_COLORS.length],
          type: null,
        })
        // 同步备注信息：供资料面板展示/编辑备注
        friendRemarkMap.value[String(f.uid)] = {
          remark: f.remark || '',
          nickname: f.nickname || `用户${f.uid}`,
        }
      })
    }
  } catch {}
  try {
    const needFetch = force || !groupApi.isGroupCacheLoaded()
    const groups = await groupApi.list(needFetch)
    if (groups) {
      groups.forEach((g, i) => {
        map.set(String(g.g_uid), {
          name: g.name || `群${g.g_uid}`,
          avatar: (g.name || '群')[0],
          color: CONTACT_COLORS[(i + 3) % CONTACT_COLORS.length],
          type: 'group',
        })
      })
    }
  } catch {}
  return map
}

// 加载真实会话列表 + 各会话历史（离线优先：先本地库秒开，再网络刷新落库）
async function loadRealData() {
  const contactMap = await buildContactMap()

  // 秒开：先读当前账户本地库立即渲染（会话未打开/浏览器环境降级为空）
  const localConvs = await localdb.conversations.list()
  // 维护不落盘会话集合（开关状态持久化在本地库）
  noPersistSet.value = new Set(
    localConvs.filter((c) => c.no_persist === 1).map((c) => String(c.id))
  )
  if (localConvs.length) {
    useRealBackend.value = true
    // 本地秒开：过滤 target_id 缺失的损坏行（历史 bug 遗留，表现为“用户 null”且无法发送），
    // 网络刷新后以服务端数据重建；离线时这类行本身也无法使用
    applyConvList(localConvs.filter((c) => c.target_id != null).map((c) => toConvItem(c, contactMap)))
  }

  let convs
  // 差量刷新（阶段二减压）：本地已有数据时，只拉本地最新最后消息时间之后变化的会话
  // （无消息的空会话服务端也会返回，保证完整）；本地无数据则全量拉取。
  const maxLocalTime = localConvs.reduce((mx, c) => Math.max(mx, Number(c.last_msg_time) || 0), 0)
  const diffMode = maxLocalTime > 0
  try {
    convs = diffMode
      ? await messageApi.listConversations({ changedSince: maxLocalTime - 1 }) // -1s 容错边界重复，合并时按 id 去重
      : await messageApi.listConversations()
  } catch {
    // 后端不可达：有本地数据则离线渲染，否则保留 mock 兜底
    if (localConvs.length) {
      if (activeId.value) await reloadActiveConvMessages()
      return
    }
    return
  }
  useRealBackend.value = true

  // 网络数据落本地库（差量模式仅落变化的会话）
  const incoming = (convs || []).map((c) => toConvItem(c, contactMap))
  localdb.conversations.upsert(
    (convs || []).map((c) => ({
      id: String(c.id),
      type: c.type,
      target_id: c.target_id != null ? String(c.target_id) : null,
      last_msg: c.last_msg || '',
      last_msg_time: Number(c.last_msg_time) || 0,
      unread: Number(c.unread) || 0,
      peer_read_seq: Number(c.peer_read_seq) || 0,
      last_synced_seq: Number(c.last_synced_seq) || 0,
    }))
  )
  let list = incoming
  if (diffMode) {
    // 差量合并：未变化会话保留现有项（含本地维护的未读/水位），同 target 占位会话被真实会话替换
    const incomingIds = new Set(incoming.map((c) => c.id))
    const incomingTargets = new Set(incoming.map((c) => String(c.targetId)))
    const kept = conversations.value.filter(
      (c) => !incomingIds.has(c.id) && !(String(c.id).startsWith('new-') && incomingTargets.has(String(c.targetId)))
    )
    list = incoming.concat(kept)
  }
  applyConvList(list)
  // 仅加载已有选中会话的消息
  if (activeId.value && conversations.value.some((x) => x.id === activeId.value)) {
    await reloadActiveConvMessages()
  }
}

// 重新加载当前选中会话的消息（秒开/网络刷新后）
async function reloadActiveConvMessages() {
  const activeContact = conversations.value.find((x) => x.id === activeId.value)
  if (activeContact && activeContact.convId) {
    await loadConversationMessages(activeContact)
    scrollToBottom()
  }
}

// 应用会话列表：排序 + 注册 conv_id 映射/已读游标；保留已加载的消息数组避免切换闪烁
function applyConvList(list) {
  const prevById = new Map(conversations.value.map((c) => [c.id, c]))
  // 当前选中的是占位会话（通讯录跳转且从未聊过）：真实会话出现后无缝切换，继承已加载消息
  if (activeId.value && activeId.value.startsWith('new-')) {
    const key = activeId.value.slice(4)
    const real = list.find((c) => String(c.targetId) === key)
    if (real) {
      const ph = prevById.get(activeId.value)
      if (ph && ph.messages && ph.messages.length) {
        real.messages = ph.messages
        real.lastMessage = ph.lastMessage
        real.time = ph.time
        real.lastMsgTime = ph.lastMsgTime
      }
      // 占位会话的暂存附件迁移到真实会话键，避免首条消息发出后暂存项丢失
      if (staged.stagedFilesMap[activeId.value]) {
        staged.stagedFilesMap[real.id] = staged.stagedFilesMap[activeId.value]
        delete staged.stagedFilesMap[activeId.value]
      }
      activeId.value = real.id
    }
  }
  list.forEach((c) => {
    const prev = prevById.get(c.id)
    if (prev && prev.messages && prev.messages.length) {
      c.messages = prev.messages
      c.oldestSeq = prev.oldestSeq
      c._hasMore = prev._hasMore
    }
  })
  // 会话列表按最后一条消息时间倒序排列（最新在最上，类似微信）
  list.sort((a, b) => (b.lastMsgTime || 0) - (a.lastMsgTime || 0))
  conversations.value = list
  list.forEach((c) => {
    realConvMap.value[c.id] = c.convId
    // 记录对端已读游标（单聊），用于加载历史后恢复已读状态
    if (c.convId && c.type !== 'group' && c.peerReadSeq) {
      readCursorMap.value[c.convId] = Number(c.peerReadSeq) || 0
    }
    // 乐观预览还原：该会话有附件正在上传时，用乐观预览覆盖本地/服务端旧摘要，
    // 避免上传窗口内切页/刷新导致 [视频]/[语音] 变回 [文件]
    const op = staged.uploadingPreviewMap[String(c.convId)] || staged.uploadingPreviewMap[String(c.id)]
    if (op && Number(op.time) >= (Number(c.lastMsgTime) || 0)) {
      c.lastMessage = op.preview
    }
  })
  healMediaPreviews(list)
}

// 存量摘要自愈：录音/视频以 FILE(type=3) 发出，服务端/旧本地摘要存为 [文件]；
// 用本地库最后一条消息识别音频/视频并纠正为 [语音]/[视频]（同步修本地库）。
// 注：服务端旧数据仍会下发 [文件] 覆盖本地，此处每次列表应用时重新纠正保证展示正确。
function healMediaPreviews(list) {
  if (!localdb.available()) return
  for (const c of list || []) {
    if (c.lastMessage !== '[文件]' || !c.convId) continue
    healOneConvPreview(c, 0)
  }
}

// 单会话摘要自愈：取本地库最近若干条已同步行判定。
// 优先匹配列表项最后消息时间（±2s）对应的行，避免被 pending/更早行误导；
// 本地数据未追平（WS 落库竞态/乐观消息未发出）时延迟重试，覆盖“偶尔显示 [文件]”的竞态窗口。
function healOneConvPreview(c, attempt) {
  const convIdStr = String(c.convId)
  localdb.messages.list(convIdStr, { limit: 10 }).then((rows) => {
    const synced = (rows || []).filter((r) => Number(r.seq) > 0) // pending 行不参与摘要判定
    if (!synced.length) return retryHeal(c, attempt)
    const targetTime = Number(c.lastMsgTime) || 0
    // 优先取与列表最后消息时间匹配的行；无匹配时退化为最新已同步行
    let last = null
    if (targetTime > 0) last = synced.find((r) => Math.abs(Number(r.created_at) - targetTime) <= 2) || null
    if (!last) last = synced[synced.length - 1]
    let extra = {}
    try {
      extra = last.extra ? JSON.parse(last.extra) : {}
    } catch {}
    const pseudo = { msgType: Number(last.type) || 1, extra }
    let fixed = ''
    if (isAudioMsg(pseudo)) fixed = '[语音]'
    else if (isVideoMsg(pseudo)) fixed = '[视频]'
    if (fixed) {
      c.lastMessage = fixed
      localdb.conversations.upsert([{ id: convIdStr, last_msg: fixed }])
      return
    }
    // 本地最新行与列表时间对不上：本地数据尚未追平，稍后重试
    if (targetTime > 0 && Math.abs(Number(last.created_at) - targetTime) > 2) retryHeal(c, attempt)
  }).catch(() => {})
}

// 自愈重试：最多 3 次；期间摘要已被其它路径（WS/发送）更新则停止
function retryHeal(c, attempt) {
  if (attempt >= 2) return
  setTimeout(() => {
    if (c.lastMessage !== '[文件]') return
    healOneConvPreview(c, attempt + 1)
  }, attempt === 0 ? 800 : 2000)
}

// 挂载时接入真实后端 + WS
onMounted(() => {
  document.addEventListener('click', onDocClickPlusMenu)
  if (useRealBackend.value) return
  loadRealData().then(() => {
    if (!useRealBackend.value) return
    wsClient.on('message', onWsMessage)
    wsClient.on('read', onWsRead)
    wsClient.on('social', onWsSocial)
    wsClient.on('status', onWsStatus)
    wsStatus.value = wsClient.getStatus()
    wsClient.connect()
    window.addEventListener('wc:friend-added', onCustomFriendAdded)
  })
})

// 首次渲染后默认停在顶部（不滚动到底部）；支持 URL query 调试钩子
onMounted(() => {
  // 开发调试：支持 URL query `?conversation=<id>&profile=1&search=<kw>` 或 hash
  if (typeof window !== 'undefined') {
    const raw = window.location.hash || window.location.search
    // 支持多种分隔符：&, ;, , 以适配不同 shell 的 URL 转义限制
    const m = raw.match(/conversation=([\w-]+)/)
    if (m && conversations.value.some((c) => c.id === m[1])) {
      activeId.value = m[1]
    }
    if (/profile=1/.test(raw)) {
      showProfile.value = true
      // 调试钩子：profile=1 同时让面板自动滚到底，用于截图验证消息免打扰/退出群聊可达
      nextTick(() => {
        const p = document.querySelector('.profile')
        if (p) p.scrollTop = p.scrollHeight
      })
    }
    const sm = raw.match(/search=([^&?#;,]+)/)
    if (sm) gp.memberSearch = decodeURIComponent(sm[1])
    // 调试钩子：convSearch=<kw> 设置会话搜索关键字
    const cs = raw.match(/convSearch=([^&?#;,]+)/)
    if (cs) convSearch.value = decodeURIComponent(cs[1])
  }
})

// 页面重新激活（KeepAlive）：不再全量重拉会话列表。
// 本地库已提供数据，新增变化由 WS 推送（消息/新会话事件）增量驱动，减少重复请求。
onActivated(() => {
  // 如需强制刷新可手动触发 loadRealData()
})

onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClickPlusMenu)
  window.removeEventListener('wc:friend-added', onCustomFriendAdded)
})
</script>

<template>
  <div class="window" @dragover.prevent="staged.preventWindowDrag" @drop.prevent="staged.preventWindowDrag">
    <!-- 主体区 -->
    <main class="body">
      <!-- 会话列表 -->
      <aside class="conv-list">
        <div class="search-bar">
          <div class="search-box">
            <svg viewBox="0 0 16 16" width="16" height="16" class="search-icon">
              <circle cx="6.67" cy="6.67" r="4.67" fill="none" stroke="currentColor" stroke-width="1.4" />
              <line x1="10.67" y1="10.67" x2="13.33" y2="13.33" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
            </svg>
            <input
              ref="convSearchEl"
              v-model="convSearch"
              class="search-input"
              type="text"
              placeholder="搜索会话、联系人或消息"
              aria-label="搜索会话、联系人或消息"
              @keydown="onConvSearchKeydown"
            />
            <button
              v-if="convSearch"
              class="search-clear"
              type="button"
              aria-label="清空搜索"
              @click="convSearch = ''"
            >
              <svg viewBox="0 0 12 12" width="12" height="12">
                <line x1="3" y1="3" x2="9" y2="9" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
                <line x1="9" y1="3" x2="3" y2="9" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" />
              </svg>
            </button>
          </div>
          <!-- “+”按钮：发起群聊 / 添加朋友 / 写笔记 -->
          <div ref="plusMenuEl" class="plus-wrap">
            <button
              class="plus-btn"
              :class="{ active: showPlusMenu }"
              type="button"
              aria-label="新建操作"
              aria-haspopup="menu"
              :aria-expanded="showPlusMenu"
              @click="togglePlusMenu"
            >
              <svg viewBox="0 0 16 16" width="16" height="16">
                <path d="M8 3v10M3 8h10" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
              </svg>
            </button>
            <div v-if="showPlusMenu" class="plus-menu" role="menu">
              <button class="plus-menu-item" role="menuitem" @click="openCreateGroup">
                <svg viewBox="0 0 20 20" width="18" height="18">
                  <path d="M3.5 4.5a3 3 0 0 1 3-3h7a3 3 0 0 1 3 3v6a3 3 0 0 1-3 3H8l-3.5 3v-3a3 3 0 0 1-1-2.2v-6.8z" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" />
                  <path d="M10 5.5v4M8 7.5h4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
                </svg>
                <span>发起群聊</span>
              </button>
              <button class="plus-menu-item" role="menuitem" @click="openAddFriendFromMenu">
                <svg viewBox="0 0 20 20" width="18" height="18">
                  <circle cx="8" cy="6.5" r="2.8" fill="none" stroke="currentColor" stroke-width="1.4" />
                  <path d="M2.8 16.2c0-2.6 2.3-4.3 5.2-4.3s5.2 1.7 5.2 4.3" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
                  <path d="M15.3 5.2v4M13.3 7.2h4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
                </svg>
                <span>添加朋友</span>
              </button>
              <button class="plus-menu-item" role="menuitem" @click="openWriteNote">
                <svg viewBox="0 0 20 20" width="18" height="18">
                  <rect x="4" y="2.5" width="12" height="15" rx="2" fill="none" stroke="currentColor" stroke-width="1.4" />
                  <path d="M7 7h6M7 10.5h6M7 14h3.5" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
                </svg>
                <span>写笔记</span>
              </button>
            </div>
          </div>
        </div>
        <div class="list-header">
          <span class="list-title">会话</span>
          <!-- WS 连接状态指示（会话列表标题旁） -->
          <span class="ws-status" :class="wsStatusClass">
            <span class="ws-status-dot"></span>
            <span class="ws-status-text">{{ wsStatusText }}</span>
          </span>
        </div>
        <div
          ref="listEl"
          class="list-scroll"
          role="listbox"
          aria-label="会话列表"
          tabindex="0"
          @keydown="handleListKeydown"
        >
          <div
            v-for="(c, i) in filteredConversations"
            :key="c.id"
            :data-id="c.id"
            class="conv-item"
            :class="{ active: c.id === activeId }"
            role="option"
            :aria-selected="c.id === activeId"
            :aria-label="`${c.name}，${c.unread > 0 ? c.unread + '条未读' : ''}，最近消息：${c.lastMessage}`"
            :tabindex="c.id === activeId ? 0 : -1"
            @click="selectConversation(c.id)"
            @keydown.enter.prevent="selectConversation(c.id)"
            @keydown.space.prevent="selectConversation(c.id)"
          >
            <div class="avatar" :style="{ background: c.color }">
              <span>{{ c.avatar }}</span>
            </div>
            <div class="conv-text">
              <div class="conv-name">{{ c.name }}</div>
              <div class="conv-last">{{ c.lastMessage }}</div>
            </div>
            <div class="conv-meta">
              <span class="conv-time">{{ c.time }}</span>
              <span v-if="c.unread > 0" class="unread-badge" :aria-label="`未读消息 ${c.unread} 条`">{{ formatUnread(c.unread) }}</span>
            </div>
          </div>
          <!-- 搜索无结果占位 -->
          <div v-if="convSearch && filteredConversations.length === 0" class="conv-empty">
            <p class="conv-empty-title">未找到匹配的会话</p>
            <p class="conv-empty-sub">试试搜索"林"、"群"或"设计"</p>
          </div>
        </div>
      </aside>

      <div class="divider-col"></div>

      <!-- 聊天区：支持拖拽图片/文件到此处释放上传 -->
      <section
        class="chat"
        @dragenter="staged.onChatDragEnter"
        @dragover="staged.onChatDragOver"
        @dragleave="staged.onChatDragLeave"
        @drop="staged.onChatDrop"
      >
        <!-- 聊天头部 -->
        <header class="chat-header">
          <div class="chat-header-left">
            <span class="contact-name">{{ hasActiveContact ? contactMeta.name : 'WorkChat' }}</span>
          </div>
          <div class="chat-header-right">
            <!-- 顶部"查找聊天记录"按钮：方便用户不打开资料面板也能直接搜 -->
            <button class="circle-btn" aria-label="查找聊天记录" @click="openSearchHistory">
              <svg viewBox="0 0 22 22" width="22" height="22">
                <circle cx="9.17" cy="9.17" r="6.17" fill="none" stroke="currentColor" stroke-width="1.5" />
                <line x1="14.33" y1="14.33" x2="18.33" y2="18.33" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
              </svg>
            </button>
            <button class="circle-btn" aria-label="语音通话" :disabled="!call.canVoiceCall" @click="call.startVoiceCall()">
              <svg viewBox="0 0 22 22" width="22" height="22">
                <path d="M4.5 6.5c0-1.7 1.3-3 3-3 1 0 1.8.5 2.4 1.3l1.2 1.6c.3.4.3.9 0 1.3l-1.4 1.7a10 10 0 0 0 4.4 4.4l1.7-1.4c.4-.3.9-.3 1.3 0l1.6 1.2c.8.6 1.3 1.4 1.3 2.4 0 1.7-1.3 3-3 3h-.5c-7-1-12.6-6.6-13.5-13.5v-.5z"
                  fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
              </svg>
            </button>
            <button class="circle-btn" aria-label="视频通话" :disabled="!hasActiveContact" @click="call.startVideoCall()">
              <svg viewBox="0 0 22 22" width="22" height="22">
                <rect x="2.75" y="5.5" width="12" height="11" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.5" />
                <path d="M14.67 9.5l4.58-2.08v7.16L14.67 12.5z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
              </svg>
            </button>
            <button class="circle-btn" :class="{ active: showProfile }" aria-label="更多" @click="toggleProfile">
              <svg viewBox="0 0 22 22" width="22" height="22">
                <circle cx="6" cy="11" r="1.5" fill="currentColor" />
                <circle cx="11" cy="11" r="1.5" fill="currentColor" />
                <circle cx="16" cy="11" r="1.5" fill="currentColor" />
              </svg>
            </button>
          </div>
        </header>
        <div class="chat-divider"></div>

        <!-- 消息区容器：胶囊的定位基准，仅覆盖消息滚动区（不含头部/输入栏） -->
        <div class="messages-area">
        <!-- 消息区 -->
        <div class="messages" :class="transitionState" role="log" aria-live="polite" aria-label="聊天记录">
          <!-- 加载历史消息指示器 -->
          <div v-if="chatLoading" class="loading-more" aria-label="正在加载消息">
            <span class="spinner"></span>
            <span>加载中…</span>
          </div>

          <!-- 未选中会话占位（首次进入不默认选中） -->
          <div v-else-if="!hasActiveContact" class="empty-chat">
            <div class="empty-avatar welcome">
              <svg viewBox="0 0 32 32" width="32" height="32">
                <path d="M6 7a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H13l-5 4v-4H8a2 2 0 0 1-2-2V7z" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" />
              </svg>
            </div>
            <p class="empty-title">选择一个会话开始聊天</p>
            <p class="empty-sub">从左侧会话列表选择联系人，或发起新的对话</p>
          </div>

          <!-- 空会话占位 -->
          <div v-else-if="!hasMessages" class="empty-chat">
            <div class="empty-avatar" :style="{ background: contactMeta.color }">
              <span>{{ contactMeta.avatar }}</span>
            </div>
            <p class="empty-title">暂无消息</p>
            <p class="empty-sub">和 {{ contactMeta.name }} 打个招呼，开始聊天吧</p>
          </div>

          <!-- 消息列表：按天分组，每天独立展示 -->
          <template v-else>
            <template v-for="dayGroup in currentMsgGroups" :key="dayGroup.dayKey">
              <!-- 每天分组标题（居中） -->
              <div class="day-divider">
                <span class="day-divider-text">{{ dayGroup.dayLabel }}</span>
              </div>

              <!-- 每条消息独立展示：时间分隔独占一行，消息行另起一行 -->
              <template v-for="meta in dayGroup.list" :key="meta.msg.id">
                <!-- 组内时间分隔（间隔超过 5 分钟）：独立占一行，居中 -->
                <div v-if="meta.showTimeDivider" class="time-pill">
                  <span class="time-text">{{ formatTimeDivider(meta.msg.createdAt) }}</span>
                </div>

                <div
                  class="message-row"
                  :class="[meta.msg.type, { highlighted: highlightMessageId === meta.msg.id }]"
                  :data-msg-id="meta.msg.id"
                  @contextmenu.prevent="msgMenuApi.openMsgMenu($event, meta.msg)"
                >
                  <!-- 系统消息：居中灰色胶囊，无头像/气泡 -->
                  <div v-if="meta.msg.isSystem" class="system-msg">
                    <span class="system-msg-inner">{{ meta.msg.text }}</span>
                  </div>
                  <!-- 已撤回消息：居中提示行（自己显示"你撤回了一条消息"+重新编辑；对方显示"XX撤回了一条消息"） -->
                  <div v-else-if="meta.msg.status === 1" class="recall-msg">
                    <span class="recall-text">
                      {{ meta.msg.type === 'out' ? '你撤回了一条消息' : (meta.msg.senderName || '对方') + '撤回了一条消息' }}
                    </span>
                    <button v-if="meta.msg.type === 'out'" class="recall-edit-link" @click="msgMenuApi.recallEdit(meta.msg)">重新编辑</button>
                  </div>
                  <template v-else-if="meta.msg.type === 'in'">
                    <div class="avatar small" :style="{ background: meta.msg.color }">
                      <span>{{ meta.msg.avatar }}</span>
                    </div>
                    <div class="in-col">
                      <!-- 群聊：对方消息上方显示发送者昵称 -->
                      <div v-if="isGroupChat && meta.msg.senderName" class="sender-name">{{ meta.msg.senderName }}</div>
                      <MessageBubble
                        :msg="meta.msg"
                        side="in"
                        :voice="voicePlayer"
                        @menu="msgMenuApi.openMsgMenu"
                        @image-loaded="media.onImageLoaded"
                        @open-image="media.openImage"
                        @open-video="media.openVideo"
                        @open-file="media.openFile"
                        @video-error="media.onBubbleVideoError"
                      />
                    </div>
                  </template>
                  <template v-else>
                    <div class="out-col">
                      <MessageBubble
                        :msg="meta.msg"
                        side="out"
                        :voice="voicePlayer"
                        @menu="msgMenuApi.openMsgMenu"
                        @image-loaded="media.onImageLoaded"
                        @open-image="media.openImage"
                        @open-video="media.openVideo"
                        @open-file="media.openFile"
                        @video-error="media.onBubbleVideoError"
                      />
                      <div class="send-status">
                        <svg viewBox="0 0 14 14" width="14" height="14">
                          <path d="M2 7l2.5 2.5L8 5" fill="none" stroke="#bfdbfe" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
                          <path d="M5 7l2.5 2.5L11 5" fill="none" stroke="#bfdbfe" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
                        </svg>
                        <span class="status-text-out">{{ meta.msg.readAt }}</span>
                      </div>
                    </div>
                    <div class="avatar small" :style="{ background: meta.msg.color }">
                      <span>{{ meta.msg.avatar }}</span>
                    </div>
                  </template>
                </div>
              </template>
            </template>

            <!-- 长按/右键消息操作菜单 -->
            <div
              v-if="msgMenuApi.msgMenu"
              class="msg-menu"
              :style="{ left: msgMenuApi.msgMenu.x + 'px', top: msgMenuApi.msgMenu.y + 'px' }"
              @click.stop
            >
              <button class="msg-menu-item" @click="msgMenuApi.copyMsgText()">
                <svg viewBox="0 0 16 16" width="14" height="14">
                  <rect x="5" y="5" width="8" height="8" rx="1" fill="none" stroke="currentColor" stroke-width="1.3" />
                  <path d="M3 10V3.5C3 3 3 3 3.5 3H10" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
                <span>复制</span>
              </button>
              <button
                v-if="msgMenuApi.canRecall(msgMenuApi.menuMsg)"
                class="msg-menu-item danger"
                @click="msgMenuApi.recallMessage()"
              >
                <svg viewBox="0 0 16 16" width="14" height="14">
                  <path d="M6.5 3l.5-.5h2l.5.5H12v1.5H4V3h2.5zM5 5.5h6l-.6 8a1 1 0 01-1 .9H6.6a1 1 0 01-1-.9l-.6-8z" fill="currentColor" />
                </svg>
                <span>撤回</span>
              </button>
            </div>
          </template>
        </div>

        <!-- 微信风格：浏览历史时收到新消息，底部浮出“你有新消息”胶囊，点击回到最新消息 -->
        <transition name="scroll-btn">
          <button
            v-if="showScrollToBottom"
            class="new-msg-pill"
            aria-label="你有新消息，点击回到底部"
            @click="jumpToBottom"
          >
            <span>你有新消息</span>
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
              <path d="M3 6l5 5 5-5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
        </transition>
        </div>

        <!-- 图片预览遮罩 -->
        <transition name="fade">
          <div v-if="media.imagePreview" class="image-preview" @click="media.closeImagePreview()">
            <div class="image-preview-inner" @click.stop>
              <img :src="media.imagePreview.url" :alt="media.imagePreview.name" class="image-preview-img" />
              <div class="image-preview-footer">
                <span class="image-preview-name">{{ media.imagePreview.name }}</span>
                <button class="image-preview-close" @click="media.closeImagePreview()">×</button>
              </div>
            </div>
          </div>
        </transition>

        <!-- 视频播放遮罩：原生 video 控件 + 自动播放，点击遮罩或关闭按钮停止（v-if 卸载即停） -->
        <transition name="fade">
          <div v-if="media.videoPlayer" class="video-preview" @click="media.closeVideo()">
            <div class="video-preview-inner" @click.stop>
              <video class="video-preview-player" :src="media.videoPlayer.url" controls autoplay @error="media.onPlayerError"></video>
              <div class="image-preview-footer">
                <span class="image-preview-name">{{ media.videoPlayer.name }}</span>
                <button class="image-preview-close" aria-label="关闭视频" @click="media.closeVideo()">×</button>
              </div>
            </div>
          </div>
        </transition>

        <!-- 输入栏（未选中会话时不显示） -->
        <footer v-if="hasActiveContact" class="input-bar">
          <div class="tools-row">
            <button class="tool-btn" :class="{ active: showEmojiPanel }" aria-label="表情" @click="toggleEmojiPanel">
              <svg viewBox="0 0 20 20" width="20" height="20">
                <circle cx="10" cy="10" r="7.5" fill="none" stroke="currentColor" stroke-width="1.5" />
                <path d="M6.5 12c.8 1.2 2 2 3.5 2s2.7-.8 3.5-2" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
                <circle cx="7.5" cy="8.5" r="1" fill="currentColor" />
                <circle cx="12.5" cy="8.5" r="1" fill="currentColor" />
              </svg>
            </button>
            <button class="tool-btn" aria-label="图片" @click="staged.sendImage()">
              <svg viewBox="0 0 20 20" width="20" height="20">
                <rect x="2.5" y="3.33" width="15" height="13.33" rx="2" fill="none" stroke="currentColor" stroke-width="1.5" />
                <circle cx="7.5" cy="8.33" r="1.5" fill="none" stroke="currentColor" stroke-width="1.5" />
                <path d="M3.33 14.17l4-3.5 3 2.5 3-2 4.34 3" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
              </svg>
            </button>
            <!-- 文件按钮：回形针（附件）图标，大小/线宽/hover 效果与工具栏其他按钮完全一致 -->
            <button class="tool-btn" aria-label="发送文件" title="发送文件" @click="staged.sendFile()">
              <svg viewBox="0 0 20 20" width="20" height="20">
                <path
                  d="M17.87 9.21l-7.66 7.66a5 5 0 0 1-7.07-7.07l7.66-7.66a3.33 3.33 0 0 1 4.72 4.72l-7.67 7.66a1.67 1.67 0 0 1-2.36-2.36l7.07-7.07"
                  fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
                />
              </svg>
            </button>
            <!-- 语音按钮：点击开始录音、再点结束并进附件暂存区；无麦克风时 toast 提示 -->
            <button
              class="tool-btn"
              :class="{ recording: staged.recording }"
              :aria-label="staged.recording ? '结束录音' : '开始录音'"
              :title="staged.recording ? '再点击一次结束录音' : '点击开始录音'"
              @click="staged.toggleRecordVoice()"
            >
              <svg viewBox="0 0 20 20" width="20" height="20">
                <rect x="7.5" y="2.5" width="5" height="9.17" rx="2.5" fill="none" stroke="currentColor" stroke-width="1.5" />
                <path d="M4.17 9.17a5.83 5.83 0 0 0 11.66 0M10 15v2.5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
              </svg>
            </button>
          </div>

          <!-- 表情面板（EmojiPanel 组件） -->
          <transition name="emoji-panel">
            <EmojiPanel v-if="showEmojiPanel" ref="emojiPanelRef" @select="insertEmoji" />
          </transition>

          <!-- 暂存附件区：选择/拖入的图片与文件先在此预览，点击发送才真实上传；× 可移除 -->
          <div v-if="staged.stagedFiles.length" class="staged-files">
            <div v-for="s in staged.stagedFiles" :key="s.id" class="staged-item" :title="s.name">
              <template v-if="s.kind === 'image'">
                <img class="staged-thumb" :src="s.previewUrl" alt="图片预览" />
              </template>
              <template v-else>
                <div class="staged-file-card">
                  <div class="staged-file-text">
                    <span class="staged-file-name">{{ s.name }}</span>
                    <span class="staged-file-size">{{ formatFileSize(s.size) }}</span>
                  </div>
                </div>
              </template>
              <button class="staged-remove" aria-label="移除附件" @click="staged.removeStaged(s.id)">
                <svg viewBox="0 0 12 12" width="10" height="10">
                  <path d="M3 3l6 6M9 3l-6 6" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
                </svg>
              </button>
            </div>
          </div>

          <!-- 输入框拖动手柄：上下拖动调整高度（上限 280px） -->
          <div class="input-resize-handle" title="拖动调整输入框高度" @mousedown="startInputResize">
            <span class="handle-bar"></span>
          </div>

          <div ref="inputBoxEl" class="input-box" :style="inputBoxHeight ? { height: inputBoxHeight + 'px' } : null">
            <textarea
              ref="inputFieldEl"
              v-model="message"
              class="input-field"
              placeholder="输入消息…"
              rows="2"
              @input="autoResizeInput"
              @keydown.enter.exact.prevent="send"
            ></textarea>
          </div>

          <div class="footer-row">
            <div class="lock-hint">
              <svg viewBox="0 0 14 14" width="14" height="14">
                <rect x="2.92" y="6.42" width="8.17" height="5.25" rx="1" fill="none" stroke="currentColor" stroke-width="1.2" />
                <path d="M4.67 6.42V4.83a2.33 2.33 0 0 1 4.66 0v1.59" fill="none" stroke="currentColor" stroke-width="1.2" />
              </svg>
              <span>消息已端到端加密，并保存在本地</span>
            </div>
            <span class="shortcut">Enter 发送 · Shift+Enter 换行</span>
            <button class="send-btn" :disabled="!message.trim() && !staged.stagedFiles.length" @click="send">
              <svg viewBox="0 0 16 16" width="16" height="16">
                <path d="M2 8l12-6-4 14-3-6-5-2z" fill="none" stroke="#fff" stroke-width="1.5" stroke-linejoin="round" />
              </svg>
              <span>发送</span>
            </button>
          </div>
        </footer>
        <!-- 拖拽上传提示遮罩：仅当文件被拖入聊天区时显示，不拦截 drop 事件 -->
        <div v-if="staged.dragging" class="drop-overlay" aria-hidden="true">
          <div class="drop-card">
            <svg viewBox="0 0 40 40" width="40" height="40">
              <path d="M20 26V10m0 0l-6 6m6-6l6 6" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" />
              <path d="M8 26v4a2 2 0 0 0 2 2h20a2 2 0 0 0 2-2v-4" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" />
            </svg>
            <p class="drop-title">释放以发送</p>
            <p class="drop-sub">支持图片与常见文件格式</p>
          </div>
        </div>
      </section>

      <!-- 联系人信息面板：类名切换驱动 CSS 过渡，实现淡入淡出 + 滑入滑出 -->
      <div class="profile-wrap" :class="{ show: showProfile }">
        <div class="divider-col"></div>

        <!-- 联系人信息面板：按单聊/群聊动态切换；:key 保证切会话时局部编辑态复位 -->
        <aside class="profile">
          <SingleProfilePanel
            v-if="!isGroup"
            :key="'s-' + (currentContact.id || 'none')"
            :meta="contactMeta"
            :friend-info="currentFriendInfo"
            :remark="currentRemark"
            :remark-saving="remarkSaving"
            v-model:mute-dnd="muteDnd"
            :is-no-persist="isNoPersist"
            @send-message="sendMessageAction"
            @open-search="openSearchHistory"
            @toggle-no-persist="toggleNoPersist"
            @save-remark="saveRemark"
          />
          <GroupProfilePanel
            v-else
            :key="'g-' + (currentContact.id || 'none')"
            :gp="gp"
            v-model:mute-dnd="muteDnd"
            :is-no-persist="isNoPersist"
            @open-search="openSearchHistory"
            @toggle-no-persist="toggleNoPersist"
          />
        </aside>
      </div>
    </main>

    <!-- 邀请成员弹框 -->
    <InviteMembersModal
      v-if="gp.showInviteModal"
      :target-id="currentContact.targetId"
      :member-uids="gp.liveGroupMembers.map((m) => m.uid)"
      @close="gp.showInviteModal = false"
      @invited="gp.onInvited()"
    />

    <!-- 退出群聊二次确认弹窗 -->
    <LeaveGroupConfirm
      v-if="gp.showLeaveConfirm"
      :group-name="currentContact.name"
      :leaving="gp.leavingGroup"
      @confirm="gp.confirmLeaveGroup()"
      @cancel="gp.showLeaveConfirm = false"
    />

    <!-- 群设置弹窗：群名/群公告（管理员可编辑，普通成员只读） -->
    <GroupSettingsModal
      v-if="gp.showGroupSettings"
      :group-uid="currentContact.targetId"
      :initial-name="currentContact.name"
      :initial-announcement="gp.groupInfo.announcement || ''"
      :is-admin="gp.isGroupAdmin"
      @close="gp.showGroupSettings = false"
      @saved="gp.onGroupSettingsSaved"
      @failed="gp.onGroupSettingsFailed"
    />

    <!-- 查找聊天记录弹窗：通过父组件传入的 showSearchHistory 控制；仅搜当前会话 -->
    <SearchChatHistoryModal
      v-if="showSearchHistory"
      :conversations="conversations"
      :conv-id="realConvMap[activeId] || currentContact.convId || 'none'"
      @close="closeSearchHistory"
      @jump="jumpToMessage"
    />

    <!-- 创建群聊弹窗：会话列表“+”菜单入口 -->
    <CreateGroupModal
      v-if="showCreateGroup"
      :friends="createGroupFriends"
      @close="showCreateGroup = false"
      @created="onPlusGroupCreated"
    />

    <!-- 语音通话面板：全局单例，状态机驱动显隐（api/call.js） -->
    <CallWindow />

    <!-- 轻量顶部 toast：群设置保存成功/失败提示 -->
    <Teleport to="body">
      <div v-if="toastState" class="wc-toast" :class="toastState.type" role="status">
        <span class="wc-toast-dot" aria-hidden="true"></span>
        <span class="wc-toast-text">{{ toastState.text }}</span>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.window {
  width: 100%;
  height: 100%;
  background: var(--im-surface);
  font-family: var(--im-font-family);
  color: var(--im-text-title);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* 主体 */
.body {
  flex: 1;
  display: flex;
  min-height: 0;
}

/* 会话列表 */
.conv-list {
  width: 300px;
  background: var(--im-surface);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
}

.search-bar {
  height: 56px;
  padding: 0 12px;
  display: flex;
  align-items: center;
  gap: 8px;
}

/* ===== 搜索框旁“+”按钮与下拉菜单 ===== */
.plus-wrap {
  position: relative;
  flex-shrink: 0;
}

.plus-btn {
  width: 36px;
  height: 36px;
  border: none;
  border-radius: 8px;
  background: var(--im-surface-2);
  color: var(--im-text-secondary);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background-color 0.15s ease, color 0.15s ease;
}

.plus-btn:hover {
  background: var(--im-hover-gray);
}

/* 菜单展开态：品牌蓝强调 */
.plus-btn.active {
  background: var(--im-primary);
  color: #fff;
}

.plus-menu {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  z-index: 1200;
  min-width: 148px;
  padding: 6px;
  background: var(--im-surface);
  border-radius: 10px;
  box-shadow: 0 8px 28px rgba(0, 0, 0, 0.16);
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.plus-menu-item {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 40px;
  padding: 0 10px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--im-text-title);
  font-family: inherit;
  font-size: 0.929rem;
  text-align: left;
  cursor: pointer;
  white-space: nowrap;
}

.plus-menu-item:hover {
  background: var(--im-surface-2);
}

.plus-menu-item svg {
  color: var(--im-text-secondary);
  flex-shrink: 0;
}

.search-box {
  flex: 1;
  /* flex 项默认 min-width:auto 不会缩小到内容固有宽以下，
     会把右侧“+”按钮挤出 300px 侧栏被遮挡，必须置 0 */
  min-width: 0;
  height: 36px;
  padding: 0 12px;
  background: var(--im-surface-2);
  border-radius: 8px;
  border: 1px solid transparent;
  display: flex;
  align-items: center;
  gap: 8px;
  transition: border-color 0.15s ease, background-color 0.15s ease;
}

.search-box:focus-within {
  border-color: var(--im-primary);
  background: var(--im-surface);
}

.search-icon {
  flex-shrink: 0;
  color: var(--im-text-secondary);
}

.search-input {
  flex: 1;
  min-width: 0;
  border: none;
  background: transparent;
  outline: none;
  font-family: inherit;
  font-size: 0.929rem;
  color: var(--im-text-title);
}

.search-input::placeholder {
  color: var(--im-text-muted);
}

.search-clear {
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  background: transparent;
  border: none;
  border-radius: 999px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background-color 0.12s ease;
}

.search-clear:hover {
  background: var(--im-hover-gray);
}

/* 搜索无结果占位 */
.conv-empty {
  padding: 32px 16px;
  text-align: center;
  color: var(--im-text-muted);
}

.conv-empty-title {
  margin: 0 0 4px;
  font-size: 0.929rem;
  font-weight: 600;
  color: var(--im-text-secondary);
}

.conv-empty-sub {
  margin: 0;
  font-size: 0.786rem;
}

.list-header {
  position: relative;
  height: 32px;
  padding: 0 12px;
  display: flex;
  align-items: center;
  justify-content: flex-start; /* 标题靠左 */
}

.list-title {
  font-size: 0.929rem;
  font-weight: 700;
  color: var(--im-text-title);
}

.list-scroll {
  flex: 1;
  overflow-y: auto;
  padding: 0 4px 8px;
}

.list-scroll::-webkit-scrollbar {
  width: 6px;
}

.list-scroll::-webkit-scrollbar-thumb {
  background: transparent;
  border-radius: 3px;
}

.list-scroll:hover::-webkit-scrollbar-thumb {
  background: rgba(0, 0, 0, 0.15);
}

.conv-item {
  height: 64px;
  padding: 0 6px;
  display: flex;
  align-items: center;
  gap: 12px;
  border-radius: 8px;
  cursor: pointer;
  margin-bottom: 2px;
  outline: none;
  -webkit-tap-highlight-color: transparent;
  transition: background-color 0.15s ease;
  user-select: none;
  -webkit-user-select: none;
}

.conv-item:hover {
  background: var(--im-surface-2);
}

.conv-item:focus-visible {
  box-shadow: 0 0 0 2px var(--im-primary) inset;
}

/* 选中态：淡品牌蓝背景，比浅灰深但不过于浓烈 */
.conv-item.active {
  background: var(--im-selected);
}

/* 选中态文字：淡蓝底上保持深色文字可读 */
.conv-item.active .conv-name {
  color: var(--im-text-title);
}

.conv-item.active .conv-last {
  color: var(--im-text-secondary);
}

.conv-item.active .conv-time {
  color: var(--im-text-muted);
}

/* 选中态在线点：淡蓝底上描边仍用白色即可见 */
.conv-item.active .online-dot {
  border-color: var(--im-selected-border);
}

/* 触屏设备：无 hover，直接靠 active 高亮，按压缩放反馈 */
@media (hover: none) {
  .conv-item:active {
    background: var(--im-hover-gray-active);
    transform: scale(0.985);
  }
  .conv-item {
    transition: background-color 0.12s ease, transform 0.12s ease;
  }
}

.avatar {
  width: 40px;
  height: 40px;
  border-radius: 999px;
  position: relative;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 1rem;
  font-weight: 500;
}

.avatar.small {
  width: 32px;
  height: 32px;
  font-size: 0.929rem;
}

.online-dot {
  position: absolute;
  right: -2px;
  bottom: -2px;
  width: 10px;
  height: 10px;
  background: var(--im-online);
  border: 2px solid var(--im-surface);
  border-radius: 999px;
}

.conv-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.conv-name {
  font-size: 1rem;
  font-weight: 700;
  color: var(--im-text-title);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.conv-last {
  font-size: 0.857rem;
  color: var(--im-text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.conv-meta {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
  width: 44px;
}

.conv-time {
  font-size: 0.786rem;
  color: var(--im-text-muted);
}

.unread-badge {
  min-width: 20px;
  height: 20px;
  padding: 0 6px;  /* 对称内边距，保持圆形 */
  background: var(--im-danger);
  color: #fff;
  border-radius: 999px;
  font-size: 0.786rem;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  line-height: 1;
  /* 白色描边外环 + 红色阴影，让圆形气泡更醒目 */
  box-shadow: 0 0 0 1.5px #fff, 0 2px 4px rgba(240, 69, 69, 0.25);
}

/* 选中态：未读气泡改为品牌蓝（与选中态呼应） */
.conv-item.active .unread-badge {
  background: var(--im-primary);
  color: #fff;
  box-shadow: 0 0 0 1.5px var(--im-primary), 0 2px 4px rgba(37, 99, 235, 0.25);
}

/* 列分隔线 */
.divider-col {
  width: 1px;
  background: var(--im-border);
  flex-shrink: 0;
}

/* 聊天区 */
.chat {
  flex: 1;
  min-width: 0;
  background: var(--im-chat-bg);
  display: flex;
  flex-direction: column;
  position: relative; /* 回到底部按钮定位基准 */
}

.chat-header {
  height: 60px;
  background: var(--im-surface);
  padding: 12px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
}

.chat-header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.contact-name {
  font-size: 1.071rem;
  font-weight: 700;
  color: var(--im-text-title);
}

/* WS 连接状态：绝对定位在"会话"标题行水平居中 */
.ws-status {
  position: absolute;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 9px;
  border-radius: 999px;
  font-size: 0.714rem;
  font-weight: 500;
  white-space: nowrap;
}

.ws-status-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
}

.ws-status.is-connected {
  background: rgba(16, 185, 129, 0.12);
  color: #10b981;
}
.ws-status.is-connected .ws-status-dot {
  background: #10b981;
}

.ws-status.is-connecting {
  background: rgba(245, 158, 11, 0.12);
  color: #f59e0b;
}
.ws-status.is-connecting .ws-status-dot {
  background: #f59e0b;
  animation: wsPulse 1.2s infinite;
}

.ws-status.is-disconnected {
  background: rgba(239, 68, 68, 0.12);
  color: #ef4444;
}
.ws-status.is-disconnected .ws-status-dot {
  background: #ef4444;
}

@keyframes wsPulse {
  0%, 100% {
    opacity: 1;
  }
  50% {
    opacity: 0.3;
  }
}

.chat-header-right {
  display: flex;
  align-items: center;
  gap: 4px;
}

.circle-btn {
  width: 40px;
  height: 40px;
  background: transparent;
  border: none;
  border-radius: 999px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  color: var(--im-text-secondary);
}

/* 通话按钮不可用态（群聊/未选中会话）：置灰不可点 */
.circle-btn:disabled {
  color: var(--im-text-muted);
  opacity: 0.45;
  cursor: not-allowed;
}

.circle-btn:hover {
  background: var(--im-surface-2);
}

.circle-btn.active {
  background: var(--im-surface-2);
}

.chat-divider {
  height: 1px;
  background: var(--im-border);
  flex-shrink: 0;
}

/* 消息区 */
/* 消息区容器：占满头部与输入栏之间的空间，作为“你有新消息”胶囊的定位基准 */
.messages-area {
  position: relative;
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.messages {
  flex: 1;
  padding: 16px 24px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.messages::-webkit-scrollbar {
  width: 6px;
}

.messages::-webkit-scrollbar-thumb {
  background: transparent;
  border-radius: 3px;
}

.messages:hover::-webkit-scrollbar-thumb {
  background: rgba(0, 0, 0, 0.15);
}

/* 切换会话时消息区平滑过渡（淡出/淡入，避免闪烁） */
.messages.leaving {
  opacity: 0;
  transform: translateY(6px);
}

.messages.entering {
  opacity: 0;
  transform: translateY(6px);
  animation: msgIn 0.22s ease forwards;
}

@keyframes msgIn {
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.messages {
  transition: opacity 0.16s ease, transform 0.16s ease;
}

/* 加载历史消息指示器 */
.loading-more {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 12px 0;
  font-size: 0.857rem;
  color: var(--im-text-muted);
}

.spinner {
  width: 16px;
  height: 16px;
  border: 2px solid var(--im-border);
  border-top-color: var(--im-primary);
  border-radius: 999px;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

/* 空会话占位 */
.empty-chat {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 40px 24px;
  text-align: center;
}

.empty-avatar.welcome {
  background: var(--im-soft-blue);
  color: var(--im-primary);
}

.empty-avatar {
  width: 72px;
  height: 72px;
  border-radius: 999px;
  color: #fff;
  font-size: 1.857rem;
  font-weight: 500;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 8px;
}

.empty-title {
  margin: 0;
  font-size: 1.143rem;
  font-weight: 700;
  color: var(--im-text-title);
}

.empty-sub {
  margin: 0;
  font-size: 0.929rem;
  color: var(--im-text-muted);
}

/* 每天分组标题：居中、独立成行，与上下消息有明显间距 */
.day-divider {
  display: flex;
  justify-content: center;
  align-items: center;
  margin: 6px 0 14px;
  position: relative;
}

.day-divider-text {
  padding: 3px 14px;
  background: var(--im-surface-2);
  border-radius: 999px;
  font-size: 0.786rem;
  color: var(--im-text-secondary);
  z-index: 1;
}

/* 组内时间分隔（间隔超过 5 分钟）：独立占一行、居中、更轻量 */
.time-pill {
  display: flex;
  justify-content: center;
}

.time-text {
  padding: 2px 10px;
  background: transparent;
  border-radius: 999px;
  font-size: 0.714rem;
  color: var(--im-text-muted);
}

/* 系统消息：居中灰色胶囊（类似时间展示） */
.system-msg {
  width: 100%;
  display: flex;
  justify-content: center;
  padding: 4px 0;
  margin: 4px 0;
  user-select: none;
}

.system-msg-inner {
  padding: 3px 12px;
  background: var(--im-surface-2);
  border-radius: 999px;
  font-size: 0.786rem;
  line-height: 1.4;
  color: var(--im-text-secondary);
  max-width: 80%;
  text-align: center;
}

.message-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.message-row.in {
  justify-content: flex-start;
}

.message-row.out {
  justify-content: flex-end;
}

/* 对方消息容器：容纳昵称 + 气泡，最长占消息区 60% */
.in-col {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  max-width: 60%;
  min-width: 0;
}

/* 群聊对方消息的发送者昵称 */
.sender-name {
  font-size: 0.786rem;
  color: var(--im-text-secondary);
  margin-bottom: 3px;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 我方消息容器：最大宽度为消息行 70%，内部靠右；气泡按内容撑开，不再被父容器递归压缩 */
.out-col {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 6px;
  max-width: 70%;
  min-width: 0;
  flex: 0 1 auto;
}

.send-status {
  display: flex;
  align-items: center;
  gap: 4px;
}

.status-text-out {
  font-size: 0.786rem;
  color: #bfdbfe;
}

/* ===== 微信风格：消息操作菜单 ===== */
.msg-menu {
  position: fixed;
  z-index: 1000;
  min-width: 120px;
  background: var(--im-surface);
  border: 1px solid var(--im-border);
  border-radius: 10px;
  box-shadow: 0 6px 24px rgba(0, 0, 0, 0.12);
  padding: 4px;
  animation: msgMenuIn 0.12s ease;
}

@keyframes msgMenuIn {
  from {
    opacity: 0;
    transform: scale(0.96);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}

.msg-menu-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 9px 12px;
  border: none;
  background: transparent;
  border-radius: 7px;
  color: var(--im-text-title);
  font-size: 0.929rem;
  cursor: pointer;
  text-align: left;
}

.msg-menu-item:hover {
  background: var(--im-surface-2);
}

.msg-menu-item.danger {
  color: var(--im-danger, #ef4444);
}

/* 撤回消息提示行：整行撑满并左右居中，灰色文字 + 可选蓝色"重新编辑"链接 */
.recall-msg {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 8px 16px;
  color: #8a8f99;
  font-size: 13px;
  line-height: 1.5;
  text-align: center;
}

.recall-text {
  white-space: nowrap;
}

.recall-edit-link {
  background: none;
  border: none;
  padding: 0;
  color: #2563eb;
  cursor: pointer;
  font-size: 13px;
  font-family: inherit;
  text-decoration: none;
}

.recall-edit-link:hover {
  text-decoration: underline;
}

/* ===== 微信风格“你有新消息”胶囊：浏览历史时收到新消息浮出，点击回到最新消息 ===== */
.new-msg-pill {
  position: absolute;
  bottom: 14px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 120;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 7px 14px;
  border: none;
  border-radius: 999px;
  background: var(--im-surface);
  color: var(--im-text-secondary);
  font-family: inherit;
  font-size: 0.857rem;
  font-weight: 500;
  box-shadow: 0 3px 12px rgba(0, 0, 0, 0.14);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.new-msg-pill:hover {
  background: var(--im-surface-2);
  color: var(--im-primary);
}

.new-msg-pill:active {
  transform: translateX(-50%) scale(0.97);
}

.scroll-btn-enter-active,
.scroll-btn-leave-active {
  transition: opacity 0.2s ease, transform 0.2s ease;
}

.scroll-btn-enter-from,
.scroll-btn-leave-to {
  opacity: 0;
  transform: translateX(-50%) translateY(10px);
}

/* 查找聊天记录跳转高亮：临时给目标消息气泡加品牌蓝外环 + 浅色背景闪动
   （气泡为 MessageBubble 组件根元素，scoped 下父规则仍可命中子组件根） */
.message-row.highlighted .bubble {
  animation: highlightPulse 2s ease;
}

@keyframes highlightPulse {
  0%   { box-shadow: 0 0 0 0 rgba(37, 99, 235, 0.55), 0 1px 3px rgba(0, 0, 0, 0.06); }
  20%  { box-shadow: 0 0 0 8px rgba(37, 99, 235, 0.18), 0 1px 3px rgba(0, 0, 0, 0.06); }
  100% { box-shadow: 0 0 0 0 rgba(37, 99, 235, 0), 0 1px 3px rgba(0, 0, 0, 0.06); }
}

/* 高亮气泡用品牌蓝字色短暂强调 */
.message-row.highlighted .bubble.in {
  color: var(--im-primary);
}

/* 输入栏 */
.input-bar {
  position: relative;
  background: var(--im-surface);
  padding: 12px;
  display: flex;
  flex-direction: column;
  /* 工具按钮与输入框间距收紧：gap + 拖动手柄高度合计 8px */
  gap: 4px;
  flex-shrink: 0;
}

.tools-row {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 36px;
}

.tool-btn {
  width: 32px;
  height: 32px;
  background: transparent;
  border: none;
  border-radius: 999px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  color: var(--im-text-secondary);
}

.tool-btn:hover {
  background: var(--im-surface-2);
}

/* 语音录音中：图标变红提示正在录制 */
.tool-btn.recording {
  color: var(--im-danger, #ef4444);
}

.tool-btn.active {
  color: var(--im-primary);
}

/* 表情面板进出场过渡（面板本体在 EmojiPanel 组件内） */
.emoji-panel-enter-active,
.emoji-panel-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}

.emoji-panel-enter-from,
.emoji-panel-leave-to {
  opacity: 0;
  transform: translateY(8px);
}

/* 输入框拖动手柄：默认隐形，hover/拖动时显示灰色小横条提示可拖（高度已收紧，靠负 margin 保持可拖区域） */
.input-resize-handle {
  height: 4px;
  /* 仅向输入框一侧借 3px 扩大可拖区域，避免遮挡上方工具按钮点击 */
  margin-bottom: -3px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: row-resize;
  user-select: none;
}

.input-resize-handle .handle-bar {
  width: 36px;
  height: 3px;
  border-radius: 2px;
  background: transparent;
  transition: background-color 0.15s;
}

.input-resize-handle:hover .handle-bar {
  background: var(--im-border);
}

.input-box {
  border: 1px solid var(--im-border);
  background: var(--im-surface-2);
  border-radius: 10px;
  padding: 12px 14px;
  /* 默认两行高（内层 textarea 44px + 上下内边距）；拖动后由行内 height 接管 */
  min-height: 68px;
  max-height: 280px;
  box-sizing: border-box;
  transition: border-color 0.2s;
}

.input-box:focus-within {
  border-color: var(--im-primary);
}

.input-field {
  display: block;
  width: 100%;
  border: none;
  background: transparent;
  outline: none;
  resize: none;
  overflow-y: hidden;
  font-family: inherit;
  font-size: 1rem;
  line-height: 22px;
  color: var(--im-text-title);
  /* 默认两行高度，由 autoResizeInput 随内容动态增高（上限 6 行） */
  height: 44px;
  min-height: 44px;
}

.input-field::placeholder {
  color: var(--im-text-muted);
}

.input-field::-webkit-scrollbar {
  width: 4px;
}

.input-field::-webkit-scrollbar-thumb {
  background: var(--im-border);
  border-radius: 2px;
}

.footer-row {
  height: 28px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.lock-hint {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 0.786rem;
  color: var(--im-text-muted);
}

.shortcut {
  font-size: 0.786rem;
  color: var(--im-text-muted);
}

.send-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 16px;
  height: 32px;
  background: var(--im-primary);
  color: #fff;
  border: none;
  border-radius: 8px;
  font-size: 0.929rem;
  font-weight: 500;
  cursor: pointer;
}

.send-btn:hover {
  background: var(--im-primary-hover);
}

.send-btn:disabled {
  background: var(--im-border);
  color: rgba(255, 255, 255, 0.7);
  cursor: not-allowed;
}

.send-btn:disabled:hover {
  background: var(--im-border);
}

/* ===== 图片预览遮罩 ===== */
.image-preview {
  position: fixed;
  inset: 0;
  z-index: 2000;
  background: rgba(0, 0, 0, 0.75);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: zoom-out;
}

.image-preview-inner {
  position: relative;
  max-width: 82vw;
  max-height: 88vh;
  cursor: default;
}

.image-preview-img {
  max-width: 82vw;
  max-height: 82vh;
  object-fit: contain;
  border-radius: 6px;
  box-shadow: 0 12px 48px rgba(0, 0, 0, 0.5);
}

.image-preview-footer {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  margin-top: 12px;
}

.image-preview-name {
  color: #fff;
  font-size: 0.85rem;
  max-width: 60vw;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.image-preview-close {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  border: none;
  background: rgba(255, 255, 255, 0.18);
  color: #fff;
  font-size: 1.2rem;
  line-height: 1;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
}

.image-preview-close:hover {
  background: rgba(255, 255, 255, 0.3);
}

/* ===== 视频播放遮罩（复用图片预览的 footer/关闭按钮样式） ===== */
.video-preview {
  position: fixed;
  inset: 0;
  z-index: 2000;
  background: rgba(0, 0, 0, 0.82);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: zoom-out;
}

.video-preview-inner {
  position: relative;
  max-width: 82vw;
  max-height: 88vh;
  cursor: default;
}

.video-preview-player {
  max-width: 82vw;
  max-height: 82vh;
  border-radius: 6px;
  background: #000;
  box-shadow: 0 12px 48px rgba(0, 0, 0, 0.5);
  outline: none;
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.18s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}

/* 联系人信息面板：外层容器，负责宽度展开/收起 + 淡入淡出过渡 */
.profile-wrap {
  display: flex;
  flex-direction: row;
  width: 0;
  max-width: 301px;
  flex-shrink: 0;
  overflow: hidden;
  opacity: 0;
  transition: width 0.28s cubic-bezier(0.4, 0, 0.2, 1), opacity 0.28s ease;
  pointer-events: none;
}

.profile-wrap .divider-col {
  flex-shrink: 0;
}

/* 展开态：面板滑入并淡入 */
.profile-wrap.show {
  width: 301px;
  opacity: 1;
  pointer-events: auto;
}

/* 联系人信息面板（单聊/群聊布局分别在 Single/GroupProfilePanel 组件内） */
.profile {
  width: 300px;
  background: var(--im-surface);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  min-height: 0; /* 允许 flex 子项滚动 */
  overflow-y: auto;
  overscroll-behavior: contain;
}

.profile::-webkit-scrollbar {
  width: 6px;
}

.profile::-webkit-scrollbar-thumb {
  background: transparent;
  border-radius: 3px;
}

.profile:hover::-webkit-scrollbar-thumb {
  background: rgba(0, 0, 0, 0.15);
}

/* ===== 拖拽上传遮罩：蓝色晕染底 + 虚线卡片提示释放发送 ===== */
.drop-overlay {
  position: absolute;
  inset: 0;
  z-index: 50;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(37, 99, 235, 0.08);
  backdrop-filter: blur(2px);
  animation: dropOverlayIn 0.15s ease;
  /* 不拦截拖放事件，drop 仍落在 .chat 上 */
  pointer-events: none;
}

@keyframes dropOverlayIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

.drop-card {
  padding: 28px 40px;
  border: 2px dashed var(--im-primary);
  border-radius: 14px;
  background: var(--im-surface);
  color: var(--im-primary);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  box-shadow: 0 12px 36px rgba(37, 99, 235, 0.18);
}

.drop-title {
  margin: 0;
  font-size: 1rem;
  font-weight: 600;
  color: var(--im-text-title);
}

.drop-sub {
  margin: 0;
  font-size: 0.857rem;
  color: var(--im-text-muted);
}

/* ===== 暂存附件区：发送前的图片/文件预览（输入框上方） ===== */
.staged-files {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  /* 顶部预留 8px：移除按钮绝对定位 top:-6px 不被上方元素遮挡 */
  padding: 8px 0 2px;
}

.staged-item {
  position: relative;
}

.staged-thumb {
  width: 64px;
  height: 64px;
  object-fit: cover;
  border-radius: 8px;
  border: 1px solid var(--im-border);
  display: block;
}

/* 文件卡片：横向布局（名称/大小），高度随内容自适应。
   历史 bug：固定 64px 高 + 纵向堆叠导致内容溢出卡片被输入区遮挡 */
.staged-file-card {
  width: 180px;
  box-sizing: border-box;
  padding: 8px 12px;
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 8px;
  background: var(--im-surface-2);
  border: 1px solid var(--im-border);
  border-radius: 8px;
  color: var(--im-primary);
}

.staged-file-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.staged-file-name {
  font-size: 0.786rem;
  line-height: 15px;
  color: var(--im-text-title);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.staged-file-size {
  font-size: 0.714rem;
  line-height: 13px;
  color: var(--im-text-muted);
}

.staged-remove {
  position: absolute;
  top: -6px;
  right: -6px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  border: none;
  background: rgba(31, 35, 41, 0.72);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  padding: 0;
}

.staged-remove:hover {
  background: var(--im-danger, #ef4444);
}

/* ---- 触屏 / 移动端优化 ---- */
@media (hover: none) and (pointer: coarse) {
  .list-scroll,
  .messages {
    -webkit-overflow-scrolling: touch; /* iOS 惯性滚动 */
    scroll-behavior: smooth;
  }

  /* 加大触摸目标，便于手指点击 */
  .conv-item {
    min-height: 72px;
  }

  .tool-btn,
  .circle-btn,
  .ctrl {
    width: 44px;
    height: 44px;
  }

  .send-btn {
    min-height: 40px;
    padding: 0 20px;
  }

  /* 隐藏键盘触发的点击高亮 */
  * {
    -webkit-tap-highlight-color: transparent;
  }
}

/* 键盘操作时显示可见焦点环（无障碍） */
.list-scroll:focus-visible {
  outline: 2px solid var(--im-primary);
  outline-offset: -2px;
  border-radius: 8px;
}

/* 减少动画偏好：关闭非必要过渡 */
@media (prefers-reduced-motion: reduce) {
  .messages,
  .conv-item,
  .profile-wrap {
    transition: none !important;
    animation: none !important;
  }
  .messages.leaving,
  .messages.entering {
    opacity: 1;
    transform: none;
  }
}

/* ===== 轻量顶部 toast（与设置窗口同款） ===== */
.wc-toast {
  position: fixed;
  top: 28px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 3000;
  display: flex;
  align-items: center;
  gap: 8px;
  max-width: min(560px, calc(100vw - 48px));
  padding: 10px 18px;
  border-radius: 10px;
  background: #1f2329;
  color: #fff;
  font-size: 0.9rem;
  box-shadow: 0 8px 28px rgba(0, 0, 0, 0.24);
  animation: mw-toast-in 0.22s ease;
}

.wc-toast-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex: none;
  background: #8a919f;
}

.wc-toast.success .wc-toast-dot { background: #22c55e; }
.wc-toast.error .wc-toast-dot { background: #ef4444; }
.wc-toast.info .wc-toast-dot { background: #3b82f6; }

.wc-toast-text {
  word-break: break-all;
  line-height: 1.45;
}

@keyframes mw-toast-in {
  from { opacity: 0; transform: translate(-50%, -10px); }
  to { opacity: 1; transform: translate(-50%, 0); }
}
</style>
