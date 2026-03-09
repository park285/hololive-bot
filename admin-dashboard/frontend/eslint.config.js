import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist', 'src/api/generated/**']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommendedTypeChecked,
      tseslint.configs.strictTypeChecked,
      reactRefresh.configs.vite,
    ],
    plugins: {
      'react-hooks': reactHooks,
    },
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
      parserOptions: {
        project: ['./tsconfig.app.json', './tsconfig.node.json'],
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      // React Hooks 규칙
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',

      // ANY 금지 및 타입 우회 엄금
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/no-unsafe-assignment': 'error',
      '@typescript-eslint/no-unsafe-member-access': 'error',
      '@typescript-eslint/no-unsafe-call': 'error',
      '@typescript-eslint/no-unsafe-return': 'error',
      '@typescript-eslint/no-unsafe-argument': 'error',
      '@typescript-eslint/no-non-null-assertion': 'error',
      '@typescript-eslint/no-unnecessary-type-assertion': 'error',
      '@typescript-eslint/prefer-as-const': 'error',
      '@typescript-eslint/consistent-type-assertions': [
        'error',
        {
          assertionStyle: 'as',
          objectLiteralTypeAssertions: 'never',
        },
      ],
      '@typescript-eslint/no-inferrable-types': 'off',
      // 이벤트 핸들러 내 비동기 호출은 try-catch로 처리하므로 경고로 완화
      '@typescript-eslint/no-floating-promises': 'warn',

      // ES6 규칙 준수 (var 금지, const/let 사용)
      'no-var': 'error',
      'prefer-const': 'error',
      'prefer-arrow-callback': 'error',
      'prefer-template': 'error',
      'prefer-destructuring': ['error', {
        array: true,
        object: true,
      }],
      'prefer-spread': 'error',
      'prefer-rest-params': 'error',
      'object-shorthand': ['error', 'always'],
      'no-useless-constructor': 'off',
      '@typescript-eslint/no-useless-constructor': 'error',
      'prefer-object-spread': 'error',
      'no-duplicate-imports': 'error',
      'arrow-body-style': ['error', 'as-needed'],
      'arrow-spacing': 'error',
      'template-curly-spacing': ['error', 'never'],
      'rest-spread-spacing': ['error', 'never'],
    },
  },
])
