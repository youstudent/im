// 全局 UI 状态：确认弹窗 + 操作结果 Toast
// 由 App.vue 渲染对应 DOM，业务子组件通过 useUi() 触发。
import { reactive } from 'vue'

// 全局单例状态（避免各组件各持一份）
const state = reactive({
  confirm: null, // { title, message, confirmText, danger, onConfirm }
  toast: null,   // { message, type }
})

let confirmResolve = null
let toastTimer = null

// 打开确认弹窗，返回 Promise<boolean>（用户点击确认/取消）
function openConfirm(opts) {
  state.confirm = {
    title: opts.title || '请确认',
    message: opts.message || '',
    confirmText: opts.confirmText || '确 定',
    danger: !!opts.danger,
    onConfirm: opts.onConfirm || null,
  }
  return new Promise((resolve) => {
    confirmResolve = resolve
  })
}

function closeConfirm(result) {
  state.confirm = null
  confirmResolve && confirmResolve(result)
  confirmResolve = null
}

// 执行确认回调并关闭
function runConfirm() {
  const action = state.confirm && state.confirm.onConfirm
  if (action) action()
  closeConfirm(true)
}

// 打开提示 Toast（3 秒自动消失，可点击关闭）
function showToast(message, type = 'error') {
  state.toast = { message, type }
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { state.toast = null }, 3000)
}

function closeToast() {
  if (toastTimer) clearTimeout(toastTimer)
  state.toast = null
}

export function useUi() {
  return {
    // 状态（供 App.vue 渲染）
    state,
    openConfirm,
    closeConfirm,
    runConfirm,
    showToast,
    closeToast,
  }
}
