import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import checker from 'vite-plugin-checker'
import { resolve } from 'path'


export default defineConfig({
  plugins: [
    vue(),
    checker({
      typescript: true,
      vueTsc: true
    })
  ],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      // 使用 vue-i18n 运行时版本，避免 CSP unsafe-eval 问题
      'vue-i18n': 'vue-i18n/dist/vue-i18n.runtime.esm-bundler.js'
    }
  },
  define: {
    // 启用 vue-i18n JIT 编译，在 CSP 环境下处理消息插值
    // JIT 编译器生成 AST 对象而非 JS 代码，无需 unsafe-eval
    __INTLIFY_JIT_COMPILATION__: true
  },
  build: {
    outDir: '../backend/internal/web/dist',
    emptyOutDir: true
  },
  server: {
    host: '0.0.0.0',
    port: Number(process.env.VITE_DEV_PORT || 3000),
    proxy: {
      '/api': {
        target: process.env.VITE_DEV_PROXY_TARGET || 'http://localhost:8080',
        changeOrigin: true
      },
      '/setup': {
        target: process.env.VITE_DEV_PROXY_TARGET || 'http://localhost:8080',
        changeOrigin: true
      }
    }
  }
})
