import axios from 'axios'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useMutation } from '@tanstack/react-query'
import { authApi, statusApi } from '@/api/core'
import { useAuthStore } from '@/stores/authStore'
import ArrowRight from 'lucide-react/dist/esm/icons/arrow-right'
import Loader2 from 'lucide-react/dist/esm/icons/loader-2'
import Lock from 'lucide-react/dist/esm/icons/lock'
import Play from 'lucide-react/dist/esm/icons/play'
import User from 'lucide-react/dist/esm/icons/user'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Label } from '@/components/ui/Label'

const LoginPage = () => {
  const navigate = useNavigate()
  const setAuthenticated = useAuthStore((state) => state.setAuthenticated)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')

  const loginMutation = useMutation({
    mutationFn: async () => {
      await authApi.login(username, password)
      // 로그인 성공 직후 세션 검증 (2-2)
      await statusApi.get()
    },
    onSuccess: () => {
      setAuthenticated(true)
      void navigate('/dashboard/stats')
    },
    onError: (err: unknown) => {
      if (axios.isAxiosError(err)) {
        if (err.response?.status === 429) {
          setError('너무 많은 로그인 시도가 감지되었습니다. 15분 후 다시 시도해주세요.')
          return
        }
        if (err.response?.status && err.response.status >= 500) {
          setError('서버 오류가 발생했습니다. 잠시 후 다시 시도해주세요.')
          return
        }
      }
      setError(err instanceof Error ? err.message : '로그인에 실패했습니다')
    },
  })

  const handleSubmit = (e: React.SyntheticEvent<HTMLFormElement>) => {
    e.preventDefault()
    setError('')

    if (!username || !password) {
      setError('아이디와 비밀번호를 입력해주세요')
      return
    }

    void (async () => {
      try {
        await loginMutation.mutateAsync()
      } catch {
        // handled in onError
      }
    })()
  }

  return (
    <div className="min-h-screen w-full flex items-center justify-center relative overflow-hidden bg-slate-50 font-display selection:bg-sky-200">
      {/* Dynamic Background with Hololive Colors */}
      <div className="absolute inset-0 bg-white z-0">
        <div className="absolute top-0 left-0 right-0 h-[500px] bg-linear-to-b from-sky-100/50 to-transparent"></div>
        <div className="absolute -top-24 right-0 w-[500px] h-[500px] bg-sky-200/30 rounded-full blur-[100px] animate-pulse"></div>
        <div className="absolute top-1/2 -left-24 w-[400px] h-[400px] bg-cyan-100/40 rounded-full blur-[80px]"></div>
      </div>

      <div className="w-full max-w-[400px] z-10 px-6">
        <div className="relative">
          {/* Logo Section */}
          <div className="text-center mb-10">
            <div className="inline-flex items-center justify-center w-16 h-16 bg-gradient-to-tr from-sky-400 to-cyan-400 rounded-2xl shadow-lg shadow-sky-200 mb-6 transform rotate-3 hover:rotate-6 transition-transform duration-300">
              <Play className="w-8 h-8 text-white fill-white ml-1" aria-hidden="true" />
            </div>

            <h1 className="text-2xl font-bold text-slate-800 tracking-tight">
              Hololive Bot <span className="text-sky-500">Console</span>
            </h1>
          </div>

          {/* Login Form */}
          <form onSubmit={handleSubmit} className="space-y-5">
            <div className="space-y-4">
              <div className="group relative">
                <Label htmlFor="username" className="sr-only">Username</Label>
                <div className="absolute inset-y-0 left-0 pl-4 flex items-center pointer-events-none text-slate-400 group-focus-within:text-sky-500 transition-colors z-10">
                  <User size={18} aria-hidden="true" />
                </div>
                <Input
                  id="username"
                  type="text"
                  name="username"
                  autoComplete="username"
                  value={username}
                  onChange={(e) => { setUsername(e.target.value); }}
                  className="pl-11 pr-4 py-6 bg-white border-slate-200 rounded-xl text-slate-800 placeholder-slate-400 focus-visible:border-sky-400 focus-visible:ring-4 focus-visible:ring-sky-100 transition-colors shadow-sm font-medium"
                  placeholder="Username"
                />
              </div>

              <div className="group relative">
                <Label htmlFor="password" className="sr-only">Password</Label>
                <div className="absolute inset-y-0 left-0 pl-4 flex items-center pointer-events-none text-slate-400 group-focus-within:text-sky-500 transition-colors z-10">
                  <Lock size={18} aria-hidden="true" />
                </div>
                <Input
                  id="password"
                  type="password"
                  name="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => { setPassword(e.target.value); }}
                  className="pl-11 pr-4 py-6 bg-white border-slate-200 rounded-xl text-slate-800 placeholder-slate-400 focus-visible:border-sky-400 focus-visible:ring-4 focus-visible:ring-sky-100 transition-colors shadow-sm font-medium"
                  placeholder="Password"
                />
              </div>
            </div>

            {error && (
              <div
                role="alert"
                aria-live="polite"
                className="text-rose-500 text-sm bg-rose-50 px-4 py-3 rounded-xl border border-rose-100 flex items-center font-medium"
              >
                <div className="w-1.5 h-1.5 rounded-full bg-rose-500 mr-2.5" />
                {error}
              </div>
            )}

            <Button
              type="submit"
              disabled={loginMutation.isPending}
              className="w-full relative overflow-hidden flex justify-center items-center py-6 px-4 bg-slate-900 rounded-xl text-sm font-bold text-white hover:bg-slate-800 focus-visible:ring-4 focus-visible:ring-slate-200 disabled:opacity-70 disabled:cursor-not-allowed transition-colors shadow-xl shadow-slate-200 group"
            >
              <div className="relative z-10 flex items-center justify-center">
                {loginMutation.isPending ? (
                  <>
                    <div className="animate-spin mr-2">
                      <Loader2 className="h-5 w-5" aria-hidden="true" />
                    </div>
                    Connecting…
                  </>
                ) : (
                  <>
                    Sign In
                    <span className="inline-flex ml-2">
                      <ArrowRight className="h-4 w-4 transition-transform duration-300 group-hover:translate-x-1" aria-hidden="true" />
                    </span>
                  </>
                )}
              </div>
            </Button>
          </form>

          {/* Footer */}
          <div className="mt-12 text-center space-y-2">
            <div className="flex justify-center space-x-2">
              <div className="w-1.5 h-1.5 rounded-full bg-sky-400"></div>
              <div className="w-1.5 h-1.5 rounded-full bg-cyan-400"></div>
              <div className="w-1.5 h-1.5 rounded-full bg-teal-400"></div>
            </div>
            <p className="text-xs text-slate-400 font-medium tracking-wide">
              AUTHORIZED PERSONNEL ONLY
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}

export default LoginPage
