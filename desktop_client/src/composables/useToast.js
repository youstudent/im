// 轻量顶部 toast：保存成功/失败等统一提示（替代原生 alert）
import { ref } from 'vue'

export function useToast() {
  const toastState = ref(null) // { text, type: success | error | info }
  let toastTimer = null

  function showToast(text, type = 'info') {
    toastState.value = { text, type }
    if (toastTimer) clearTimeout(toastTimer)
    toastTimer = setTimeout(() => {
      toastState.value = null
    }, 3200)
  }

  return { toastState, showToast }
}
