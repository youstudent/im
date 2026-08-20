// 微信风格：消息操作菜单（右键 / 点击）——复制、引用与撤回
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { messageApi } from '../api/message'

export function useMessageMenu(ctx) {
  const { realConvMap, activeId, message, inputFieldEl, autoResizeInput, refreshConvPreview, startQuote } = ctx

  const msgMenu = ref(null) // { x, y } | null
  const menuMsg = ref(null) // 当前操作的消息

  // 打开消息操作菜单
  function openMsgMenu(e, msg) {
    // 阻止冒泡到 document 的 click 监听（避免菜单立即关闭）
    if (e && e.stopPropagation) e.stopPropagation()
    if (e && e.preventDefault) e.preventDefault()
    // 定位在消息气泡附近，避免超出消息区
    const msgArea = document.querySelector('.messages')
    let x = (e.clientX || 0) - 140
    let y = e.clientY || 0
    if (msgArea) {
      const areaRect = msgArea.getBoundingClientRect()
      x = Math.min(Math.max(x, areaRect.left + 8), areaRect.right - 150)
      y = Math.min(y, areaRect.bottom - 90)
      y = Math.max(y, areaRect.top + 8)
    }
    menuMsg.value = msg
    msgMenu.value = { x, y }
  }

  // 复制消息文本
  async function copyMsgText() {
    if (!menuMsg.value) return
    try {
      await navigator.clipboard.writeText(menuMsg.value.text)
    } catch {
      // clipboard 不可用时回退
      const ta = document.createElement('textarea')
      ta.value = menuMsg.value.text
      document.body.appendChild(ta)
      ta.select()
      try {
        document.execCommand('copy')
      } finally {
        document.body.removeChild(ta)
      }
    }
    closeMsgMenu()
  }

  // 是否可撤回：仅自己发送、未撤回、且发送不超过 2 分钟
  function canRecall(msg) {
    if (!msg || msg.type !== 'out' || msg.status === 1) return false
    const created = Number(msg.createdAt) * 1000
    if (!created) return false
    return Date.now() - created <= 2 * 60 * 1000
  }

  // 是否可引用：系统消息与已撤回消息不可引用（收发两侧均可引用其余消息）
  function canQuote(msg) {
    if (!msg || msg.isSystem || msg.status === 1) return false
    return true
  }

  // 引用：构建被引消息快照交给输入区（展示引用条，发送时携带 extra.quote）
  function quoteMsg() {
    const msg = menuMsg.value
    closeMsgMenu()
    if (!canQuote(msg) || typeof startQuote !== 'function') return
    startQuote(msg)
  }

  // 撤回消息：调用后端接口（2 分钟内），成功后本地把消息标记为已撤回。
  // 注意：保留 msg.text / msg.extra 原文，用于支持"重新编辑"——撤回提示行由模板按 status===1 渲染，不显示原文。
  async function recallMessage() {
    const msg = menuMsg.value
    if (!msg) return
    closeMsgMenu()
    const convId = realConvMap.value[activeId.value]
    if (!convId || !msg.id) return
    // 撤回前做一次前端校验，减少无效请求
    if (!canRecall(msg)) {
      alert('已超过 2 分钟，无法撤回')
      return
    }
    try {
      await messageApi.recall(convId, msg.id)
      // 本地标记为已撤回（text/extra 保留原文，供"重新编辑"使用；视觉上由模板渲染为居中提示行）
      msg.status = 1
      msg.readAt = ''
      // 若撤回的是会话最后一条，刷新会话列表最后消息
      refreshConvPreview(activeId.value)
    } catch (e) {
      alert(e.message || '撤回失败')
    }
  }

  // 重新编辑：把撤回消息的原文填回输入框
  function recallEdit(msg) {
    if (!msg) return
    message.value = String(msg.text || '')
    if (inputFieldEl.value) {
      inputFieldEl.value.focus()
      autoResizeInput()
    }
  }

  // 关闭消息菜单
  function closeMsgMenu() {
    msgMenu.value = null
    menuMsg.value = null
  }

  // 点击页面其它位置 / 滚动时关闭菜单
  onMounted(() => {
    document.addEventListener('click', closeMsgMenu)
    document.addEventListener('scroll', closeMsgMenu, true)
  })
  onBeforeUnmount(() => {
    document.removeEventListener('click', closeMsgMenu)
    document.removeEventListener('scroll', closeMsgMenu, true)
  })

  return { msgMenu, menuMsg, openMsgMenu, copyMsgText, canRecall, recallMessage, recallEdit, closeMsgMenu, canQuote, quoteMsg }
}
