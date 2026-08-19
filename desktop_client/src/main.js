import { createApp } from 'vue'
import App from './App.vue'
import './styles/im-tokens.css'

/**
 * 在挂载前应用已保存的主题，避免刷新时从浅色闪一下再到深色。
 * 优先级：localStorage > 系统偏好 > light
 */
const THEME_KEY = 'workchat:theme'
const FONT_SIZE_KEY = 'workchat:font-size'
function applyInitialTheme() {
  try {
    const saved = window.localStorage.getItem(THEME_KEY)
    let theme = saved === 'light' || saved === 'dark' ? saved : null
    if (!theme && window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
      theme = 'dark'
    }
    if (theme === 'dark') {
      document.documentElement.setAttribute('data-theme', 'dark')
    }
  } catch (e) {
    /* 忽略 localStorage 访问异常 */
  }
}
function applyInitialFontSize() {
  try {
    const saved = window.localStorage.getItem(FONT_SIZE_KEY)
    if (saved === 'small' || saved === 'large') {
      document.documentElement.setAttribute('data-font-size', saved)
    }
    // medium 视为默认，无需设置属性
  } catch (e) {
    /* 忽略 */
  }
}
applyInitialTheme()
applyInitialFontSize()

createApp(App).mount('#app')
