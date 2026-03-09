import { lazy, Suspense } from 'react'
import { createBrowserRouter, Navigate, RouterProvider } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Loader2 from 'lucide-react/dist/esm/icons/loader-2'
import { CONFIG } from '@/config'
import { useActivityDetection } from '@/hooks/useActivityDetection'
import { useHeartbeat } from '@/hooks/useHeartbeat'
import { getErrorMessageFromUnknown } from '@/lib/typeUtils'
import toast, { Toaster } from '@/lib/toast'
import { useAuthStore } from '@/stores/authStore'
import { getLazyComponent, ROUTE_DEFINITIONS } from '@/routes/route-definitions'

const LoginPage = lazy(() => import('@/pages/LoginPage'))
const AppLayout = lazy(() => import('@/layouts/AppLayout').then((module) => ({ default: module.AppLayout })))
const ErrorPage = lazy(() => import('@/components/ErrorPage'))

const TabLoader = () => (
  <div className="flex h-64 items-center justify-center text-slate-400">
    <div className="animate-spin mr-2">
      <Loader2 className="h-6 w-6" />
    </div>
    <span className="text-sm font-medium">로딩 중…</span>
  </div>
)

const FullPageLoader = () => (
  <div className="flex min-h-screen items-center justify-center bg-slate-50 text-slate-400">
    <div className="animate-spin mr-2">
      <Loader2 className="h-6 w-6" />
    </div>
    <span className="text-sm font-medium">페이지를 준비 중…</span>
  </div>
)

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 1000 * 60 * 5,
      gcTime: 1000 * 60 * 60,
      retry: 1,
      refetchOnWindowFocus: false,
    },
    mutations: {
      retry: 0,
      onError: (error: Error) => {
        toast.error(getErrorMessageFromUnknown(error))
      },
    },
  },
})

const ProtectedRoute = ({ children }: { children: React.ReactNode }) => {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated)
  const isIdle = useActivityDetection(CONFIG.heartbeat.idleTimeoutMs)

  useHeartbeat(isIdle)

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  return <>{children}</>
}

const LazyRoute = ({ children }: { children: React.ReactNode }) => (
  <Suspense fallback={<TabLoader />}>
    {children}
  </Suspense>
)

const LoginRoute = () => (
  <Suspense fallback={<FullPageLoader />}>
    <LoginPage />
  </Suspense>
)

const DashboardShellRoute = () => (
  <Suspense fallback={<FullPageLoader />}>
    <AppLayout />
  </Suspense>
)

const RouteErrorElement = () => (
  <Suspense fallback={<FullPageLoader />}>
    <ErrorPage />
  </Suspense>
)

const router = createBrowserRouter([
  {
    path: '/login',
    element: <LoginRoute />,
    errorElement: <RouteErrorElement />,
  },
  {
    path: '/dashboard',
    element: (
      <ProtectedRoute>
        <DashboardShellRoute />
      </ProtectedRoute>
    ),
    errorElement: <RouteErrorElement />,
    children: [
      {
        index: true,
        element: <Navigate to="stats" replace />,
      },
      ...ROUTE_DEFINITIONS.map((route) => {
        const Component = getLazyComponent(route.id)
        return {
          path: route.path,
          element: (
            <LazyRoute>
              <Component />
            </LazyRoute>
          ),
        }
      }),
    ],
  },
  {
    path: '/',
    element: <Navigate to="/dashboard" replace />,
    errorElement: <RouteErrorElement />,
  },
  {
    path: '*',
    element: <Navigate to="/dashboard" replace />,
  },
])

const toastOptions = {
  className: 'text-sm font-medium',
  style: {
    background: '#ffffff',
    color: '#334155',
    padding: '12px 16px',
    borderRadius: '12px',
    boxShadow: '0 10px 15px -3px rgba(0, 0, 0, 0.1), 0 4px 6px -2px rgba(0, 0, 0, 0.05)',
    border: '1px solid #f1f5f9',
  },
  success: {
    iconTheme: { primary: '#0ea5e9', secondary: '#ffffff' },
  },
  error: {
    iconTheme: { primary: '#ef4444', secondary: '#ffffff' },
  },
}

const App = () => (
  <QueryClientProvider client={queryClient}>
    <Toaster position="top-center" reverseOrder={false} toastOptions={toastOptions} />
    <RouterProvider router={router} />
  </QueryClientProvider>
)

export default App
