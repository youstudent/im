<script setup>
// 消息气泡内容：文本/图片/语音/视频/文件（收/发两侧共用，消除重复模板）
import { isAudioMsg, isVideoMsg } from '../utils/message'
import { formatFileSize } from '../utils/format'

const props = defineProps({
  msg: { type: Object, required: true },
  // 方向：'in' 对方消息 / 'out' 我方消息（影响气泡配色与语音波形方向）
  side: { type: String, default: 'in' },
  // 语音播放器能力（useVoicePlayer 返回值）：播放状态/时长/宽度/未读红点
  voice: { type: Object, default: null },
})

const emit = defineEmits(['menu', 'image-loaded', 'open-image', 'open-video', 'open-file', 'video-error'])
</script>

<template>
  <div
    class="bubble"
    :class="[side, { 'bubble-media': (msg.msgType === 2 || msg.msgType === 3) && !isAudioMsg(msg) }]"
    @click="emit('menu', $event, msg)"
  >
    <!-- 文本消息 -->
    <template v-if="msg.msgType === 1 || !msg.msgType">{{ msg.text }}</template>
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
  </div>
</template>

<style scoped>
/* 气泡基础样式：根元素同时携带父组件作用域属性，父级 .message-row 相关规则仍可命中 */
.bubble {
  /* 按文本自然撑开，避免父容器把气泡压缩到最小宽度 */
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
</style>
