<script setup>
// 消息气泡内容：文本/图片/语音/视频/文件（收/发两侧共用，消除重复模板）
import { computed } from 'vue'
import { isAudioMsg, isVideoMsg, msgTypeLabel } from '../utils/message'
import { formatFileSize } from '../utils/format'

const props = defineProps({
  msg: { type: Object, required: true },
  // 方向：'in' 对方消息 / 'out' 我方消息（影响气泡配色与语音波形方向）
  side: { type: String, default: 'in' },
  // 语音播放器能力（useVoicePlayer 返回值）：播放状态/时长/宽度/未读红点
  voice: { type: Object, default: null },
  // 引用发送者名称解析（可选）：(uid, fallback) => 展示名，使备注变更后引用名同步刷新
  resolveName: { type: Function, default: null },
  // S6 表情回应（已聚合）：[{ emoji, count, mine }]，父组件按消息渲染时传入
  reactions: { type: Array, default: () => [] },
  // 是否可添加表情回应（会话成员均可；消息未撤回时父组件保证）
  canReact: { type: Boolean, default: true },
  // G14 群已读人数：showReadCount=true 且 readCount>0 时气泡下方显示 "N 人已读"
  showReadCount: { type: Boolean, default: false },
  readCount: { type: Number, default: 0 },
})

const emit = defineEmits(['menu', 'image-loaded', 'open-image', 'open-video', 'open-file', 'video-error', 'quote-jump', 'open-merge', 'react'])

// S6 快捷表情栏：hover 气泡时浮现（微信风格：点赞/爱心/大笑/惊讶/哭/生气）
const QUICK_EMOJIS = ['👍', '❤️', '😂', '😮', '😢', '😡']

// 文本分段：携带 mention_uids 时把 @xxx 片段高亮（蓝色），其余原样保留换行
const textSegments = computed(() => {
  const text = props.msg.text || ''
  const mentions = props.msg.extra && props.msg.extra.mention_uids
  if (!Array.isArray(mentions) || !mentions.length || !text) return null
  const segs = []
  const re = /@[^\s@]+/g
  let last = 0
  let m
  while ((m = re.exec(text))) {
    if (m.index > last) segs.push({ t: text.slice(last, m.index) })
    segs.push({ t: m[0], hl: true })
    last = m.index + m[0].length
  }
  if (last < text.length) segs.push({ t: text.slice(last) })
  return segs
})

// 引用块摘要：非文本被引消息展示类型占位；被引消息已撤回时降级提示（发送时快照，不实时查原文）
const quoteSummary = computed(() => {
  const q = props.msg.extra && props.msg.extra.quote
  if (!q) return ''
  const t = Number(q.type) || 1
  if (t !== 1) return msgTypeLabel(t)
  const c = String(q.content || '')
  return c.length > 60 ? c.slice(0, 60) + '…' : c
})

// 引用发送者展示名：优先走外部解析（好友备注优先，备注变更时响应式刷新），
// 无解析器时回落发送时快照名
const quoteSenderName = computed(() => {
  const q = props.msg.extra && props.msg.extra.quote
  if (!q) return ''
  if (props.resolveName) {
    const n = props.resolveName(q.sender_uid, q.sender_name)
    if (n) return n
  }
  return q.sender_name || '对方'
})

// 被引消息是否已失效（撤回）：原文快照仍在，但展示降级提示（删除场景本地无行同理处理）
const quoteInvalid = computed(() => {
  const q = props.msg.extra && props.msg.extra.quote
  return !!q && Number(q.status) === 1
})

// 合并转发数据（S2，type=7）：content 为 JSON { count, items:[{sender_name, type, content}] }；
// 解析失败降级为空列表（旧数据/脏数据不阻断渲染）
const mergeData = computed(() => {
  try {
    const d = JSON.parse(props.msg.text || '{}')
    return {
      count: Number(d.count) || (Array.isArray(d.items) ? d.items.length : 0),
      items: Array.isArray(d.items) ? d.items : [],
    }
  } catch {
    return { count: 0, items: [] }
  }
})
</script>

<template>
  <div
    class="bubble"
    :class="[side, { 'bubble-media': (msg.msgType === 2 || msg.msgType === 3) && !isAudioMsg(msg), 'has-quote': !!(msg.extra && msg.extra.quote) }]"
    @click="emit('menu', $event, msg)"
  >
    <!-- 正文容器：带引用时纵向排列（正文在上，引用独立气泡在下） -->
    <div class="bubble-body">
    <!-- 文本消息：带 @提及时分段高亮 -->
    <template v-if="msg.msgType === 1 || !msg.msgType">
      <template v-if="textSegments"><template v-for="(seg, si) in textSegments" :key="si"><span v-if="seg.hl" class="mention-hl">{{ seg.t }}</span><template v-else>{{ seg.t }}</template></template></template>
      <template v-else>{{ msg.text }}</template>
    </template>
    <!-- 图片消息：本体 + 上传中旋转遮罩（微信风格，仅未发送完成时显示） -->
    <template v-else-if="msg.msgType === 2">
      <div class="msg-image-wrap">
        <img class="msg-image" :src="msg.extra.cacheUrl || msg.extra.url" alt="图片" @load="emit('image-loaded', msg)" @click.stop="emit('open-image', msg)" />
        <div v-if="msg.isUploading" class="media-pending-mask">
          <span class="spinner media-spinner"></span>
        </div>
      </div>
    </template>
    <!-- 语音消息：微信风格波形 + 时长，点击播放/暂停 -->
    <template v-else-if="isAudioMsg(msg)">
      <div
        class="msg-voice"
        :class="{ playing: voice && voice.isPlayingVoice(msg) }"
        :style="{ minWidth: voice ? voice.voiceBubbleWidth(msg) : '64px' }"
        role="button"
        :aria-label="voice && voice.isPlayingVoice(msg) ? '暂停语音' : '播放语音'"
        :title="voice && voice.isPlayingVoice(msg) ? '点击暂停' : '点击播放'"
        @click.stop="voice && voice.togglePlayVoice(msg)"
      >
        <svg class="voice-wave" viewBox="0 0 20 20" width="18" height="18" aria-hidden="true">
          <path d="M7.2 8.1a3.3 3.3 0 0 1 0 3.8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
          <path d="M9.9 6a6.6 6.6 0 0 1 0 8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
          <path d="M12.6 4a9.9 9.9 0 0 1 0 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
        </svg>
        <span class="voice-dur">{{ voice ? voice.voiceDurationLabel(msg) : '' }}</span>
        <span v-if="msg.isUploading" class="spinner media-spinner voice-spinner"></span>
      </div>
      <!-- 未读红点：接收的语音未播放过时展示（自己发送的不显示） -->
      <span v-if="voice && voice.voiceUnread(msg)" class="voice-unread-dot" aria-label="未播放语音"></span>
    </template>
    <!-- 视频消息：缩略图预览，点击弹层播放 -->
    <template v-else-if="isVideoMsg(msg)">
      <div class="msg-video-wrap" title="点击播放" @click.stop="emit('open-video', msg)">
        <video class="msg-video" :src="msg.extra.cacheUrl || msg.extra.url" preload="metadata" muted playsinline @error="emit('video-error', msg, $event)"></video>
        <div v-if="msg.isUploading" class="media-pending-mask">
          <span class="spinner media-spinner"></span>
        </div>
        <div v-else class="video-play-icon" aria-hidden="true">
          <svg viewBox="0 0 24 24" width="26" height="26">
            <path d="M8.5 6v12l10-6z" fill="currentColor" />
          </svg>
        </div>
      </div>
    </template>
    <!-- 文件消息 -->
    <template v-else-if="msg.msgType === 3">
      <div class="msg-file" @click.stop="emit('open-file', msg)">
        <div class="file-icon">
          <svg viewBox="0 0 20 20" width="20" height="20">
            <path d="M5 2.5h6l4 4v11H5z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
            <path d="M11.67 2.5v3.33H15" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
          </svg>
        </div>
        <div class="file-meta">
          <span class="file-name">{{ msg.extra.name || '文件' }}</span>
          <span class="file-size">{{ formatFileSize(msg.extra.size) }}</span>
        </div>
        <!-- 上传中旋转态 -->
        <span v-if="msg.isUploading" class="spinner media-spinner file-spinner"></span>
      </div>
    </template>
    <!-- 合并转发消息（S2）：卡片展示前几条摘要，点击打开完整列表弹层 -->
    <template v-else-if="msg.msgType === 7">
      <div class="msg-merge" role="button" title="点击查看合并转发的消息" @click.stop="emit('open-merge', mergeData)">
        <div class="merge-title">合并转发的消息</div>
        <div v-for="(it, ii) in mergeData.items.slice(0, 3)" :key="ii" class="merge-line">{{ it.sender_name }}：{{ it.content }}</div>
        <div class="merge-footer">共 {{ mergeData.count }} 条</div>
      </div>
    </template>
    </div>
    <!-- 引用块：独立灰色气泡置于正文气泡下方，点击定位被引消息；
         被引消息已撤回时降级提示 -->
    <div v-if="msg.extra && msg.extra.quote" class="quote-block" @click.stop="emit('quote-jump', msg.extra.quote)">
      <template v-if="quoteInvalid"><span class="quote-line">引用内容不存在</span></template>
      <template v-else>
        <span class="quote-line">{{ quoteSenderName }}：{{ quoteSummary }}</span>
      </template>
    </div>

    <!-- S6 表情回应：已添加的反应横排展示（点击自己的反应可取消）；hover 快捷表情栏 -->
    <div v-if="reactions.length" class="reaction-row" @click.stop>
      <button
        v-for="r in reactions"
        :key="r.emoji"
        class="reaction-chip"
        :class="{ mine: r.mine }"
        :title="`${r.count} 人回应`"
        @click.stop="emit('react', { emoji: r.emoji, add: !r.mine })"
      >
        <span class="reaction-emoji">{{ r.emoji }}</span>
        <span class="reaction-count">{{ r.count }}</span>
      </button>
    </div>

    <!-- G14 已读人数：仅群主/管理员视角展示 -->
    <span v-if="showReadCount && readCount > 0" class="read-count" @click.stop>已读 {{ readCount }} 人</span>

    <!-- S6 快捷表情栏：hover 气泡浮现（不可撤回消息不展示） -->
    <div v-if="canReact && msg.status !== 1" class="quick-reaction" @click.stop>
      <button
        v-for="e in QUICK_EMOJIS"
        :key="e"
        class="quick-reaction-btn"
        :title="`回应 ${e}`"
        @click.stop="emit('react', { emoji: e, add: true })"
      >
        {{ e }}
      </button>
    </div>
  </div>
</template>

<style scoped>
/* 气泡基础样式：根元素同时携带父组件作用域属性，父级 .message-row 相关规则仍可命中 */
.bubble {
  /* 按文本自然撑开，避免父容器把气泡压缩到最小宽度 */
  position: relative; /* S6 快捷表情栏定位锚点 */
  width: fit-content;
  min-width: 2.5em;
  padding: 12px 14px;
  border-radius: 12px;
  font-size: 1rem;
  line-height: 22px;
  word-break: break-word;
  white-space: pre-wrap;
  box-sizing: border-box;
}

.bubble.in {
  max-width: 100%;
  background: var(--im-surface);
  color: var(--im-text-title);
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.06);
}

.bubble.out {
  max-width: 100%;
  background: #b2f0c5;
  color: #000;
  box-shadow: 0 2px 6px rgba(46, 160, 82, 0.15);
}

/* 图片/文件消息气泡：去掉气泡背景/内边距/阴影，内容由自身卡片样式控制 */
.bubble.bubble-media {
  background: transparent;
  box-shadow: none;
  padding: 0;
  min-width: 0;
  /* 字色改用主题标题色：原 .bubble.out 的 #000 在无背景深色模式下不可见 */
  color: var(--im-text-title);
}

/* 深色模式下图片/文件气泡同样无背景 */
:global([data-theme='dark']) .bubble.bubble-media.in,
:global([data-theme='dark']) .bubble.bubble-media.out {
  background: transparent;
}

/* 消息气泡 hover 提示可操作 */
.bubble:hover {
  cursor: pointer;
}

/* 加载指示器（气泡内媒体上传中用） */
.spinner {
  width: 16px;
  height: 16px;
  border: 2px solid var(--im-border);
  border-top-color: var(--im-primary);
  border-radius: 999px;
  animation: bubbleSpin 0.8s linear infinite;
}

@keyframes bubbleSpin {
  to {
    transform: rotate(360deg);
  }
}

/* ===== 图片消息 ===== */
.msg-image-wrap {
  position: relative;
  line-height: 0;
}

.msg-image {
  display: block;
  max-width: 220px;
  max-height: 260px;
  border-radius: 8px;
  object-fit: cover;
  cursor: zoom-in;
}

/* ===== 语音消息：微信风格波形 + 时长（currentColor 自适应收/发气泡配色） ===== */
.msg-voice {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 22px;
  cursor: pointer;
  user-select: none;
}

/* 发送方语音镜像：时长在左、波形在右（与微信收发方向一致） */
.bubble.out .msg-voice {
  flex-direction: row-reverse;
}

.bubble.out .voice-wave {
  transform: scaleX(-1);
}

.voice-wave {
  flex-shrink: 0;
}

/* 播放中：波形三段弧线依次闪烁 */
.msg-voice.playing .voice-wave path { animation: wavePulse 0.9s ease-in-out infinite; }
.msg-voice.playing .voice-wave path:nth-child(2) { animation-delay: 0.15s; }
.msg-voice.playing .voice-wave path:nth-child(3) { animation-delay: 0.3s; }

@keyframes wavePulse {
  0%, 100% { opacity: 0.3; }
  50% { opacity: 1; }
}

.voice-dur {
  font-size: 0.929rem;
  line-height: 22px;
}

.voice-spinner {
  width: 14px;
  height: 14px;
}

/* 含语音的气泡：未读红点绝对定位在气泡外侧，不占气泡背景 */
.bubble:has(.msg-voice) {
  position: relative;
}

/* 未读红点：接收语音未播放过时展示在气泡旁 */
.voice-unread-dot {
  position: absolute;
  top: 50%;
  right: -14px;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--im-danger, #ef4444);
  transform: translateY(-50%);
}

/* ===== 文件消息卡片：气泡背景移除后，文件卡片自带浅色底以保证可读性 ===== */
.msg-file {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 160px;
  padding: 10px 12px;
  background: var(--im-surface-2);
  border: 1px solid var(--im-border);
  border-radius: 10px;
  cursor: pointer;
}

.file-icon {
  flex-shrink: 0;
  width: 38px;
  height: 38px;
  border-radius: 8px;
  background: rgba(37, 99, 235, 0.1);
  color: var(--im-primary);
  display: flex;
  align-items: center;
  justify-content: center;
}

:global([data-theme='dark']) .file-icon {
  background: rgba(96, 165, 250, 0.14);
}

.file-meta {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}

.file-name {
  font-size: 0.9rem;
  line-height: 1.3;
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-size {
  font-size: 0.75rem;
  opacity: 0.75;
}

/* ===== 视频消息气泡：缩略图 + 中央播放图标 ===== */
.msg-video-wrap {
  position: relative;
  width: 220px;
  max-width: 100%;
  border-radius: 8px;
  overflow: hidden;
  background: #000;
  cursor: pointer;
}

.msg-video {
  display: block;
  width: 100%;
  min-height: 120px;
  max-height: 260px;
  object-fit: cover;
}

.video-play-icon {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  background: rgba(0, 0, 0, 0.12);
  transition: background-color 0.15s ease;
}

.msg-video-wrap:hover .video-play-icon {
  background: rgba(0, 0, 0, 0.24);
}

/* ===== 上传中旋转态（微信风格：未发送完成的图片/文件覆盖旋转指示） ===== */
.media-pending-mask {
  position: absolute;
  inset: 0;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
}

:global([data-theme='dark']) .media-pending-mask {
  background: rgba(0, 0, 0, 0.4);
}

.media-spinner {
  width: 22px;
  height: 22px;
  border-width: 2.5px;
  border-color: rgba(37, 99, 235, 0.25);
  border-top-color: var(--im-primary);
}

.file-spinner {
  flex-shrink: 0;
  margin-left: 4px;
}

/* ===== 引用回复：正文与引用使用独立气泡纵向排列（正文在上，引用灰色
   独立气泡在下），点击引用定位原文 ===== */
.bubble.has-quote {
  display: flex;
  flex-direction: column;
  /* 不拉伸子块：正文/引用气泡宽度各自跟随自身内容 */
  align-items: flex-start;
  /* 外层仅做容器，背景/阴影/内边距下放至正文气泡与引用气泡 */
  padding: 0;
  background: transparent;
  box-shadow: none;
}

/* 发送侧靠右对齐：正文与引用气泡右边缘对齐 */
.bubble.has-quote.out {
  align-items: flex-end;
}

/* 带引用的正文独立气泡：继承基础气泡配色 */
.bubble.has-quote .bubble-body {
  padding: 12px 14px;
  border-radius: 12px;
}

.bubble.has-quote.in .bubble-body {
  background: var(--im-surface);
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.06);
}

.bubble.has-quote.out .bubble-body {
  background: #b2f0c5;
  box-shadow: 0 2px 6px rgba(46, 160, 82, 0.15);
}

/* 媒体消息（图片/视频/文件）本体无气泡背景，卡片自带样式 */
.bubble.bubble-media.has-quote .bubble-body {
  padding: 0;
  background: transparent;
  box-shadow: none;
}

.bubble-body {
  min-width: 0;
}

/* 引用独立气泡：浅灰底圆角卡片，宽度跟随引用文字，超宽时省略号截断 */
.quote-block {
  margin-top: 6px;
  padding: 6px 10px;
  border-radius: 8px;
  background: rgba(0, 0, 0, 0.045);
  cursor: pointer;
  user-select: none;
  max-width: 100%;
}

.quote-block:hover .quote-line {
  opacity: 0.7;
}

.quote-line {
  display: block;
  font-size: 0.857rem;
  line-height: 1.5;
  color: rgba(0, 0, 0, 0.5);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 发送侧正文气泡上引用文字保持深灰，可读性一致 */
.bubble.out .quote-line {
  color: rgba(0, 0, 0, 0.55);
}

:global([data-theme='dark']) .quote-block {
  background: rgba(255, 255, 255, 0.08);
}

:global([data-theme='dark']) .quote-line,
:global([data-theme='dark']) .bubble.out .quote-line {
  color: rgba(255, 255, 255, 0.55);
}

/* @提及高亮：品牌蓝 */
.mention-hl {
  color: var(--im-primary);
}

/* ===== S6 表情回应 ===== */
.reaction-row {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 6px;
  max-width: 260px;
}

.reaction-chip {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 2px 8px;
  border: 1px solid var(--im-border);
  border-radius: 999px;
  background: var(--im-surface);
  font-family: inherit;
  font-size: 0.857rem;
  line-height: 18px;
  cursor: pointer;
  transition: background 0.15s ease;
}

.reaction-chip.mine {
  background: rgba(37, 99, 235, 0.12);
  border-color: rgba(37, 99, 235, 0.4);
}

.reaction-chip:hover {
  background: var(--im-surface-2);
}

.reaction-count {
  color: var(--im-text-secondary);
  font-size: 0.786rem;
}

/* G14 已读人数：气泡下方小字（仅群主/管理员视角） */
.read-count {
  display: block;
  margin-top: 4px;
  font-size: 0.714rem;
  color: var(--im-text-muted);
  text-align: right;
}

/* S6 快捷表情栏：hover 气泡时右上角浮现 */
.quick-reaction {
  position: absolute;
  top: -30px;
  right: 0;
  display: flex;
  gap: 2px;
  padding: 3px 6px;
  background: var(--im-surface);
  border: 1px solid var(--im-border);
  border-radius: 999px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.12);
  opacity: 0;
  visibility: hidden;
  transform: translateY(2px);
  transition: opacity 0.15s ease, transform 0.15s ease, visibility 0.15s;
  z-index: 20;
}

.bubble:hover .quick-reaction {
  opacity: 1;
  visibility: visible;
  transform: translateY(0);
}

.quick-reaction-btn {
  border: none;
  background: none;
  font-size: 1.071rem;
  line-height: 1;
  padding: 3px;
  border-radius: 999px;
  cursor: pointer;
  transition: transform 0.1s ease, background 0.15s ease;
}

.quick-reaction-btn:hover {
  transform: scale(1.25);
  background: var(--im-surface-2);
}

:global([data-theme='dark']) .quick-reaction {
  background: var(--im-surface-2);
  border-color: var(--im-border);
}

/* ===== 合并转发卡片（S2）：标题 + 最多三条摘要 + 条数页脚，点击查看详情 ===== */
.msg-merge {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: 220px;
  max-width: 100%;
  cursor: pointer;
}

.merge-title {
  font-size: 0.929rem;
}

.merge-line {
  font-size: 0.857rem;
  color: var(--im-text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.bubble.out .merge-line {
  color: rgba(0, 0, 0, 0.55);
}

.merge-footer {
  font-size: 0.786rem;
  color: var(--im-text-muted);
  border-top: 1px solid var(--im-border);
  padding-top: 4px;
  margin-top: 2px;
}

.bubble.out .merge-footer {
  color: rgba(0, 0, 0, 0.45);
  border-top-color: rgba(0, 0, 0, 0.12);
}
</style>
