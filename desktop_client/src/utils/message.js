// 消息结构映射与摘要预览工具：服务端/本地库行 ↔ 面板消息结构，均为纯函数（依赖通过参数注入）
import { MSG_TYPE, SEND_TIMEOUT_MS, formatMsgTime, formatConvTime } from './format'
import { AUDIO_EXT_RE, VIDEO_EXT_RE } from './fileGuard'

// 消息类型占位文案（与后端 convPreview 保持一致）：1 文本 / 2 图片 / 3 文件 / 4 语音 / 5 视频 / 6 系统 / 7 合并转发
export function msgTypeLabel(type) {
  switch (type) {
    case MSG_TYPE.IMAGE: return '[图片]'
    case MSG_TYPE.FILE: return '[文件]'
    case MSG_TYPE.VOICE: return '[语音]'
    case MSG_TYPE.VIDEO: return '[视频]'
    case MSG_TYPE.SYSTEM: return '[系统消息]'
    case MSG_TYPE.MERGE: return '[合并转发]'
    default: return ''
  }
}

// 语音消息识别：type=4 原生语音，或以音频后缀发送的 FILE 消息（兼容存量录音）
export function isAudioMsg(m) {
  if (!m) return false
  const t = Number(m.msgType)
  if (t === MSG_TYPE.VOICE) return true
  return t === MSG_TYPE.FILE && AUDIO_EXT_RE.test(String(m.extra?.name || ''))
}

// 视频消息识别：以 FILE 类型发送且后缀可被 Chromium 原生解码；
// webm 也可能是录音产物：带时长或名称以"语音_"开头时按语音处理
export function isVideoMsg(m) {
  if (!m || Number(m.msgType) !== MSG_TYPE.FILE) return false
  const name = String(m.extra?.name || '')
  if (!VIDEO_EXT_RE.test(name)) return false
  if (/\.webm$/i.test(name) && (Number(m.extra?.duration) > 0 || /^语音_/.test(name))) return false
  return true
}

// 会话列表摘要预览：图片/文件消息显示类型占位，文本截断。
export function messagePreview(m) {
  if (!m) return ''
  // 语音通话记录：摘要统一 [语音通话]
  if (m.extra && m.extra.call === 'voice') return '[语音通话]'
  // 音频后缀的文件消息（录音产物）按语音展示，与气泡渲染保持一致
  if (isAudioMsg(m)) return '[语音]'
  // 视频后缀的文件消息展示 [视频]，与后端 convPreview 一致
  if (isVideoMsg(m)) return '[视频]'
  // 非文本类型显示类型占位，与后端 convPreview 一致；避免会话列表展示资源 URL
  if (m.msgType && m.msgType !== MSG_TYPE.TEXT) return msgTypeLabel(m.msgType)
  const text = m.text || ''
  return text.length > 50 ? text.slice(0, 50) + '…' : text
}

// 统一生成会话列表的最后消息预览：撤回消息显示"你/对方撤回了一条消息"，否则正常预览
export function convLastPreview(msg) {
  if (!msg) return ''
  if (msg.status === 1) {
    return msg.type === 'out' ? '你撤回了一条消息' : '对方撤回了一条消息'
  }
  return messagePreview(msg)
}

// extra 解析容错（本地库行/服务端消息共用）
export function parseExtra(extra) {
  if (!extra) return {}
  try {
    return typeof extra === 'string' ? JSON.parse(extra) : extra
  } catch {
    return {}
  }
}

// "xx 退出群聊" 系统消息仅群主可见（extra.owner_uid）：其他成员不渲染也不落本地库，
// 兼容历史加载/本地秒开/WS 推送各路径的原始行（extra 为字符串或对象）
export function isHiddenLeaveMsg(m, meUid) {
  if (!m || Number(m.type ?? m.msgType) !== 6) return false
  let extra = m.extra
  if (typeof extra === 'string') {
    if (!extra) return false
    try {
      extra = JSON.parse(extra)
    } catch {
      return false
    }
  }
  if (!extra || extra.kind !== 'group_leave') return false
  return String(extra.owner_uid ?? '') !== String(meUid)
}

// 服务端消息 → 本地库行（upsert 用，雪花 ID 字符串化）
export function toDbMessage(m, convId) {
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

// 会话行（网络/本地库字段同名）→ 列表项（展示资料取自联系人缓存）
export function toConvItem(c, contactMap) {
  const key = String(c.target_id)
  const info = contactMap.get(key) || {}
  const lastMsgTime = Number(c.last_msg_time) || 0
  return {
    id: `conv-${c.id}`,
    name: info.name || `用户 ${c.target_id}`,
    avatar: info.avatar || '?',
    color: info.color || '#64748b',
    online: false,
    type: Number(c.type) === 2 ? 'group' : info.type || null,
    lastMessage: c.last_msg || '',
    // 最后消息发送者（群聊列表拼 "发送者: 内容" 前缀，渲染时动态解析名称）；
    // 服务端 DTO 与本地库行同名字段，0/空表示系统/无
    lastSenderUid: Number(c.last_sender_uid) || 0,
    lastSenderName: c.last_sender_name || '',
    lastMentionMe: false, // [有人@我] 标记（内存态，重启不保留）
    // 用最后消息时间格式化展示（微信风格：今天时刻/昨天/星期/日期）；无消息则留空
    time: formatConvTime(lastMsgTime),
    lastMsgTime, // 最后消息 unix 秒时间戳，用于会话排序
    unread: Number(c.unread) || 0,
    markedUnread: !!Number(c.marked_unread), // 标记未读（纯本地：手动挂起红点，收新消息/打开会话自动清除）
    // uid/g_uid 为 10 位数字，Number 安全；本地库行 target_id 为文本，必须归一为数字，
    // 否则发送消息时字符串 target_id 会被后端 int64 反序列化拒绝
    targetId: c.target_id != null ? Number(c.target_id) : 0,
    convId: c.id,
    peerReadSeq: Number(c.peer_read_seq) || 0,
    // 同步水位：服务端下发的 last_synced_seq（本地行同名字段），本地追平则免拉历史
    syncSeq: Number(c.last_synced_seq) || 0,
    pinned: !!Number(c.pinned) || c.pinned === true, // 置顶（列表置顶区优先排序）
    muted: !!Number(c.muted) || c.muted === true, // 免打扰（不提醒，未读角标变灰点）
    draft: c.draft || '', // 草稿（纯本地；列表红色 [草稿] 前缀，切换会话时恢复）
    messages: [],
    oldestSeq: 0,
    _hasMore: false,
  }
}

// 消息映射器工厂：deps = {
//   meUid(): 当前用户 uid,
//   friendDisplayName(uid, fallback): 群聊发送者展示名（好友备注优先）,
//   convPeer(convId): 按服务端 conv_id 取会话头像/颜色 { avatar, color },
// }
export function createMessageMapper(deps) {
  // 把服务端消息映射为本地面板消息结构
  function mapServerMessage(m, convId, flags = {}) {
    const isMine = m.sender_uid === deps.meUid()
    // 解析 extra（图片/文件元数据），容错
    let extra = {}
    if (m.extra) {
      try {
        extra = typeof m.extra === 'string' ? JSON.parse(m.extra) : m.extra
      } catch {
        extra = {}
      }
    }
    // 群邀请系统消息：按查看者身份渲染文案——被邀请者看"你被邀请加入群聊…"，
    // 邀请者看"你邀请了xx进入群聊"，其他成员看"xx邀请了xx进入群聊"（extra 为后端结构化邀请信息）
    let text = m.content
    if (m.type === 6 && extra && extra.kind === 'group_invite') {
      const me = String(deps.meUid())
      const inviteeNames = (extra.invitee_names || []).join('、')
      if ((extra.invitee_uids || []).some((u) => String(u) === me)) {
        text = `你被邀请加入群聊「${extra.group_name || ''}」`
      } else if (String(extra.inviter_uid) === me) {
        text = `你邀请了${inviteeNames}进入群聊`
      } else if (extra.inviter_name) {
        text = `${extra.inviter_name}邀请了${inviteeNames}进入群聊`
      }
    }
    const peer = deps.convPeer(convId)
    return {
      id: String(m.id),
      seq: m.seq, // 会话内序号，用于向上分页游标
      type: isMine ? 'out' : 'in',
      msgType: m.type, // 消息类型：1 文本 / 2 图片 / 3 文件 / 6 系统
      isSystem: m.sender_uid === 0, // 系统消息（sender_uid=0，居中灰色提示，无头像/气泡）
      senderUid: m.sender_uid != null ? Number(m.sender_uid) : 0, // 发送者 uid：备注变更时据此同步刷新展示名
      senderName: deps.friendDisplayName(m.sender_uid, m.sender_name), // 发送者展示名（群聊）：好友备注优先
      avatar: isMine ? '我' : (peer.avatar || '?'),
      color: isMine ? '#2563eb' : peer.color,
      text,
      extra, // 图片/文件元数据 { url, name, size, ... }
      status: m.status || 0, // 0 正常 / 1 已撤回
      readAt: '',
      createdAt: m.created_at, // 原始 unix 秒，用于时间分组
      time: formatMsgTime(m.created_at),
      server: true,
      voicePlayed: !!flags.voicePlayed, // 语音已播放标记（本地库加载时传入，红点状态随消息恢复）
      reactions: Array.isArray(m.reactions) ? m.reactions : [], // S6 表情回应（服务端历史/增量携带）
    }
  }

  // 本地库消息行 → 面板消息结构（未同步行携带 localId/发送状态）
  // convId 为服务端 conv_id（非面板 id）：mapServerMessage 据此匹配会话头像/颜色
  function mapLocalMessage(r, convId) {
    if (!r.server_id) {
      // 本地未同步消息（离线发送 pending / failed）
      return {
        id: 'local-' + r.local_id,
        localId: r.local_id,
        seq: 0,
        type: 'out',
        msgType: Number(r.type) || 1,
        isSystem: false,
        senderName: '',
        avatar: '我',
        color: '#2563eb',
        text: r.content,
        extra: parseExtra(r.extra),
        status: Number(r.status) || 0,
        // pending 超过发送超时仍未确认的，刷新后展示为发送失败（避免永远卡在"发送中"）；
        // isPending 保持 true，离线重发回显仍可匹配替换
        readAt:
          r.sync_state === 'failed' ||
          (r.sync_state === 'pending' &&
            Number(r.created_at) > 0 &&
            Date.now() - Number(r.created_at) * 1000 > SEND_TIMEOUT_MS)
            ? '发送失败'
            : '发送中…',
        createdAt: Number(r.created_at) || 0,
        time: formatMsgTime(Number(r.created_at) || 0),
        server: true,
        isPending: r.sync_state === 'pending',
      }
    }
    return mapServerMessage(
      {
        id: r.server_id,
        seq: r.seq,
        sender_uid: r.sender_uid != null ? Number(r.sender_uid) : 0,
        sender_name: r.sender_name,
        type: r.type,
        content: r.content,
        extra: r.extra,
        status: r.status,
        created_at: r.created_at,
      },
      convId,
      { voicePlayed: !!r.voice_played } // 本地库语音已播放标记 → 红点状态随消息恢复
    )
  }

  return { mapServerMessage, mapLocalMessage }
}
