<script setup>
// 表情面板：常用 emoji 网格，点击插入输入框
const emit = defineEmits(['select'])

const emojiList = [
  '😀', '😁', '😂', '🤣', '😊', '😍', '😘', '😜', '🤔', '😎',
  '😭', '😡', '😱', '😴', '🤗', '🙄', '😏', '🥺', '😇', '🤯',
  '👍', '👎', '👏', '🙏', '💪', '🫶', '✌️', '🤞', '🖐️', '👌',
  '❤️', '💔', '💯', '🔥', '✨', '⭐', '🌹', '🎉', '🎂', '☀️',
]
</script>

<template>
  <div class="emoji-panel" @mousedown.stop>
    <div class="emoji-grid">
      <button
        v-for="e in emojiList"
        :key="e"
        class="emoji-cell"
        @click="emit('select', e)"
      >{{ e }}</button>
    </div>
  </div>
</template>

<style scoped>
.emoji-panel {
  position: absolute;
  bottom: 132px;
  left: 16px;
  z-index: 100;
  width: 320px;
  max-height: 220px;
  overflow-y: auto;
  background: var(--im-surface);
  border: 1px solid var(--im-border);
  border-radius: 12px;
  box-shadow: 0 8px 30px rgba(0, 0, 0, 0.14);
  padding: 10px;
}

.emoji-grid {
  display: grid;
  grid-template-columns: repeat(8, 1fr);
  gap: 2px;
}

.emoji-cell {
  background: transparent;
  border: none;
  border-radius: 6px;
  font-size: 1.35rem;
  line-height: 1;
  padding: 6px 2px;
  cursor: pointer;
  transition: background 0.12s;
}

.emoji-cell:hover {
  background: var(--im-surface-2);
}
</style>
