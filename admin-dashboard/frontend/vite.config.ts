import { defineConfig } from 'vite'
import react, { reactCompilerPreset } from '@vitejs/plugin-react'
import babel from '@rolldown/plugin-babel'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

const babelOptions = {
  include: /[\\/]src[\\/].*\.[jt]sx?$/,
  presets: [reactCompilerPreset({ target: '19' })],
  sourceMap: true,
} as unknown as Parameters<typeof babel>[0]
const adminApiProxyTarget = process.env.ADMIN_DASHBOARD_PROXY_TARGET ?? 'http://localhost:30190'

// https://vite.dev/config/
export default defineConfig({
  // P0: Barrel file imports 최적화 (lucide-react 1,583 모듈 → 필요한 것만)
  optimizeDeps: {
    include: [
      'lucide-react',
    ],
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
      '@/components': path.resolve(__dirname, './src/components'),
      '@/pages': path.resolve(__dirname, './src/pages'),
      '@/api': path.resolve(__dirname, './src/api'),
      '@/stores': path.resolve(__dirname, './src/stores'),
      '@/types': path.resolve(__dirname, './src/types'),
      '@/lib': path.resolve(__dirname, './src/lib'),
      '@/hooks': path.resolve(__dirname, './src/hooks'),
      '@/config': path.resolve(__dirname, './src/config'),
      '@/layouts': path.resolve(__dirname, './src/layouts'),
      '@/utils': path.resolve(__dirname, './src/utils'),
    },
  },
  plugins: [
    tailwindcss(),
    // Vite 8: Oxc 기반 React Refresh (Babel 제거)
    react(),
    // React Compiler는 @rolldown/plugin-babel 경유로 적용
    babel(babelOptions),
  ],
  build: {
    target: 'esnext',
    sourcemap: false,
    cssCodeSplit: true,
    // Vite 8: Rolldown 기반 code splitting (manualChunks 제거됨)
    rolldownOptions: {
      checks: {
        pluginTimings: false,
      },
      output: {
        codeSplitting: {
          groups: [
            {
              // 라우팅
              name: 'vendor-router',
              test: /react-router-dom/,
            },
            {
              // 데이터 fetching
              name: 'vendor-query',
              test: /@tanstack\/react-query/,
            },
            {
              // 아이콘
              name: 'vendor-icons',
              test: /lucide-react/,
            },
          ],
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/admin/api': {
        target: adminApiProxyTarget,
        changeOrigin: true,
      },
    },
  },
})
