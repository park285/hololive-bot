import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

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
    react({
      babel: {
        plugins: [['babel-plugin-react-compiler', { target: '19' }]],
      },
    }),
  ],
  build: {
    target: 'esnext',
    sourcemap: false,
    cssCodeSplit: true,
    minify: 'esbuild',
    rollupOptions: {
      output: {
        manualChunks: {
          // 라우팅
          'vendor-router': ['react-router-dom'],
          // 데이터 fetching
          'vendor-query': ['@tanstack/react-query'],
          // 아이콘
          'vendor-icons': ['lucide-react'],
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/admin/api': {
        target: 'http://localhost:30001',
        changeOrigin: true,
      },
    },
  },
})
