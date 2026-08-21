// 微信风格：会话列表右键操作菜单 —— 置顶聊天 / 消息免打扰
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { messageApi } from '../api/message'
import { localdb } from '../api/localdb'

// ctx = { showToast(text, type), reorderConversations(), deleteConv(conv) }
export function useConvMenu(ctx) {
  const { showToast, reorderConversations, deleteConv } = ctx

  const convMenu = ref(null) // { x, y } | null
  const menuConv = ref(null) // 当前操作的会话项

  // 打开会话操作菜单（右键触发，定位在光标处并夹在会话列表可视区内）
  function openConvMenu(e, conv) {
    if (e && e.stopPropagation) e.stopPropagation()
    if (e && e.preventDefault) e.preventDefault()
    const listArea = document.querySelector('.conv-list')
    let x = e.clientX || 0
    let y = e.clientY || 0
    if (listArea) {
      const rect = listArea.getBoundingClientRect()
      x = Math.min(Math.max(x, rect.left + 4), rect.right - 150)
      y = Math.min(y, rect.bottom - 90)
      y = Math.max(y, rect.top + 4)
    }
    menuConv.value = conv
    convMenu.value = { x, y }
  }

  // 应用设置：本地即时生效（列表重排 + 落本地库），再同步服务端；失败回滚并提示。
  // 同时供右键菜单与资料面板免打扰开关复用（conv 为占位/空会话时忽略）
  async function applySettings(conv, patch) {
    if (!conv || !conv.id) return
    const prev = { pinned: conv.pinned, muted: conv.muted }
    Object.assign(conv, patch)
    reorderConversations()
    const convIdStr = conv.convId ? String(conv.convId) : ''
    if (convIdStr) localdb.conversations.setSettings(convIdStr, patch)
    // 占位会话（尚未与服务端建立）：仅本地生效，真实会话建立后由服务端默认值接管
    if (!conv.convId) return
    try {
      const body = {}
      if (patch.pinned != null) body.pinned = patch.pinned ? 1 : 0
      if (patch.muted != null) body.muted = patch.muted ? 1 : 0
      await messageApi.updateSettings(conv.convId, body)
    } catch (e) {
      // 回滚内存与本地库状态
      Object.assign(conv, prev)
      reorderConversations()
      if (convIdStr) localdb.conversations.setSettings(convIdStr, prev)
      showToast(e.message || '会话设置更新失败', 'error')
    }
  }

  // 置顶 / 取消置顶
  function togglePin() {
    const conv = menuConv.value
    if (!conv) return
    const next = !conv.pinned
    closeConvMenu()
    applySettings(conv, { pinned: next })
  }

  // 消息免打扰 / 取消免打扰
  function toggleMute() {
    const conv = menuConv.value
    if (!conv) return
    const next = !conv.muted
    closeConvMenu()
    applySettings(conv, { muted: next })
  }

  // 标记未读 / 取消标记（纯本地状态，不走服务端）：
  // 挂起时无未读也显示红点；取消时若确有未读一并清零（视为已读）
  function toggleMarkUnread() {
    const conv = menuConv.value
    if (!conv) return
    const next = !conv.markedUnread
    closeConvMenu()
    conv.markedUnread = next
    if (!next && conv.unread > 0) {
      conv.unread = 0
      if (conv.convId) localdb.conversations.setUnread(String(conv.convId), 0)
    }
    if (conv.convId) localdb.conversations.setMarkedUnread(String(conv.convId), next ? 1 : 0)
  }

  // 删除会话：打开自定义确认弹框（危险操作风格，替代原生 confirm），确认后由外部执行 deleteConv
  const showDeleteConfirm = ref(false)
  const deleting = ref(false)
  const pendingDelete = ref(null) // 待删除的会话项（弹框确认期间保留引用）

  function removeConversation() {
    const conv = menuConv.value
    if (!conv) return
    pendingDelete.value = conv
    closeConvMenu()
    showDeleteConfirm.value = true
  }

  // 确认删除：执行外部清理（本地列表/本地库 + 服务端视图行），成功即关闭弹框
  async function confirmDelete() {
    if (deleting.value || !pendingDelete.value) return
    deleting.value = true
    try {
      if (deleteConv) await deleteConv(pendingDelete.value)
      showDeleteConfirm.value = false
      pendingDelete.value = null
    } finally {
      deleting.value = false
    }
  }

  function cancelDelete() {
    showDeleteConfirm.value = false
    pendingDelete.value = null
  }

  function closeConvMenu() {
    convMenu.value = null
    menuConv.value = null
  }

  // 点击页面其它位置 / 滚动时关闭菜单
  onMounted(() => {
    document.addEventListener('click', closeConvMenu)
    document.addEventListener('scroll', closeConvMenu, true)
  })
  onBeforeUnmount(() => {
    document.removeEventListener('click', closeConvMenu)
    document.removeEventListener('scroll', closeConvMenu, true)
  })

  return { convMenu, menuConv, openConvMenu, togglePin, toggleMute, toggleMarkUnread, removeConversation, closeConvMenu, applySettings, showDeleteConfirm, deleting, pendingDelete, confirmDelete, cancelDelete }
}
