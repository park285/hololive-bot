import { defineConfig } from 'vite'
import react, { reactCompilerPreset } from '@vitejs/plugin-react'
import babel from '@rolldown/plugin-babel'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'
import fs from 'node:fs'
import { createHash } from 'node:crypto'

const babelOptions = {
  include: /[\\/]src[\\/].*\.[jt]sx?$/,
  presets: [reactCompilerPreset({ target: '19' })],
  sourceMap: true,
} as unknown as Parameters<typeof babel>[0]
const adminApiProxyTarget = process.env.ADMIN_DASHBOARD_PROXY_TARGET ?? 'http://localhost:30190'

export default defineConfig({
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
    react(),
    babel(babelOptions),
    {
      name: 'admin-build-inventory',
      apply: 'build',
      generateBundle() {
        // production의 public 파일 계약은 이 두 자산이며 개발용 MSW worker는 포함하지 않습니다.
        for (const file of ['favicon.svg', 'theme-init.js']) this.emitFile({ type: 'asset', fileName: file, source: fs.readFileSync(path.resolve(__dirname, 'public', file)) })
      },
      writeBundle(_options, bundle) {
        // 출시 검증용 graph·hash는 node_modules 아래에만 기록하며 공개 자산에 포함하지 않습니다.
        const sha256 = (value: string | Uint8Array) => createHash('sha256').update(value).digest('hex')
        const sourcePaths = [...this.getModuleIds()]
          .filter(id => id.startsWith(`${__dirname}/src/`) && fs.existsSync(id))
          .map(id => path.relative(__dirname, id))
        const inputs = Object.fromEntries([...new Set([...sourcePaths, 'index.html', 'public/favicon.svg', 'public/theme-init.js', 'vite.config.ts', 'package.json', 'package-lock.json', '../backend/internal/contract/openapi.json'])]
          .sort().map(file => [file, sha256(fs.readFileSync(path.resolve(__dirname, file)))]))
        const files = Object.fromEntries(Object.entries(bundle).sort(([a], [b]) => a.localeCompare(b)).map(([name, output]) => [name, {
          sha256: sha256(fs.readFileSync(path.resolve(__dirname, 'dist', name))),
          modules: output.type === 'chunk' ? Object.keys(output.modules).map(id => path.relative(__dirname, id)).sort() : [],
        }]))
        const destination = path.resolve(__dirname, 'node_modules/.cache/admin-build-inventory.json')
        fs.mkdirSync(path.dirname(destination), { recursive: true })
        fs.writeFileSync(destination, `${JSON.stringify({ schema_version: 1, inputs, files }, null, 2)}\n`)
      },
    },
  ],
  build: {
    copyPublicDir: false,
    // Native import가 의존 JS 준비를 보장하며 Vite는 분리 CSS의 선행 로딩을 유지합니다(C07).
    modulePreload: false,
    target: 'esnext',
    sourcemap: false,
    cssCodeSplit: true,
    rolldownOptions: {
      checks: {
        pluginTimings: false,
      },
      output: {
        codeSplitting: {
          groups: [
            {
              name: 'vendor-router',
              test: /react-router/,
            },
            {
              name: 'vendor-query',
              test: /@tanstack\/react-query/,
            },
            {
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
      '/admin/meta.json': { target: adminApiProxyTarget, changeOrigin: true },
      '/admin/api': {
        target: adminApiProxyTarget,
        changeOrigin: true,
        ws: true,
      },
    },
  },
})
