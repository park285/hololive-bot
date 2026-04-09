import { useEffect, useMemo, useState, lazy, Suspense } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { statusApi } from '@/api/core'
import { queryKeys } from '@/api/queryKeys'
import { Button } from '@/components/ui/Button'
import { statsApi } from '@/features/stats/api'
import { StatsHero } from '@/features/stats/components/StatsHero'
import { StatsOverviewSection, type StatsOverviewCard } from '@/features/stats/components/StatsOverviewSection'
import { StatsServicesSection } from '@/features/stats/components/StatsServicesSection'
import Users from 'lucide-react/dist/esm/icons/users'
import Bell from 'lucide-react/dist/esm/icons/bell'
import MessageSquare from 'lucide-react/dist/esm/icons/message-square'
import Loader2 from 'lucide-react/dist/esm/icons/loader-2'
import Activity from 'lucide-react/dist/esm/icons/activity'

const ChannelStatsTable = lazy(() => import('@/components/dashboard/ChannelStatsTable').then((m) => ({ default: m.ChannelStatsTable })))

const StatsSectionLoader = () => (
  <div className="flex items-center justify-center h-48 text-slate-400 w-full bg-slate-50/50 rounded-lg">
    <Loader2 className="w-6 h-6 animate-spin mr-2" />
    <span className="text-sm">로딩 중…</span>
  </div>
)

export const StatsPage = () => {
  const navigate = useNavigate()
  const [selectedService, setSelectedService] = useState('hololive-bot')

  const { data: holoStats, isLoading: isHoloLoading, isError: isHoloError, refetch: refetchHolo } = useQuery({
    queryKey: queryKeys.stats.summary,
    queryFn: statsApi.get,
    staleTime: 1000 * 30,
    refetchInterval: () => document.visibilityState === 'visible' ? 30000 : false,
  })

  const { data: statusData, isLoading: isStatusLoading, isError: isStatusError, refetch: refetchStatus } = useQuery({
    queryKey: queryKeys.status.aggregated,
    queryFn: statusApi.get,
    staleTime: 1000 * 15,
    refetchInterval: () => document.visibilityState === 'visible' ? 15000 : false,
  })

  useEffect(() => {
    if (statusData?.services && statusData.services.length > 0) {
      setSelectedService((prev) => {
        const exists = statusData.services.find((service) => service.name === prev)
        if (exists) return prev
        const defaultService = statusData.services.find((service) => service.name === 'hololive-bot')
        return defaultService?.name ?? statusData.services[0]?.name ?? prev
      })
    }
  }, [statusData])

  const currentServiceStats = useMemo(() => {
    const baseService = statusData?.services.find((service) => service.name === selectedService)

    const runtimeInfo = selectedService === 'hololive-bot'
      ? {
          version: holoStats?.version,
          uptime: holoStats?.uptime,
        }
      : selectedService === 'admin-dashboard'
        ? {
            version: statusData?.version,
            uptime: statusData?.uptime,
          }
        : {
            version: undefined,
            uptime: undefined,
          }

    return {
      name: selectedService,
      available: baseService?.available ?? false,
      version: runtimeInfo.version ?? '-',
      uptime: runtimeInfo.uptime ?? '-',
    }
  }, [
    holoStats?.uptime,
    holoStats?.version,
    selectedService,
    statusData?.services,
    statusData?.uptime,
    statusData?.version,
  ])

  const go = (path: string) => { void navigate(path) }

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

  if (isHoloError && isStatusError) {
    return (
      <div className="text-center py-12 bg-rose-50 rounded-2xl border border-rose-100">
        <div className="text-rose-600 font-bold mb-2">통계를 불러올 수 없습니다</div>
        <Button onClick={() => { void refetchHolo(); void refetchStatus() }} className="bg-rose-600 hover:bg-rose-700 text-white">
          다시 시도
        </Button>
      </div>
    )
  }

  const mainStats: StatsOverviewCard[] = [
    {
      label: '등록된 멤버',
      value: holoStats?.members || 0,
      variant: 'cyan',
      icon: <Users size={24} />,
    },
    {
      label: '활성 알람',
      value: holoStats?.alarms || 0,
      variant: 'rose',
      icon: <Bell size={24} />,
    },
    {
      label: '연동된 방',
      value: holoStats?.rooms || 0,
      variant: 'indigo',
      icon: <MessageSquare size={24} />,
    },
  ]

  return (
    <div className="space-y-8">
      <StatsHero />
      <StatsOverviewSection cards={mainStats} />
      <StatsServicesSection
        statusData={statusData}
        selectedService={selectedService}
        currentServiceStats={currentServiceStats}
        onSelectService={setSelectedService}
        onNavigate={go}
      />

      <div className="bg-white rounded-2xl border border-slate-200 p-6 shadow-sm animate-fade-in-up stagger-5">
        <h3 className="text-lg font-display font-bold text-slate-800 mb-6 flex items-center gap-2">
          <Activity size={20} className="text-rose-500" />
          채널 통계 (구독자 순 상위 10등)
        </h3>
        <Suspense fallback={<StatsSectionLoader />}>
          <ChannelStatsTable />
        </Suspense>
      </div>
    </div>
  )
}
