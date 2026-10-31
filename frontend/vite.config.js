import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  base: './',                       // 相对路径,便于被后端任意挂载
  server: {
    proxy: { '/api': 'http://127.0.0.1:2017' },   // dev 时转发到 uvicorn
  },
  test: { environment: 'node' },
})
