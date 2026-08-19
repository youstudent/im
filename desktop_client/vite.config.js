const { defineConfig } = require('vite')
const vue = require('@vitejs/plugin-vue')

// https://vitejs.dev/config/
// 采用 CommonJS 配置：项目未声明 "type": "module"，
// 因此 vite.config.js、electron/main.js 均以 CJS 运行，避免 ESM/CJS 歧义。
module.exports = defineConfig({
  // 相对路径，保证打包后通过 file:// 加载 dist/index.html 时资源路径正确
  base: './',
  plugins: [vue()],
  server: {
    port: 5173,
    strictPort: true, // 端口被占用时直接失败而非自动切换，配合 wait-on 更稳定
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
  },
})
