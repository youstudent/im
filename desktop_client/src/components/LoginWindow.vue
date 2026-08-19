<script setup>
import { ref, computed, onUnmounted } from 'vue'
import { authApi } from '../api/auth'
import { tokenStore } from '../api/token'
import { generateQR } from '../api/qrcode'
import SliderCaptcha from './SliderCaptcha.vue'

const emit = defineEmits(['switch', 'logged-in'])

const account = ref('')
const password = ref('')
const remember = ref(true)
const showPwd = ref(false)

// 提交状态：loading 期间禁用按钮；error 用于展示服务端/网络错误
const loading = ref(false)
const submitError = ref('')

// ===== 滑动验证码：连续输错密码 3 次后，需先通过验证才能继续登录 =====
const CAPTCHA_THRESHOLD = 3
const failedAttempts = ref(0) // 连续密码错误次数（登录成功清零）
const captchaVerified = ref(false) // 本轮滑动验证是否已通过
const captchaKey = ref(0) // 递增 key：强制重置验证码组件（再次输错后需重新滑动）
const needCaptcha = computed(() => failedAttempts.value >= CAPTCHA_THRESHOLD)
// 需验证但未通过时拦截登录
const captchaBlocking = computed(() => needCaptcha.value && !captchaVerified.value)
function onCaptchaSuccess() {
  captchaVerified.value = true
}

// 表单验证错误信息
const errors = ref({ account: '', password: '' })
const touched = ref({ account: false, password: false })

// 校验账号：支持手机号或邮箱
function validateAccount(v) {
  const val = v ?? account.value
  if (!val) return '请输入账号'
  const isPhone = /^1[3-9]\d{9}$/.test(val)
  const isEmail = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(val)
  if (!isPhone && !isEmail) return '请输入正确的手机号或邮箱'
  return ''
}

// 校验密码
function validatePassword(v) {
  const val = v ?? password.value
  if (!val) return '请输入密码'
  if (val.length < 6) return '密码长度至少 6 位'
  return ''
}

// 单字段校验（失焦/输入时）
function checkField(field) {
  touched.value[field] = true
  if (field === 'account') errors.value.account = validateAccount()
  else if (field === 'password') errors.value.password = validatePassword()
}

// 提交校验 + 真实登录
async function handleLogin() {
  touched.value.account = true
  touched.value.password = true
  errors.value.account = validateAccount()
  errors.value.password = validatePassword()
  if (errors.value.account || errors.value.password) return
  // 触发滑动验证后未通过：拦截登录，引导先完成验证
  if (captchaBlocking.value) {
    submitError.value = '请先完成滑动验证'
    return
  }

  loading.value = true
  submitError.value = ''
  try {
    const data = await authApi.login({
      account: account.value.trim(),
      password: password.value,
    })
    await tokenStore.save({
      accessToken: data.access_token,
      refreshToken: data.refresh_token,
      remember: remember.value,
    })
    // 登录成功：清零错误计数与验证状态
    failedAttempts.value = 0
    captchaVerified.value = false
    emit('logged-in', data.user, data.has_pending_friend_request)
  } catch (e) {
    submitError.value = e.message || '登录失败，请稍后重试'
    // 仅"账号或密码错误"计入连续失败（网络异常/限流不计），达到阈值后强制滑动验证
    if (e && e.message && e.message.includes('账号或密码错误')) {
      failedAttempts.value++
      captchaVerified.value = false
      captchaKey.value++ // 重置滑块，需重新拖动验证
    }
  } finally {
    loading.value = false
  }
}

// ===== 二维码登录 =====
const showQr = ref(false)
// 二维码矩阵（0/1 表示黑白格）
const qrGrid = ref([])
// 二维码状态文案与进度
const qrStatusText = ref('请使用手机扫码后确认登录')
const qrPolling = ref(false)
// 轮询定时器句柄
let qrTimer = null
// 当前二维码 ID
let qrId = ''

// 创建二维码并开始轮询
async function openQR() {
  showQr.value = true
  qrStatusText.value = '请使用手机扫码后确认登录'
  qrPolling.value = true
  qrTimer = null
  try {
    const data = await authApi.createQR()
    qrId = data.qrcode_id
    qrGrid.value = generateQR(data.payload)
    startPolling()
  } catch (e) {
    qrStatusText.value = '二维码获取失败：' + (e.message || '请重试')
    qrPolling.value = false
  }
}

// 关闭二维码弹框并停止轮询
function closeQR() {
  showQr.value = false
  stopPolling()
}

function startPolling() {
  stopPolling()
  qrTimer = setInterval(async () => {
    if (!qrId) return
    try {
      const res = await authApi.pollQR(qrId)
      if (res.status === 'confirmed' && res.login) {
        stopPolling()
        qrStatusText.value = '扫码登录成功'
        const d = res.login
        await tokenStore.save({
          accessToken: d.access_token,
          refreshToken: d.refresh_token,
          remember: remember.value,
        })
        emit('logged-in', d.user, d.has_pending_friend_request)
      } else if (res.status === 'expired') {
        stopPolling()
        qrStatusText.value = '二维码已失效，请刷新'
      }
    } catch (e) {
      // 网络抖动时静默重试，不打断轮询
    }
  }, 1500)
}

function stopPolling() {
  if (qrTimer) {
    clearInterval(qrTimer)
    qrTimer = null
  }
}

// 刷新二维码
async function refreshQr() {
  stopPolling()
  qrStatusText.value = '正在刷新…'
  try {
    const data = await authApi.createQR()
    qrId = data.qrcode_id
    qrGrid.value = generateQR(data.payload)
    qrStatusText.value = '请使用手机扫码后确认登录'
    startPolling()
  } catch (e) {
    qrStatusText.value = '刷新失败：' + (e.message || '请重试')
  }
}

// 组件卸载时停止轮询
onUnmounted(stopPolling)
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
          <h2 class="form-title">欢迎回来</h2>
          <p class="form-subtitle">登录你的 WorkChat 账号以继续</p>

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
              placeholder="请输入密码"
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

          <div class="options-row">
            <label class="remember">
              <span class="checkbox" :class="{ checked: remember }" @click.stop="remember = !remember">
                <svg v-if="remember" viewBox="0 0 8 6" width="8" height="6">
                  <path d="M1 3l2 2 4-4.5" fill="none" stroke="#fff" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
              </span>
              记住我
            </label>
            <a class="link" href="#">忘记密码？</a>
          </div>

          <div v-if="submitError" class="submit-error">{{ submitError }}</div>

          <!-- 连续输错密码 3 次后：必须先通过滑动验证才能登录 -->
          <div v-if="needCaptcha" class="captcha-block">
            <SliderCaptcha :key="captchaKey" @success="onCaptchaSuccess" />
            <p v-if="captchaBlocking" class="captcha-tip">
              已连续 {{ failedAttempts }} 次输错密码，请完成滑动验证后再登录
            </p>
          </div>

          <button class="btn-primary" type="button" @click="handleLogin" :disabled="loading || captchaBlocking">
            {{ loading ? '登录中…' : '登 录' }}
          </button>

          <div class="divider">
            <span class="line"></span>
            <span class="or">或</span>
            <span class="line"></span>
          </div>

          <button class="btn-secondary" type="button" @click="openQR" :disabled="loading">
            <span class="qr-icon">
              <svg viewBox="0 0 20 20" width="20" height="20">
                <rect x="2" y="2" width="6" height="6" rx="1.5" stroke="currentColor" fill="none" stroke-width="1.4" />
                <rect x="12" y="2" width="6" height="6" rx="1.5" stroke="currentColor" fill="none" stroke-width="1.4" />
                <rect x="2" y="12" width="6" height="6" rx="1.5" stroke="currentColor" fill="none" stroke-width="1.4" />
                <rect x="13" y="13" width="4" height="4" rx="1" fill="currentColor" />
              </svg>
            </span>
            二维码登录
          </button>

          <div class="goto-row">
            <span>还没有账号？</span>
            <a class="link" href="#" @click.prevent="$emit('switch', 'register')">立即注册</a>
          </div>
        </div>
      </section>
    </div>
  </div>

  <!-- 二维码登录弹框 -->
  <Teleport to="body">
    <div
      v-if="showQr"
      class="qr-mask"
      role="dialog"
      aria-modal="true"
      aria-labelledby="qr-title"
      @click.self="closeQR"
    >
      <div class="qr-dialog">
        <button class="qr-close" type="button" aria-label="关闭" @click="closeQR">
          <svg viewBox="0 0 20 20" width="18" height="18">
            <path d="M5 5l10 10M15 5L5 15" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
          </svg>
        </button>

        <h2 id="qr-title" class="qr-title">二维码登录</h2>
        <p class="qr-hint">请使用 WorkChat 手机版扫码登录</p>

        <!-- 二维码图案：由后端 payload 生成真实二维码矩阵渲染 -->
        <div class="qr-box" aria-hidden="true">
          <div v-if="qrGrid.length" class="qr-grid">
            <div v-for="(row, r) in qrGrid" :key="'r' + r" class="qr-row">
              <span
                v-for="(cell, c) in row"
                :key="r + '-' + c"
                class="qr-cell"
                :class="{ dark: cell === 1 }"
              ></span>
            </div>
          </div>
          <div v-else class="qr-loading">加载中…</div>
          <div class="qr-logo" aria-hidden="true">
            <svg viewBox="0 0 20 20" width="20" height="20">
              <path
                d="M4 10a6 6 0 0 1 12 0h-2a4 4 0 0 0-8 0H4zm4 0a2 2 0 0 1 4 0h-2a.8.8 0 0 0-1.6 0H8zm1 6l1-3 1 3h-2z"
                fill="var(--im-primary)"
              />
            </svg>
          </div>
        </div>

        <div class="qr-status">
          <span class="status-dot" :class="{ gray: qrStatusText.includes('失效') || qrStatusText.includes('失败') }"></span>
          {{ qrStatusText }}
        </div>

        <button class="qr-refresh" type="button" @click="refreshQr" :disabled="qrPolling && qrStatusText === '正在刷新…'">
          <svg viewBox="0 0 20 20" width="16" height="16">
            <path
              d="M16 4v4h-4M4 16v-4h4M4.5 13a6 6 0 0 0 9.5 2.5M15.5 7A6 6 0 0 0 6 4.5"
              fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"
            />
          </svg>
          刷新二维码
        </button>

        <p class="qr-tip">二维码每 5 分钟自动失效，请及时扫描</p>
      </div>
    </div>
  </Teleport>
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
  gap: 12px;
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

.options-row {
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.remember {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  color: var(--im-text-label);
  cursor: pointer;
}

.checkbox {
  width: 16px;
  height: 16px;
  border-radius: var(--im-radius-check);
  background: transparent;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.checkbox.checked {
  background: var(--im-primary);
}

.link {
  font-size: 14px;
  line-height: 19px;
  color: var(--im-primary);
  text-decoration: none;
  cursor: pointer;
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

.btn-primary:disabled,
.btn-secondary:disabled {
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

/* 滑动验证码区域 */
.captcha-block {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.captcha-tip {
  margin: 0;
  font-size: 12px;
  line-height: 16px;
  color: var(--im-text-muted);
}

.divider {
  height: 24px;
  display: flex;
  align-items: center;
  gap: 12px;
}

.line {
  flex: 1;
  height: 1px;
  background: var(--im-border);
}

.or {
  font-size: 14px;
  color: var(--im-text-muted);
}

.btn-secondary {
  height: 46px;
  border: 1px solid var(--im-border);
  border-radius: var(--im-radius-card);
  background: var(--im-surface-2);
  color: var(--im-text-label);
  font-size: 15px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  cursor: pointer;
  outline: none;
  transition: background 0.18s ease, color 0.18s ease, border-color 0.18s ease,
    box-shadow 0.18s ease, transform 0.08s ease;
}

/* 悬停：背景提亮、文字与图标变品牌色、边框高亮 */
.btn-secondary:hover {
  background: var(--im-surface);
  color: var(--im-primary);
  border-color: rgba(37, 99, 235, 0.4);
  box-shadow: 0 2px 8px rgba(37, 99, 235, 0.12);
}

/* 点击：微缩反馈 */
.btn-secondary:active {
  transform: scale(0.97);
  background: var(--im-hover-gray);
  box-shadow: none;
}

/* 键盘聚焦：可见焦点环 */
.btn-secondary:focus-visible {
  box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.25);
}

.qr-icon {
  width: 20px;
  height: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
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

/* ===== 二维码登录弹框 ===== */
.qr-mask {
  position: fixed;
  inset: 0;
  z-index: 1000;
  background: var(--im-overlay);
  display: flex;
  align-items: center;
  justify-content: center;
  animation: qr-mask-in 0.18s ease;
}

.qr-dialog {
  position: relative;
  width: 320px;
  background: var(--im-surface);
  border-radius: 16px;
  padding: 28px 28px 24px;
  display: flex;
  flex-direction: column;
  align-items: center;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.18);
  animation: qr-dialog-in 0.22s cubic-bezier(0.34, 1.2, 0.64, 1);
}

.qr-close {
  position: absolute;
  top: 14px;
  right: 14px;
  width: 28px;
  height: 28px;
  padding: 0;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--im-text-secondary);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.15s ease, color 0.15s ease;
}

.qr-close:hover {
  background: var(--im-hover-gray);
  color: var(--im-text-title);
}

.qr-title {
  margin: 0;
  font-size: 1.143rem;
  font-weight: 700;
  color: var(--im-text-title);
}

.qr-hint {
  margin: 6px 0 18px;
  font-size: 0.857rem;
  color: var(--im-text-secondary);
}

/* 二维码容器 */
.qr-box {
  position: relative;
  width: 200px;
  height: 200px;
  border: 1px solid var(--im-border);
  border-radius: 12px;
  padding: 12px;
  background: #fff;
  box-sizing: border-box;
}

.qr-grid {
  display: flex;
  flex-direction: column;
  gap: 1px;
}

/* 二维码加载占位 */
.qr-loading {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 0.857rem;
  color: var(--im-text-muted);
}

.qr-row {
  display: flex;
  gap: 1px;
}

.qr-cell {
  width: 8px;
  height: 8px;
  border-radius: 1px;
  background: #fff;
  flex-shrink: 0;
}

.qr-cell.dark {
  background: #111827;
}

/* 中央 logo */
.qr-logo {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  width: 40px;
  height: 40px;
  background: #fff;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 0 0 4px #fff;
}

/* 状态提示 */
.qr-status {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 16px;
  font-size: 0.857rem;
  color: var(--im-text-secondary);
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #22c55e;
  animation: status-pulse 1.6s ease-in-out infinite;
}

/* 失效/失败状态：灰色停止闪烁 */
.status-dot.gray {
  background: var(--im-text-muted);
  animation: none;
}

/* 刷新按钮 */
.qr-refresh {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 16px;
  padding: 6px 14px;
  background: transparent;
  border: 1px solid var(--im-border);
  border-radius: 8px;
  font-family: inherit;
  font-size: 0.857rem;
  color: var(--im-text-label);
  cursor: pointer;
  transition: background 0.15s ease, color 0.15s ease;
}

.qr-refresh:hover {
  background: var(--im-surface-2);
  color: var(--im-text-title);
}

.qr-tip {
  margin: 14px 0 0;
  font-size: 0.786rem;
  color: var(--im-text-muted);
}

@keyframes qr-mask-in {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

@keyframes qr-dialog-in {
  from {
    opacity: 0;
    transform: scale(0.92);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}

@keyframes status-pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.4;
  }
}

/* 减少动画偏好 */
@media (prefers-reduced-motion: reduce) {
  .qr-mask,
  .qr-dialog,
  .status-dot {
    animation: none !important;
  }
}
</style>
