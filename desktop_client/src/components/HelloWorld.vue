<script setup>
import { ref } from 'vue'

const count = ref(0)
</script>

<template>
  <section class="card">
    <h2>Vue 3 组合式 API 示例</h2>
    <p>这是一个计数器，验证渲染进程工作正常：</p>
    <div class="counter">
      <button class="btn" @click="count--">-1</button>
      <span class="value">{{ count }}</span>
      <button class="btn" @click="count++">+1</button>
    </div>
  </section>
</template>

<style scoped>
.counter {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-top: 12px;
}

.btn {
  width: 40px;
  height: 40px;
  border: none;
  border-radius: 10px;
  background: #8b5cf6;
  color: #fff;
  font-size: 20px;
  cursor: pointer;
}

.btn:hover {
  background: #7c3aed;
}

.value {
  min-width: 48px;
  text-align: center;
  font-size: 22px;
  font-weight: 600;
}
</style>
