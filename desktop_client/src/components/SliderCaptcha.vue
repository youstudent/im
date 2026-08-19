<script setup>
/**
 * 滑动验证码：按住滑块拖到最右即验证通过（纯前端行为验证，防脚本批量撞库）。
 * 状态：ready（待拖动）→ sliding（拖动中）→ success（验证通过，emit success）。
 * 中途松手自动回弹复位；父组件可通过 :key 强制重置。
 */
import { ref, computed, onBeforeUnmount } from 'vue'

const emit = defineEmits(['success'])

const KNOB_SIZE = 40 // 滑块按钮尺寸（与 CSS 保持一致）
const TOLERANCE = 4 // 距右端小于该像素数即判定通过

const trackEl = ref(null)
const state = ref('ready') // ready | sliding | success
const offsetX = ref(0) // 滑块当前位移
let startX = 0
let dragging = false

// 最大可滑动距离（轨道宽 - 滑块宽 - 两侧内边距）
function maxOffset() {
  const w = trackEl.value ? trackEl.value.clientWidth : 0
  return Math.max(0, w - KNOB_SIZE - 4)
}

function onPointerDown(e) {
  if (state.value === 'success') return
  dragging = true
  startX = e.clientX - offsetX.value
  state.value = 'sliding'
  e.target.setPointerCapture?.(e.pointerId)
}

function onPointerMove(e) {
  if (!dragging || state.value === 'success') return
  const max = maxOffset()
  offsetX.value = Math.min(max, Math.max(0, e.clientX - startX))
}

function onPointerUp() {
  if (!dragging) return
  dragging = false
  if (state.value === 'success') return
  if (offsetX.value >= maxOffset() - TOLERANCE) {
    offsetX.value = maxOffset()
    state.value = 'success'
    emit('success')
  } else {
    // 未拖到底：回弹复位，允许重试
    offsetX.value = 0
    state.value = 'ready'
  }
}

const fillStyle = computed(() => ({ width: `${offsetX.value + KNOB_SIZE / 2 + 2}px` }))
const knobStyle = computed(() => ({ transform: `translateX(${offsetX.value}px)` }))
const hintText = computed(() => {
  if (state.value === 'success') return '验证通过'
  if (state.value === 'sliding') return '继续拖动滑块到最右'
  return '请按住滑块，拖动到最右侧'
})

// 兜底：指针被系统中断（如窗口失焦）时复位，避免卡在 sliding 态
function onPointerCancel() {
  if (state.value !== 'success') {
    dragging = false
    offsetX.value = 0
    state.value = 'ready'
  }
}

onBeforeUnmount(() => {
  dragging = false
})
</script>

<template>
  <div
    class="slider-captcha"
    :class="state"
    role="slider"
    aria-label="滑动验证码"
    :aria-valuenow="offsetX"
  >
    <div ref="trackEl" class="track">
      <div class="fill" :style="fillStyle"></div>
      <span class="hint" :class="{ hidden: state === 'sliding' && offsetX > 30 }">{{ hintText }}</span>
      <button
        type="button"
        class="knob"
        :style="knobStyle"
        :class="{ noanim: state === 'sliding' }"
        @pointerdown.prevent="onPointerDown"
        @pointermove="onPointerMove"
        @pointerup="onPointerUp"
        @pointercancel="onPointerCancel"
      >
        <!-- 箭头 / 对勾 -->
        <svg v-if="state !== 'success'" viewBox="0 0 16 16" width="16" height="16">
          <path d="M5 3l5 5-5 5M2.5 8H10" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
        <svg v-else viewBox="0 0 16 16" width="16" height="16">
          <path d="M3 8.5l3.2 3L13 4.5" fill="none" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
    </div>
  </div>
</template>

<style scoped>
.slider-captcha {
  width: 100%;
  user-select: none;
  touch-action: none;
}

.track {
  position: relative;
  height: 44px;
  background: var(--im-surface-2);
  border: 1px solid var(--im-border);
  border-radius: var(--im-radius-card);
  overflow: hidden;
}

/* 成功态：整条变绿 */
.slider-captcha.success .track {
  background: rgba(34, 197, 94, 0.12);
  border-color: rgba(34, 197, 94, 0.45);
}

.fill {
  position: absolute;
  top: 0;
  left: 0;
  height: 100%;
  background: rgba(37, 99, 235, 0.12);
  border-radius: var(--im-radius-card) 0 0 var(--im-radius-card);
}

.slider-captcha.success .fill {
  background: rgba(34, 197, 94, 0.25);
}

.hint {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  color: var(--im-text-muted);
  pointer-events: none;
  transition: opacity 0.15s;
}

.hint.hidden {
  opacity: 0;
}

.slider-captcha.success .hint {
  color: #16a34a;
  font-weight: 500;
}

.knob {
  position: absolute;
  top: 1px;
  left: 2px;
  width: 40px;
  height: 40px;
  border: none;
  border-radius: calc(var(--im-radius-card) - 2px);
  background: #fff;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.18);
  color: var(--im-text-muted);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: grab;
  transition: transform 0.25s ease, background 0.2s;
}

/* 拖动中关闭回弹过渡，保证跟手 */
.knob.noanim {
  transition: none;
  cursor: grabbing;
}

.slider-captcha.success .knob {
  background: #22c55e;
  color: #fff;
}
</style>
