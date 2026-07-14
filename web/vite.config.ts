import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import path from 'node:path'

function elementPlusChunk(id: string) {
  const normalized = id.replace(/\\/g, '/')
  if (normalized.includes('/node_modules/@element-plus/icons-vue/')) return 'element-icons'
  if (normalized.includes('/node_modules/element-plus/')) return 'element-plus'
  return undefined
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const apiBase = env.VITE_API_BASE || 'http://localhost:8080'
  return {
    resolve: {
      alias: {
        '@': path.resolve(__dirname, 'src'),
      },
    },
    server: {
      host: '0.0.0.0',
      port: 5173,
      proxy: {
        // 开发期统一经本地代理,避免 CORS;生产由 nginx / ingress 承担
        '/api': { target: apiBase, changeOrigin: true },
        '/v1': { target: apiBase, changeOrigin: true },
        '/p/img': { target: apiBase, changeOrigin: true },
        '/p/vwf': { target: apiBase, changeOrigin: true },
        '/site-assets': { target: apiBase, changeOrigin: true },
        '/healthz': { target: apiBase, changeOrigin: true },
      },
    },
    plugins: [
      vue(),
      AutoImport({
        imports: ['vue', 'vue-router', 'pinia', '@vueuse/core'],
        resolvers: [ElementPlusResolver()],
        dts: 'src/auto-imports.d.ts',
      }),
      Components({
        resolvers: [ElementPlusResolver()],
        dts: 'src/components.d.ts',
      }),
    ],
    build: {
      outDir: 'dist',
      sourcemap: false,
      chunkSizeWarningLimit: 800,
      rollupOptions: {
        output: {
          /**
           * 手工拆包,避免图标全集沉到页面入口。
           * - element-icons:@element-plus/icons-vue 图标全集,独立缓存。
           * - Element Plus 组件由自动导入和 Rollup 按实际页面依赖拆分。
           * - vue-core:vue / vue-router / pinia / @vueuse,运行时核心。
           * - vendor:其它 node_modules(axios、dayjs 等)。
           */
          manualChunks(id) {
            if (!id.includes('node_modules')) return
            const elementChunk = elementPlusChunk(id)
            if (elementChunk) return elementChunk
            if (
              id.includes('/vue/') ||
              id.includes('/@vue/') ||
              id.includes('/vue-router/') ||
              id.includes('/pinia') ||
              id.includes('/@vueuse/')
            ) return 'vue-core'
            return 'vendor'
          },
        },
      },
    },
  }
})
