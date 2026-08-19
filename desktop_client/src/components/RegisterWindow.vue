<script setup>
import { ref } from 'vue'
import { authApi } from '../api/auth'
import { tokenStore } from '../api/token'

const emit = defineEmits(['switch', 'logged-in'])

const nickname = ref('')
const account = ref('')
const password = ref('')
const confirmPassword = ref('')
const agreed = ref(false)
const showPwd = ref(false)
const showConfirmPwd = ref(false)

// 提交状态：loading 期间禁用；submitError 展示服务端错误
const loading = ref(false)
const submitError = ref('')

// 表单验证错误信息
const errors = ref({
  nickname: '',
  account: '',
  password: '',
  confirmPassword: '',
  agreement: '',
})
const touched = ref({
  nickname: false,
  account: false,
  password: false,
  confirmPassword: false,
  agreement: false,
})

// 校验昵称
function validateNickname() {
  const val = nickname.value.trim()
  if (!val) return '请输入昵称'
  if (val.length < 2 || val.length > 20) return '昵称长度需在 2-20 个字符之间'
  return ''
}

// 校验账号：支持手机号或邮箱
function validateAccount() {
  const val = account.value.trim()
  if (!val) return '请输入账号'
  const isPhone = /^1[3-9]\d{9}$/.test(val)
  const isEmail = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(val)
  if (!isPhone && !isEmail) return '请输入正确的手机号或邮箱'
  return ''
}

// 校验密码：至少 8 位，需包含字母和数字
function validatePassword() {
  const val = password.value
  if (!val) return '请输入密码'
  if (val.length < 8) return '密码长度至少 8 位'
  if (!/[a-zA-Z]/.test(val) || !/\d/.test(val)) return '密码需同时包含字母和数字'
  return ''
}

// 校验确认密码
function validateConfirmPassword() {
  const val = confirmPassword.value
  if (!val) return '请再次输入密码'
  if (val !== password.value) return '两次输入的密码不一致'
  return ''
}

// 校验用户协议
function validateAgreement() {
  return agreed.value ? '' : '请阅读并同意用户协议与隐私政策'
}

// 单字段校验（失焦时）
function checkField(field) {
  touched.value[field] = true
  switch (field) {
    case 'nickname':
      errors.value.nickname = validateNickname()
      break
    case 'account':
      errors.value.account = validateAccount()
      break
    case 'password':
      errors.value.password = validatePassword()
      break
    case 'confirmPassword':
      errors.value.confirmPassword = validateConfirmPassword()
      break
    case 'agreement':
      errors.value.agreement = validateAgreement()
      break
  }
}

// 提交校验 + 真实注册（注册即登录）
async function handleRegister() {
  ;['nickname', 'account', 'password', 'confirmPassword', 'agreement'].forEach((f) => {
    touched.value[f] = true
  })
  errors.value.nickname = validateNickname()
  errors.value.account = validateAccount()
  errors.value.password = validatePassword()
  errors.value.confirmPassword = validateConfirmPassword()
  errors.value.agreement = validateAgreement()
  if (Object.values(errors.value).some((e) => e)) return

  loading.value = true
  submitError.value = ''
  try {
    const data = await authApi.register({
      nickname: nickname.value.trim(),
      account: account.value.trim(),
      password: password.value,
    })
    await tokenStore.save({
      accessToken: data.access_token,
      refreshToken: data.refresh_token,
      remember: true,
    })
    emit('logged-in', data.user)
  } catch (e) {
    submitError.value = e.message || '注册失败，请稍后重试'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="window">
    <!-- 内容区 -->
    <div class="content">
      <!-- 品牌面板 -->
      <aside class="brand-panel">
        <div class="brand-head">
          <div class="logo">
            <svg viewBox="0 0 52 52" width="52" height="52">
              <rect width="52" height="52" rx="16" fill="#fff" />
              <path
                d="M26 14c-6.6 0-12 4.7-12 10.5 0 3.1 1.7 5.9 4.4 7.7-.2 1.3-.8 3.2-1 3.7 1.8-.4 4.5-1.6 5.5-2.3.7.1 1.4.2 2.1.2 6.6 0 12-4.7 12-10.5S32.6 14 26 14z"
                :fill="'var(--im-primary)'"
              />
            </svg>
          </div>
          <h1 class="brand-title">WorkChat</h1>
          <p class="brand-slogan">安全 · 高效 · 跨平台桌面通讯</p>
        </div>

        <ul class="features">
          <li class="feature">
            <span class="check">
              <svg viewBox="0 0 6 5" width="6" height="5">
                <path d="M1 2.5L2.5 4 5 1" fill="none" stroke="#fff" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
            </span>
            端到端加密，消息安全无忧
          </li>
          <li class="feature">
            <span class="check">
              <svg viewBox="0 0 6 5" width="6" height="5">
                <path d="M1 2.5L2.5 4 5 1" fill="none" stroke="#fff" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
            </span>
            好友与群组，沟通随时随地
          </li>
          <li class="feature">
            <span class="check">
              <svg viewBox="0 0 6 5" width="6" height="5">
                <path d="M1 2.5L2.5 4 5 1" fill="none" stroke="#fff" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
            </span>
            多端同步，历史记录云存储
          </li>
        </ul>
      </aside>

      <!-- 表单面板 -->
      <section class="form-panel">
        <div class="form-body">
          <h2 class="form-title">创建账号</h2>
          <p class="form-subtitle">填写以下信息，开通你的 WorkChat</p>

          <label class="field-label">昵称</label>
          <div class="input" :class="{ 'has-error': errors.nickname }">
            <input
              v-model="nickname"
              type="text"
              placeholder="请输入昵称"
              @input="errors.nickname = ''; touched.nickname = true"
              @blur="checkField('nickname')"
            />
          </div>

          <label class="field-label">账号</label>
          <div class="input" :class="{ 'has-error': errors.account }">
            <input
              v-model="account"
              type="text"
              placeholder="手机号 / 邮箱"
              @input="errors.account = ''; touched.account = true"
              @blur="checkField('account')"
            />
          </div>

          <label class="field-label">密码</label>
          <div class="input" :class="{ 'has-error': errors.password }">
            <input
              v-model="password"
              :type="showPwd ? 'text' : 'password'"
              placeholder="至少 8 位，含字母和数字"
              @input="errors.password = ''; touched.password = true"
              @blur="checkField('password')"
            />
            <button class="eye-btn" type="button" @click="showPwd = !showPwd">
              <svg viewBox="0 0 20 20" width="20" height="20">
                <path
                  d="M10 4c-4 0-7.3 3.3-8.5 5.5C2.7 11.7 6 15 10 15s7.3-3.3 8.5-5.5C17.3 7.3 14 4 10 4z"
                  fill="none"
                  :stroke="'var(--im-text-muted)'"
                  stroke-width="1.5"
                />
                <circle cx="10" cy="9.5" r="2.5" fill="none" :stroke="'var(--im-text-muted)'" stroke-width="1.5" />
              </svg>
            </button>
          </div>

          <label class="field-label">确认密码</label>
          <div class="input" :class="{ 'has-error': errors.confirmPassword }">
            <input
              v-model="confirmPassword"
              :type="showConfirmPwd ? 'text' : 'password'"
              placeholder="再次输入密码"
              @input="errors.confirmPassword = ''; touched.confirmPassword = true"
              @blur="checkField('confirmPassword')"
            />
            <button class="eye-btn" type="button" @click="showConfirmPwd = !showConfirmPwd">
              <svg viewBox="0 0 20 20" width="20" height="20">
                <path
                  d="M10 4c-4 0-7.3 3.3-8.5 5.5C2.7 11.7 6 15 10 15s7.3-3.3 8.5-5.5C17.3 7.3 14 4 10 4z"
                  fill="none"
                  :stroke="'var(--im-text-muted)'"
                  stroke-width="1.5"
                />
                <circle cx="10" cy="9.5" r="2.5" fill="none" :stroke="'var(--im-text-muted)'" stroke-width="1.5" />
              </svg>
            </button>
          </div>

          <div class="agreement-row">
            <span
              class="checkbox"
              :class="{ checked: agreed, 'has-error': errors.agreement }"
              @click="agreed = !agreed; errors.agreement = ''"
            >
              <svg v-if="agreed" viewBox="0 0 8 6" width="8" height="6">
                <path d="M1 3l2 2 4-4.5" fill="none" stroke="#fff" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
            </span>
            <span class="agreement-text">我已阅读并同意</span>
            <a class="link" href="#">《用户协议》与《隐私政策》</a>
          </div>

          <div v-if="submitError" class="submit-error">{{ submitError }}</div>

          <button class="btn-primary" type="button" @click="handleRegister" :disabled="loading">
            {{ loading ? '注册中…' : '注 册' }}
          </button>

          <div class="goto-row">
            <span>已有账号？</span>
            <a class="link" href="#" @click.prevent="$emit('switch', 'login')">去登录</a>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.window {
  width: 100%;
  height: 100%;
  background: var(--im-surface);
  overflow: hidden;
  display: flex;
  flex-direction: column;
  font-family: var(--im-font-family);
  color: var(--im-text-title);
}

/* 内容区 */
.content {
  flex: 1;
  min-height: 0;
  display: flex;
}

/* 品牌面板 */
.brand-panel {
  width: 380px;
  height: 100%;
  background: linear-gradient(90deg, var(--im-brand-from) 0%, var(--im-brand-to) 100%);
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  padding: 40px;
  color: #fff;
}

.brand-head {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.logo {
  width: 64px;
  height: 64px;
}

.brand-title {
  margin: 0;
  font-size: 36px;
  font-weight: 700;
  line-height: 43px;
  color: #fff;
}

.brand-slogan {
  margin: 0;
  font-size: 15px;
  line-height: 20px;
  color: rgba(255, 255, 255, 0.92);
}

.features {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.feature {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  line-height: 19px;
}

.check {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.18);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

/* 表单面板 */
.form-panel {
  width: 580px;
  height: 100%;
  background: var(--im-surface);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 56px;
}

.form-body {
  width: min(468px, 100%);
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.form-title {
  margin: 0;
  font-size: 28px;
  font-weight: 700;
  line-height: 35px;
  color: var(--im-text-title);
}

.form-subtitle {
  margin: 0;
  font-size: 15px;
  line-height: 19px;
  color: var(--im-text-muted);
}

.field-label {
  font-size: 14px;
  line-height: 17px;
  color: var(--im-text-label);
  font-weight: 500;
}

.input {
  height: 46px;
  padding: 0 14px;
  background: var(--im-surface-2);
  border: 1px solid var(--im-border);
  border-radius: var(--im-radius-card);
  display: flex;
  align-items: center;
  transition: border-color 0.2s;
}

/* 输入框错误态 */
.input.has-error {
  border-color: var(--im-danger);
  box-shadow: 0 0 0 3px rgba(240, 69, 69, 0.12);
}


.input:focus-within {
  border-color: var(--im-primary);
}

.input input {
  flex: 1;
  border: none;
  background: transparent;
  outline: none;
  font-size: 15px;
  line-height: 20px;
  color: var(--im-text-title);
  font-family: inherit;
}

.input input::placeholder {
  color: var(--im-text-muted);
}

.eye-btn {
  width: 20px;
  height: 20px;
  padding: 0;
  border: none;
  background: transparent;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-left: 8px;
  flex-shrink: 0;
}

.agreement-row {
  height: 24px;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  color: var(--im-text-label);
}

.checkbox {
  width: 16px;
  height: 16px;
  border-radius: var(--im-radius-check);
  border: 1px solid var(--im-check-stroke);
  background: transparent;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  flex-shrink: 0;
  transition: border-color 0.2s;
}

/* 未勾选协议时的错误态 */
.checkbox.has-error {
  border-color: var(--im-danger);
  box-shadow: 0 0 0 3px rgba(240, 69, 69, 0.12);
}

.checkbox.checked {
  background: var(--im-primary);
  border-color: var(--im-primary);
}

.agreement-text {
  flex-shrink: 0;
}

.link {
  font-size: 14px;
  line-height: 19px;
  color: var(--im-primary);
  text-decoration: none;
  cursor: pointer;
  flex-shrink: 0;
}

.btn-primary {
  height: 46px;
  border: none;
  border-radius: var(--im-radius-card);
  background: var(--im-primary);
  color: #fff;
  font-size: 16px;
  font-weight: 600;
  letter-spacing: 0.2em;
  cursor: pointer;
  transition: background 0.2s;
}

.btn-primary:hover {
  background: var(--im-primary-hover);
}

.btn-primary:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

/* 提交错误提示 */
.submit-error {
  font-size: 13px;
  line-height: 18px;
  color: var(--im-danger);
  background: rgba(240, 69, 69, 0.08);
  border-radius: 8px;
  padding: 8px 12px;
}

.goto-row {
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  font-size: 14px;
  color: var(--im-text-label);
}
</style>
