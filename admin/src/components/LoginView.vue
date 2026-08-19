<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { adminApi } from '../api/admin'
import { setToken } from '../api/http'

const router = useRouter()

const loginForm = ref({ username: '', password: '' })
const loginError = ref('')
const loginLoading = ref(false)

async function doLogin() {
  loginError.value = ''
  loginLoading.value = true
  try {
    const res = await adminApi.login(loginForm.value.username, loginForm.value.password)
    setToken(res.access_token)
    router.replace('/dashboard')
  } catch (e) {
    loginError.value = e.message || '登录失败'
  } finally {
    loginLoading.value = false
  }
}
</script>

<template>
  <div class="login-page">
    <div class="login-card">
      <div class="login-logo">W</div>
      <h1>WorkChat 管理后台</h1>
      <p class="login-sub">请使用管理员账号登录</p>
      <form @submit.prevent="doLogin">
        <input v-model="loginForm.username" class="field" type="text" placeholder="用户名" autocomplete="username" />
        <input v-model="loginForm.password" class="field" type="password" placeholder="密码" autocomplete="current-password" />
        <div v-if="loginError" class="error">{{ loginError }}</div>
        <button class="btn-primary" type="submit" :disabled="loginLoading">
          {{ loginLoading ? '登录中…' : '登 录' }}
        </button>
      </form>
      <p class="login-tip">默认账号 admin / admin123</p>
    </div>
  </div>
</template>
