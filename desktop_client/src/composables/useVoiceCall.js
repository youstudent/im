// 语音/视频通话入口接线：通话状态机由 api/call.js 全局维护，此处负责入口可用性与通话记录落库
import { computed, watch } from 'vue'
import { callState, startCall, onCallEnded } from '../api/call'
import { wsClient } from '../api/ws'
import { localdb } from '../api/localdb'
import { MSG_TYPE, formatMsgTime, formatConvTime, formatCallDuration } from '../utils/format'

export function useVoiceCall(ctx) {
  const {
    currentContact, hasActiveContact, isGroupChat, conversations, activeId,
    realConvMap, noPersistSet, showToast, scrollToBottom, reorderConversations,
  } = ctx

  // 语音通话入口可用性：仅单聊可拨打（群通话本期不支持）
  const canVoiceCall = computed(() => hasActiveContact.value && !isGroupChat.value)

  function startVoiceCall() {
    if (!hasActiveContact.value) return
    if (isGroupChat.value) {
      showToast('暂不支持群语音通话', 'info')
      return
    }
    if (callState.status !== 'idle') {
      showToast('当前有通话正在进行', 'info')
      return
    }
    if (!wsClient.isConnected()) {
      showToast('服务器未连接，无法发起通话', 'error')
      return
    }
    startCall({
      uid: currentContact.value.targetId,
      name: currentContact.value.name,
      avatar: currentContact.value.avatar,
      color: currentContact.value.color,
      convId: currentContact.value.id,
    })
  }

  // 视频通话：本期占位入口，后续迭代开放
  function startVideoCall() {
    showToast('视频通话暂未开放', 'info')
  }

  // 来电时从会话列表补全对方展示信息（昵称/头像/会话定位），无匹配时由 UI 展示默认占位
  watch(
    () => callState.status,
    (st) => {
      if (st !== 'incoming' || !callState.peer || !callState.peer.uid) return
      const conv = conversations.value.find(
        (c) => c.type !== 'group' && String(c.targetId) === String(callState.peer.uid)
      )
      if (conv) {
        callState.peer = {
          ...callState.peer,
          name: conv.name,
          avatar: conv.avatar,
          color: conv.color,
          convId: conv.id,
        }
      }
    }
  )

  // 通话结束：插入通话记录（本地系统消息，不发送服务端，双端各自记录各自视角）
  onCallEnded((info) => insertCallLog(info))

  // 通话记录文案：按结果与方向（out 拨出方 / in 被叫方）区分视角
  function callLogText(info) {
    switch (info.result) {
      case 'completed':
        return `语音通话 ${formatCallDuration(info.duration)}`
      case 'missed':
        return info.direction === 'in' ? '未接语音通话' : '对方无应答'
      case 'declined':
        return info.direction === 'out' ? '对方已拒绝' : '已拒接语音通话'
      case 'cancelled':
        return info.direction === 'out' ? '已取消语音通话' : '对方取消了呼叫'
      case 'busy':
        return '对方正在通话中'
      case 'offline':
        return '对方不在线，呼叫未接通'
      default:
        return '语音通话未接通'
    }
  }

  function insertCallLog(info) {
    const convId = info.peer && info.peer.convId
    if (!convId) return
    const conv = conversations.value.find((c) => c.id === convId)
    if (!conv) return
    const createdAt = Math.floor(Date.now() / 1000)
    const text = callLogText(info)
    const msg = {
      id: `call-${info.callId}`,
      type: 'in', // 行样式占位；系统消息按 isSystem 居中灰字渲染
      msgType: MSG_TYPE.SYSTEM,
      isSystem: true,
      text,
      extra: { call: 'voice', result: info.result, duration: Number(info.duration) || 0 },
      status: 0,
      createdAt,
      time: formatMsgTime(createdAt),
      server: true,
    }
    conv.messages = conv.messages ?? []
    conv.messages.push(msg)
    // 会话列表摘要统一 [语音通话]（messagePreview 识别 extra.call）；
    // 发送者置 0：通话记录无发送者语义，群聊列表不加名称前缀
    conv.lastMessage = '[语音通话]'
    conv.lastSenderUid = 0
    conv.lastSenderName = ''
    conv.lastMentionMe = false
    conv.lastMsgTime = createdAt
    conv.time = formatConvTime(createdAt)
    if (String(activeId.value) === String(conv.id)) scrollToBottom()
    reorderConversations()
    // 本地落库：server_id 带 call- 前缀不会与雪花 ID 冲突；sender_uid=0 重载后仍渲染为系统消息
    const realConvId = realConvMap.value[conv.id]
    if (localdb.available() && realConvId && !noPersistSet.value.has(String(realConvId))) {
      localdb.messages.upsert([
        {
          server_id: `call-${info.callId}`,
          conv_id: String(realConvId),
          seq: 0,
          sender_uid: '0',
          sender_name: '',
          type: MSG_TYPE.SYSTEM,
          content: text,
          extra: JSON.stringify(msg.extra),
          status: 0,
          created_at: createdAt,
        },
      ])
      localdb.conversations.bump(String(realConvId), '[语音通话]', createdAt, '0', '')
    }
  }

  return { canVoiceCall, startVoiceCall, startVideoCall }
}
