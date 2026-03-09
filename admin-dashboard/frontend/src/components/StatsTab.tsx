import { useState, useEffect, lazy, Suspense } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { statusApi } from '@/api/core'
import { statsApi } from '@/api/holo'
import { queryKeys } from '@/api/queryKeys'
import { Button } from '@/components/ui/Button'
import { StatCard } from '@/components/ui/StatCard'
import Users from 'lucide-react/dist/esm/icons/users'
import Bell from 'lucide-react/dist/esm/icons/bell'
import MessageSquare from 'lucide-react/dist/esm/icons/message-square'
import Loader2 from 'lucide-react/dist/esm/icons/loader-2'
import ArrowRight from 'lucide-react/dist/esm/icons/arrow-right'
import Activity from 'lucide-react/dist/esm/icons/activity'
import Server from 'lucide-react/dist/esm/icons/server'
import Play from 'lucide-react/dist/esm/icons/play'
import Bot from 'lucide-react/dist/esm/icons/bot'
import ShieldCheck from 'lucide-react/dist/esm/icons/shield-check'
import ChevronDown from 'lucide-react/dist/esm/icons/chevron-down'

// Lazy load heavy dashboard components
const SystemStatsChart = lazy(() => import('@/components/dashboard/SystemStatsChart').then(m => ({ default: m.SystemStatsChart })))
const ChannelStatsTable = lazy(() => import('@/components/dashboard/ChannelStatsTable').then(m => ({ default: m.ChannelStatsTable })))

const ComponentLoader = () => (
    <div className="flex items-center justify-center h-48 text-slate-400 w-full bg-slate-50/50 rounded-lg">
        <Loader2 className="w-6 h-6 animate-spin mr-2" />
        <span className="text-sm">로딩 중…</span>
    </div>
)

const StatsTab = () => {
  const navigate = useNavigate()
  const [selectedService, setSelectedService] = useState('hololive-bot')

  // 1. Holo Bot 통계
  const { data: holoStats, isLoading: isHoloLoading, isError: isHoloError, refetch: refetchHolo } = useQuery({
    queryKey: queryKeys.stats.summary,
    queryFn: statsApi.get,
    staleTime: 1000 * 30,
    refetchInterval: () => document.visibilityState === 'visible' ? 30000 : false,
  })

  // 2. 통합 시스템 상태
  const { data: statusData, isLoading: isStatusLoading, isError: isStatusError, refetch: refetchStatus } = useQuery({
    queryKey: queryKeys.status.aggregated,
    queryFn: statusApi.get,
    staleTime: 1000 * 15,
    refetchInterval: () => document.visibilityState === 'visible' ? 15000 : false,
  })

  // 초기 로딩 완료 시 기본 서비스가 목록에 없으면 첫 번째 서비스 선택 (P1: dependency 최적화)
  useEffect(() => {
    if (statusData?.services && statusData.services.length > 0) {
      setSelectedService(prev => {
        const exists = statusData.services.find(s => s.name === prev)
        if (exists) return prev // 기존 선택 유지
        // 기본적으로 hololive-bot을 찾고, 없으면 첫 번째 것 선택
        const defaultSvc = statusData.services.find(s => s.name === 'hololive-bot')
        return defaultSvc?.name ?? statusData.services[0]?.name ?? prev
      })
    }
  }, [statusData]) // P1: selectedService 제거 - functional setState로 자기참조 루프 방지

  // 현재 선택된 서비스 데이터 찾기
  const currentServiceStats = statusData?.services.find(s => s.name === selectedService) || {
    name: selectedService,
    available: false,
    version: '-',
    uptime: '-',
    goroutines: 0
  }

  // 바로가기 핸들러
  const go = (path: string) => { void navigate(path) }

  // 둘 다 로딩 중일 때만 로더 표시
  if (isHoloLoading && isStatusLoading) {
    return (
      <div className="flex justify-center items-center h-64 text-slate-400">
        <div className="animate-spin mr-2">
          <Loader2 />
        </div>
        데이터를 불러오는 중…
      </div>
    )
  }

  // 치명적 에러 (둘 다 실패)
  if (isHoloError && isStatusError) {
    return (
      <div className="text-center py-12 bg-rose-50 rounded-2xl border border-rose-100">
        <div className="text-rose-600 font-bold mb-2">통계를 불러올 수 없습니다</div>
        <Button onClick={() => { void refetchHolo(); void refetchStatus(); }} className="bg-rose-600 hover:bg-rose-700 text-white">
          다시 시도
        </Button>
      </div>
    )
  }

  const mainStats = [
    {
      label: '등록된 멤버',
      value: holoStats?.members || 0,
      variant: 'cyan' as const,
      icon: <Users size={24} />,
    },
    {
      label: '활성 알람',
      value: holoStats?.alarms || 0,
      variant: 'rose' as const,
      icon: <Bell size={24} />,
    },
    {
      label: '연동된 방',
      value: holoStats?.rooms || 0,
      variant: 'indigo' as const,
      icon: <MessageSquare size={24} />,
    },
  ]

  // 서비스 아이콘 헬퍼
  const getServiceIcon = (name: string) => {
    if (name.includes('hololive')) return <Bot size={20} className="text-sky-500" />
    if (name.includes('admin')) return <ShieldCheck size={20} className="text-slate-500" />
    return <Server size={20} className="text-slate-400" />
  }

  return (
    <div className="space-y-8">
      {/* 1. 환영 배너 */}
      <div className="relative overflow-hidden rounded-3xl bg-white border border-slate-100 p-8 shadow-sm">
        {/* 배경 Gradients */}
        <div className="absolute top-0 right-0 w-96 h-96 bg-sky-50 rounded-full blur-3xl opacity-60 -mr-20 -mt-20 pointer-events-none"></div>
        <div className="absolute bottom-0 left-0 w-64 h-64 bg-cyan-50 rounded-full blur-3xl opacity-40 -ml-10 -mb-10 pointer-events-none"></div>

        <div className="relative z-10 flex flex-col md:flex-row items-center justify-between gap-8">
          <div className="max-w-xl">
            <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-sky-50 border border-sky-100 text-sky-600 text-xs font-semibold mb-4">
              <span className="relative flex h-2 w-2">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-sky-400 opacity-75"></span>
                <span className="relative inline-flex rounded-full h-2 w-2 bg-sky-500"></span>
              </span>
              System Operational
            </div>
            <h1 className="text-3xl font-bold text-slate-800 tracking-tight">
              Bot Management Console
            </h1>
          </div>

          {/* Hero 일러스트 */}
          <div className="hidden md:flex items-center justify-center w-32 h-32 bg-linear-to-br from-sky-400 via-cyan-400 to-indigo-400 rounded-3xl shadow-xl shadow-sky-200 transform rotate-6 border-4 border-white">
            <Play className="w-16 h-16 text-white drop-shadow-md fill-white ml-2" />
          </div>
        </div>
      </div>

      {/* 2. 주요 지표 (Holo Bot) */}
      <div>
        <h3 className="text-lg font-bold text-slate-800 mb-4 flex items-center gap-2">
          <Activity size={20} className="text-sky-500" />
          실시간 현황 (Hololive Bot)
        </h3>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
          {mainStats.map((stat) => (
            <div key={stat.label}>
              <StatCard
                label={stat.label}
                value={stat.value}
                icon={stat.icon}
                variant={stat.variant}
              />
            </div>
          ))}
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
        {/* 좌측 칼럼: 시스템 상태 & 차트 */}
        <div className="lg:col-span-2 space-y-6">

          {/* 3. 시스템 상태 (Compact & Selectable) */}
          <div className="bg-white rounded-2xl border border-slate-200 p-6 shadow-sm">
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-bold text-slate-800 flex items-center gap-2">
                <Server size={20} className="text-slate-500" />
                서비스 상태
              </h3>

              {/* 서비스 선택기 */}
              <div className="relative">
                <select
                  value={selectedService}
                  onChange={(e) => { setSelectedService(e.target.value); }}
                  className="appearance-none bg-slate-50 border border-slate-200 text-slate-700 text-sm font-medium rounded-lg py-2 pl-3 pr-8 focus:outline-none focus:ring-2 focus:ring-sky-500 focus:border-transparent cursor-pointer hover:bg-slate-100 transition-colors"
                  aria-label="서비스 선택"
                >
                  {statusData?.services.map(s => (
                    <option key={s.name} value={s.name}>{s.name}</option>
                  )) || <option value="hololive-bot">hololive-bot</option>}
                </select>
                <ChevronDown className="absolute right-2.5 top-2.5 text-slate-400 pointer-events-none" size={16} />
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              {/* 상태 카드 */}
              <div className="p-4 bg-slate-50 rounded-xl border border-slate-100 flex items-center justify-between">
                <div>
                  <div className="text-xs text-slate-500 font-medium uppercase tracking-wider mb-1">Service Status</div>
                  <div className="flex items-center gap-2">
                    {currentServiceStats.available ? (
                      <>
                        <span className="relative flex h-3 w-3">
                          <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                          <span className="relative inline-flex rounded-full h-3 w-3 bg-emerald-500"></span>
                        </span>
                        <span className="font-bold text-slate-700">Online</span>
                      </>
                    ) : (
                      <>
                        <div className="w-3 h-3 rounded-full bg-rose-500" />
                        <span className="font-bold text-slate-700">Offline</span>
                      </>
                    )}
                  </div>
                </div>
                <div className="h-10 w-10 bg-white rounded-full flex items-center justify-center border border-slate-200">
                  <ShieldCheck size={20} className={currentServiceStats.available ? "text-emerald-500" : "text-rose-500"} />
                </div>
              </div>

              {/* 버전 & 리소스 카드 */}
              <div className="p-4 bg-slate-50 rounded-xl border border-slate-100 flex items-center justify-between">
                <div>
                  <div className="text-xs text-slate-500 font-medium uppercase tracking-wider mb-1">Version Info</div>
                  <div className="font-bold text-slate-700 font-mono text-sm">
                    {currentServiceStats.version || 'Unknown'}
                  </div>
                  <div className="text-[10px] text-slate-400 mt-1">Uptime: {currentServiceStats.uptime || '-'}</div>
                </div>
                <div className="h-10 w-10 bg-white rounded-full flex items-center justify-center border border-slate-200">
                  {getServiceIcon(currentServiceStats.name)}
                </div>
              </div>
            </div>
          </div>

          <Suspense fallback={<ComponentLoader />}>
            <SystemStatsChart />
          </Suspense>
        </div>

        {/* 4. 바로가기 */}
        <div className="bg-white rounded-2xl border border-slate-200 p-6 shadow-sm flex flex-col h-fit">
          <h3 className="text-lg font-bold text-slate-800 mb-4">바로가기</h3>
          <div className="space-y-3 flex-1">
            <button
              onClick={() => { go('/dashboard/members') }}
              className="w-full flex items-center justify-between p-3 rounded-xl bg-sky-50 text-sky-700 hover:bg-sky-100 transition-colors group text-left"
            >
              <span className="font-medium">멤버 관리하기</span>
              <ArrowRight size={18} className="opacity-50 group-hover:opacity-100 group-hover:translate-x-1 transition-transform" aria-hidden="true" />
            </button>
            <button
              onClick={() => { go('/dashboard/alarms') }}
              className="w-full flex items-center justify-between p-3 rounded-xl bg-rose-50 text-rose-700 hover:bg-rose-100 transition-colors group text-left"
            >
              <span className="font-medium">알람 설정 확인</span>
              <ArrowRight size={18} className="opacity-50 group-hover:opacity-100 group-hover:translate-x-1 transition-transform" aria-hidden="true" />
            </button>
            <button
              onClick={() => { go('/dashboard/rooms') }}
              className="w-full flex items-center justify-between p-3 rounded-xl bg-indigo-50 text-indigo-700 hover:bg-indigo-100 transition-colors group text-left"
            >
              <span className="font-medium">채팅방 목록</span>
              <ArrowRight size={18} className="opacity-50 group-hover:opacity-100 group-hover:translate-x-1 transition-transform" aria-hidden="true" />
            </button>
          </div>
        </div>
      </div>


      {/* 5. 채널 통계 */}
      <div className="bg-white rounded-2xl border border-slate-200 p-6 shadow-sm">
        <h3 className="text-lg font-bold text-slate-800 mb-6 flex items-center gap-2">
          <Activity size={20} className="text-rose-500" />
          채널 통계 (구독자 순 상위 10등)
        </h3>
        <Suspense fallback={<ComponentLoader />}>
            <ChannelStatsTable />
        </Suspense>
      </div>
    </div >
  )
}

export default StatsTab
