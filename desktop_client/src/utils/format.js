// 消息模块通用常量与纯格式化/分组工具（无 Vue 依赖，可单测）

// 消息类型常量（与后端一致）：1 文本 / 2 图片 / 3 文件 / 4 语音 / 5 视频 / 6 系统 / 7 合并转发
export const MSG_TYPE = { TEXT: 1, IMAGE: 2, FILE: 3, VOICE: 4, VIDEO: 5, SYSTEM: 6, MERGE: 7 }

// 发送确认超时：30s 内未收到服务端确认（WS 回显/HTTP 响应）即置为发送失败
export const SEND_TIMEOUT_MS = 30 * 1000

// 未读数量格式化：超过 99 显示 "99+"
export function formatUnread(n) {
  return n > 99 ? '99+' : String(n)
}

// 通话时长格式化：超过 1 小时展示 H:MM:SS，否则 MM:SS
export function formatCallDuration(sec) {
  const s = Math.max(0, Number(sec) || 0)
  const hh = Math.floor(s / 3600)
  const mm = String(Math.floor((s % 3600) / 60)).padStart(2, '0')
  const ss = String(s % 60).padStart(2, '0')
  return hh > 0 ? `${hh}:${mm}:${ss}` : `${mm}:${ss}`
}

export function formatMsgTime(unixSec) {
  if (!unixSec) return ''
  const d = new Date(unixSec * 1000)
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  return `${hh}:${mm}`
}

// 会话列表时间展示：按微信风格（今天 HH:MM / 昨天 / 星期X / 同年内 M月D日 / 跨年 X年M月D日）
export function formatConvTime(unixSec) {
  if (!unixSec) return ''
  const d = new Date(unixSec * 1000)
  const now = new Date()
  if (sameDay(unixSec, Math.floor(now.getTime() / 1000))) {
    const hh = String(d.getHours()).padStart(2, '0')
    const mm = String(d.getMinutes()).padStart(2, '0')
    return `${hh}:${mm}`
  }
  const yesterday = new Date(now.getTime() - 86400 * 1000)
  if (d.getFullYear() === yesterday.getFullYear() && d.getMonth() === yesterday.getMonth() && d.getDate() === yesterday.getDate()) {
    return '昨天'
  }
  const week = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'][d.getDay()]
  const startOfWeek = new Date(now.getFullYear(), now.getMonth(), now.getDate() - now.getDay())
  if (d >= startOfWeek) return week
  if (d.getFullYear() === now.getFullYear()) return `${d.getMonth() + 1}月${d.getDate()}日`
  return `${d.getFullYear()}年${d.getMonth() + 1}月${d.getDate()}日`
}

// 格式化文件大小
export function formatFileSize(bytes) {
  if (!bytes && bytes !== 0) return ''
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}

// 判断两个 unix 秒时间戳是否属于同一天（用于显示日期分隔）
export function sameDay(a, b) {
  if (!a || !b) return false
  const da = new Date(a * 1000)
  const db = new Date(b * 1000)
  return (
    da.getFullYear() === db.getFullYear() &&
    da.getMonth() === db.getMonth() &&
    da.getDate() === db.getDate()
  )
}

// 时间分隔文案：按微信风格（今天/昨天/星期/具体日期 + 时间）
export function formatTimeDivider(unixSec) {
  if (!unixSec) return ''
  const d = new Date(unixSec * 1000)
  const now = new Date()
  const pad = (n) => String(n).padStart(2, '0')
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`
  if (sameDay(unixSec, Math.floor(now.getTime() / 1000))) return hm
  const yesterday = new Date(now.getTime() - 86400 * 1000)
  if (d.getFullYear() === yesterday.getFullYear() && d.getMonth() === yesterday.getMonth() && d.getDate() === yesterday.getDate()) {
    return `昨天 ${hm}`
  }
  const week = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'][d.getDay()]
  const startOfWeek = new Date(now.getFullYear(), now.getMonth(), now.getDate() - now.getDay())
  if (d >= startOfWeek) return `${week} ${hm}`
  return `${d.getFullYear()}年${d.getMonth() + 1}月${d.getDate()}日 ${hm}`
}

// 日期分组标题：仅显示"今天 / 昨天 / 星期X / 具体日期"（不含时刻）
export function formatDayLabel(unixSec) {
  if (!unixSec) return ''
  const d = new Date(unixSec * 1000)
  const now = new Date()
  if (sameDay(unixSec, Math.floor(now.getTime() / 1000))) return '今天'
  const yesterday = new Date(now.getTime() - 86400 * 1000)
  if (d.getFullYear() === yesterday.getFullYear() && d.getMonth() === yesterday.getMonth() && d.getDate() === yesterday.getDate()) {
    return '昨天'
  }
  const week = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'][d.getDay()]
  const startOfWeek = new Date(now.getFullYear(), now.getMonth(), now.getDate() - now.getDay())
  if (d >= startOfWeek) return week
  return `${d.getFullYear()}年${d.getMonth() + 1}月${d.getDate()}日`
}

// 计算消息列表的展示元信息：时间分隔 + 每条消息均展示头像
export function computeDisplayMeta(msgs) {
  if (!Array.isArray(msgs) || msgs.length === 0) return []
  return msgs.map((m, i) => {
    const prev = msgs[i - 1]
    // 时间分隔：只看时间间隔（跨天 / 与上一条间隔超过 5 分钟），与发送者无关
    const showTimeDivider =
      !prev || !sameDay(prev.createdAt, m.createdAt) || m.createdAt - prev.createdAt > 5 * 60
    return {
      msg: m,
      showTimeDivider,
    }
  })
}

// 按天分组消息：返回 [{ dayLabel, dayKey, list: [meta...] }]，每天独立展示
export function groupMessagesByDay(msgs) {
  if (!Array.isArray(msgs) || msgs.length === 0) return []
  const groups = []
  for (const m of msgs) {
    const key = m.createdAt ? new Date(m.createdAt * 1000).toDateString() : 'other'
    const last = groups[groups.length - 1]
    if (!last || last.dayKey !== key) {
      groups.push({ dayKey: key, dayLabel: formatDayLabel(m.createdAt), list: [] })
    }
    groups[groups.length - 1].list.push(m)
  }
  // 每组内部计算展示元信息（时间分隔 / 头像合并）
  return groups.map((g) => ({ ...g, list: computeDisplayMeta(g.list) }))
}
