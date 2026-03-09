import axios, { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { useAuthStore } from '@/stores/authStore'
import { CONFIG } from '@/config/constants'

const unsafeMethods = new Set(['post', 'put', 'delete', 'patch'])

// getCookie: document.cookie에서 특정 쿠키 값을 추출
function getCookie(name: string): string | null {
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = document.cookie.match(new RegExp(`(?:^|; )${escaped}=([^;]*)`))
  return m ? decodeURIComponent(m[1] ?? '') : null
}

function setRequestHeader(config: InternalAxiosRequestConfig, name: string, value: string): void {
  const headers = config.headers instanceof AxiosHeaders
    ? config.headers
    : AxiosHeaders.from(config.headers)

  headers.set(name, value)
  config.headers = headers
}

// API 클라이언트 생성
const apiClient = axios.create({
  baseURL: CONFIG.api.baseUrl,
  withCredentials: true,
  headers: {
    'Content-Type': 'application/json',
  },
  timeout: CONFIG.api.timeoutMs,
})

// Request interceptor: 민감한 정보 URL 파라미터 방지
apiClient.interceptors.request.use((config: InternalAxiosRequestConfig) => {
  // 민감한 정보 URL 파라미터 방지
  if (config.params != null && typeof config.params === 'object') {
    const params = config.params as Record<string, unknown>
    delete params['password']
    delete params['token']
  }

  // CSRF: 상태 변경 요청에 csrf_token 쿠키를 헤더로 전달 (Signed Double Submit Cookie)
  const method = (config.method ?? 'get').toLowerCase()
  if (unsafeMethods.has(method)) {
    const token = getCookie('csrf_token')
    if (token != null && token !== '') {
      setRequestHeader(config, 'X-CSRF-Token', token)
    }
  }

  return config
})

// Response interceptor: \uc5d0\ub7ec \ubc0f \uc778\uc99d \ucc98\ub9ac
apiClient.interceptors.response.use(
  (response) => response,
  (error: AxiosError<{ retry_after?: number }>) => {
    if (axios.isAxiosError(error)) {
      if (error.response?.status === 401) {
        // React 컴포넌트 외부에서 스토어 접근
        useAuthStore.getState().logout()

        // 로그인 페이지로 리다이렉트 (이미 로그인 페이지가 아닌 경우)
        if (window.location.pathname !== '/login') {
          window.location.href = '/login'
        }
      } else if (error.response?.status === 429) {
        // Rate limit 처리
        const retryAfter = error.response.data.retry_after ??
          (typeof error.response.headers['retry-after'] === 'string'
            ? parseInt(error.response.headers['retry-after'], 10)
            : undefined)
        console.warn(`Rate limited. Retry after ${String(retryAfter)}s`)
      }
    }
    return Promise.reject(error)
  }
)

export default apiClient
