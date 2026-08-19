<script setup>
import { useRouter } from 'vue-router'
import { setToken } from '../api/http'
import { useUi } from '../composables/useUi'

const router = useRouter()
const { state, closeConfirm, runConfirm, closeToast } = useUi()

function logout() {
  setToken('')
  router.replace('/login')
}
</script>

<template>
  <div class="layout">
    <aside class="sidebar">
      <div class="brand">
        <div class="brand-logo">W</div>
        <span>WorkChat 后台</span>
      </div>
      <nav class="nav">
        <router-link to="/dashboard" class="nav-item" active-class="active">📊 数据看板</router-link>
        <router-link to="/users" class="nav-item" active-class="active">👥 用户管理</router-link>
        <router-link to="/groups" class="nav-item" active-class="active">👪 群组管理</router-link>
        <router-link to="/versions" class="nav-item" active-class="active">🚀 版本管理</router-link>
      </nav>
      <button class="logout" @click="logout">退出登录</button>
    </aside>

    <main class="main">
      <router-view />
    </main>
  </div>

  <!-- 确认弹窗 -->
  <div v-if="state.confirm" class="modal-mask" @click.self="closeConfirm(false)">
    <div class="modal-card">
      <div class="modal-head">
        <span class="modal-icon" :class="state.confirm.danger ? 'danger' : ''">
          {{ state.confirm.danger ? '!' : '?' }}
        </span>
        <h3 class="modal-title">{{ state.confirm.title }}</h3>
      </div>
      <p class="modal-message">{{ state.confirm.message }}</p>
      <div class="modal-actions">
        <button class="btn-plain" @click="closeConfirm(false)">取 消</button>
        <button class="btn-primary modal-btn" :class="state.confirm.danger ? 'btn-danger' : ''"
          @click="runConfirm">{{ state.confirm.confirmText }}</button>
      </div>
    </div>
  </div>

  <!-- 操作结果提示 -->
  <transition name="toast">
    <div v-if="state.toast" class="toast" :class="state.toast.type" @click="closeToast">
      {{ state.toast.message }}
    </div>
  </transition>
</template>
