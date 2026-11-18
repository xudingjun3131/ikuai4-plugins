import path from 'node:path'
import { fileURLToPath } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

const rootDir = path.dirname(fileURLToPath(import.meta.url))

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  // [plugin-proxy] iKuai 加固: WebUI 经同源 /plugin_proxy/runhooks/ 代理,
  // 资源路径(Vite base)与 /api/* 请求(api-base 包装 fetch/EventSource)都带前缀。
  base: "/plugin_proxy/runhooks/",
  resolve: {
    alias: {
      '@': path.resolve(rootDir, './src'),
    },
  },
})
