<script setup>
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { setToken, setMustChangePwd, mustChangePwd } from '../api/http'
import { adminApi } from '../api/admin'
import { useUi } from '../composables/useUi'

const router = useRouter()
const { state, closeConfirm, runConfirm, showToast, closeToast } = useUi()

function logout() {
  setToken('')
  setMustChangePwd(false)
  router.replace('/login')
}

// ---- 强制改密（种子默认账号首次登录）：不可关闭，改密成功前无法使用后台 ----
const showPwdModal = ref(mustChangePwd())
const pwdForm = ref({ oldPassword: '', newPassword: '', confirm: '' })
const pwdError = ref('')
const pwdLoading = ref(false)
const pwdOk = computed(
  () => pwdForm.value.newPassword.length >= 8 && pwdForm.value.newPassword === pwdForm.value.confirm
)

async function submitPwdChange() {
  pwdError.value = ''
  if (!pwdForm.value.oldPassword || !pwdForm.value.newPassword) {
    pwdError.value = '请填写旧密码与新密码'
    return
  }
  if (pwdForm.value.newPassword.length < 8 || !/[a-zA-Z]/.test(pwdForm.value.newPassword) || !/\d/.test(pwdForm.value.newPassword)) {
    pwdError.value = '新密码至少 8 位，且同时包含字母和数字'
    return
  }
  if (pwdForm.value.newPassword !== pwdForm.value.confirm) {
    pwdError.value = '两次输入的新密码不一致'
    return
  }
  pwdLoading.value = true
  try {
    await adminApi.changePassword(pwdForm.value.oldPassword, pwdForm.value.newPassword)
    setMustChangePwd(false)
    showPwdModal.value = false
    showToast('密码修改成功', 'success')
  } catch (e) {
    pwdError.value = e.message || '修改密码失败'
  } finally {
    pwdLoading.value = false
  }
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

  <!-- 强制改密弹窗（种子默认账号）：无关闭入口，改密成功前遮罩不可穿透 -->
  <div v-if="showPwdModal" class="modal-mask">
    <div class="modal-card">
      <div class="modal-head">
        <span class="modal-icon danger">!</span>
        <h3 class="modal-title">首次登录，请修改默认密码</h3>
      </div>
      <p class="modal-message">当前账号仍在使用初始密码，为保障后台安全，请立即修改。</p>
      <form class="pwd-form" @submit.prevent="submitPwdChange">
        <input v-model="pwdForm.oldPassword" class="field" type="password" placeholder="当前密码" autocomplete="current-password" />
        <input v-model="pwdForm.newPassword" class="field" type="password" placeholder="新密码（至少 8 位，含字母和数字）" autocomplete="new-password" />
        <input v-model="pwdForm.confirm" class="field" type="password" placeholder="确认新密码" autocomplete="new-password" />
        <div v-if="pwdError" class="pwd-error">{{ pwdError }}</div>
        <button class="btn-primary" type="submit" :disabled="pwdLoading || !pwdOk">
          {{ pwdLoading ? '提交中…' : '确认修改' }}
        </button>
      </form>
    </div>
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
