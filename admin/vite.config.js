import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 管理后台：默认监听 5174，代理 /api 到 Go 服务端（含 /api/admin）
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5174,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
})
