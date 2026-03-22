import { useMemo, useState } from 'react'
import { useWebSocket } from '@/hooks/useWebSocket'
import { getCookie } from '@/api/client'
import type { SystemStats } from '@/types'
import { CONFIG } from '@/config'
import { Card } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { useAuthStore } from '@/stores/authStore'
import Activity from 'lucide-react/dist/esm/icons/activity'
import Cpu from 'lucide-react/dist/esm/icons/cpu'
import CircuitBoard from 'lucide-react/dist/esm/icons/circuit-board'
import Layers from 'lucide-react/dist/esm/icons/layers'
import Server from 'lucide-react/dist/esm/icons/server'
import { cn } from '@/lib/utils'

interface SystemStatsPoint extends SystemStats {
    time: string
    timestamp: number
    serviceValues: Record<string, number>
}

const MAX_DATA_POINTS = 30
const CHART_WIDTH = 800
const CHART_HEIGHT = 200
const CHART_PADDING_X = 24
const CHART_PADDING_Y = 18
const GOROUTINE_CHART_HEIGHT = 160
const SERVICE_FALLBACK_COLORS = ['#0ea5e9', '#8b5cf6', '#f59e0b', '#14b8a6', '#ef4444', '#6366f1']

const asRecord = (value: unknown): Record<string, unknown> | null =>
    typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : null

const asNumber = (value: unknown): number | null => {
    const parsed = typeof value === 'number' ? value : Number(value)
    return Number.isFinite(parsed) ? parsed : null
}

const parseSystemStats = (value: unknown): SystemStats | null => {
    const record = asRecord(value)
    if (!record) return null

    const cpuUsage = asNumber(record['cpuUsage'] ?? record['cpu_usage'])
    const memoryUsage = asNumber(record['memoryUsage'] ?? record['memory_usage'] ?? record['memory_usage_percent'])
    const memoryTotal = asNumber(record['memoryTotal'] ?? record['memory_total'])
    const memoryUsed = asNumber(record['memoryUsed'] ?? record['memory_used'])
    const goroutines = asNumber(record['goroutines'])
    const totalGoroutines = asNumber(record['totalGoroutines'] ?? record['total_goroutines'] ?? record['goroutines'])
    const serviceGoroutinesValue = Array.isArray(record['serviceGoroutines'])
        ? record['serviceGoroutines']
        : Array.isArray(record['service_goroutines'])
            ? record['service_goroutines']
            : []

    if (
        cpuUsage === null ||
        memoryUsage === null ||
        memoryTotal === null ||
        memoryUsed === null ||
        goroutines === null ||
        totalGoroutines === null
    ) {
        return null
    }

    const serviceGoroutines = serviceGoroutinesValue
        .map((entry) => {
            const item = asRecord(entry)
            if (!item || typeof item['name'] !== 'string') return null

            const itemGoroutines = asNumber(item['goroutines'])
            if (itemGoroutines === null || typeof item['available'] !== 'boolean') return null

            return {
                name: item['name'],
                goroutines: itemGoroutines,
                available: item['available'],
            }
        })
        .filter((entry): entry is SystemStats['serviceGoroutines'][number] => entry !== null)

    return {
        cpuUsage,
        memoryUsage,
        memoryTotal,
        memoryUsed,
        goroutines,
        totalGoroutines,
        serviceGoroutines,
    }
}

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value))

const getServiceColor = (name: string) => {
    const configured = CONFIG.ui.serviceColors[name]
    if (configured) {
        return configured
    }

    let hash = 0
    for (const char of name) {
        hash = (hash * 31 + char.charCodeAt(0)) >>> 0
    }

    return SERVICE_FALLBACK_COLORS[hash % SERVICE_FALLBACK_COLORS.length] ?? '#64748b'
}

const buildPolylinePath = (values: number[], maxValue: number, width: number, height: number) => {
    if (values.length === 0) return ''

    const innerWidth = width - (CHART_PADDING_X * 2)
    const innerHeight = height - (CHART_PADDING_Y * 2)
    const safeMax = Math.max(maxValue, 1)

    return values.map((value, index) => {
        const x = CHART_PADDING_X + ((values.length === 1 ? 0.5 : index / (values.length - 1)) * innerWidth)
        const y = CHART_PADDING_Y + (innerHeight * (1 - clamp(value / safeMax, 0, 1)))
        return `${index === 0 ? 'M' : 'L'} ${x.toFixed(2)} ${y.toFixed(2)}`
    }).join(' ')
}

const buildAreaPath = (values: number[], maxValue: number, width: number, height: number) => {
    if (values.length === 0) return ''

    const linePath = buildPolylinePath(values, maxValue, width, height)
    const innerWidth = width - (CHART_PADDING_X * 2)
    const baseY = height - CHART_PADDING_Y
    const lastX = CHART_PADDING_X + innerWidth

    return `${linePath} L ${lastX.toFixed(2)} ${baseY.toFixed(2)} L ${String(CHART_PADDING_X)} ${baseY.toFixed(2)} Z`
}

const getChartLabels = (history: SystemStatsPoint[]) => {
    if (history.length === 0) {
        return []
    }

    if (history.length === 1) {
        return [{ key: history[0]?.timestamp ?? 0, label: history[0]?.time ?? '', align: 'middle' as const, x: '50%' }]
    }

    const middleIndex = Math.floor((history.length - 1) / 2)

    return [
        { key: history[0]?.timestamp ?? 0, label: history[0]?.time ?? '', align: 'start' as const, x: '0%' },
        { key: history[middleIndex]?.timestamp ?? 0, label: history[middleIndex]?.time ?? '', align: 'middle' as const, x: '50%' },
        { key: history[history.length - 1]?.timestamp ?? 0, label: history[history.length - 1]?.time ?? '', align: 'end' as const, x: '100%' },
    ]
}

const ChartSkeleton = ({ label }: { label: string }) => (
    <div className="absolute inset-0 z-10 flex items-center justify-center bg-slate-50/50 rounded-b-lg">
        <div className="flex items-center gap-2">
            <div className="h-4 w-4 border-2 border-slate-300 border-t-sky-500 rounded-full animate-spin" />
            <span className="text-xs text-slate-500">{label}</span>
        </div>
    </div>
)

interface ResourceChartProps {
    history: SystemStatsPoint[]
}

const ResourceChart = ({ history }: ResourceChartProps) => {
    const cpuValues = history.map((point) => point.cpuUsage)
    const memoryValues = history.map((point) => point.memoryUsage)
    const maxValue = Math.max(100, ...cpuValues, ...memoryValues)
    const labels = getChartLabels(history)

    const cpuAreaPath = buildAreaPath(cpuValues, maxValue, CHART_WIDTH, CHART_HEIGHT)
    const memoryAreaPath = buildAreaPath(memoryValues, maxValue, CHART_WIDTH, CHART_HEIGHT)
    const cpuLinePath = buildPolylinePath(cpuValues, maxValue, CHART_WIDTH, CHART_HEIGHT)
    const memoryLinePath = buildPolylinePath(memoryValues, maxValue, CHART_WIDTH, CHART_HEIGHT)

    return (
        <div className="w-full">
            <div className="relative h-[200px] w-full overflow-hidden rounded-lg border border-slate-100 bg-white">
                <svg viewBox={`0 0 ${String(CHART_WIDTH)} ${String(CHART_HEIGHT)}`} className="h-full w-full" preserveAspectRatio="none" aria-label="CPU 및 메모리 사용량 추이">
                    <defs>
                        <linearGradient id="cpuGradient" x1="0" y1="0" x2="0" y2="1">
                            <stop offset="0%" stopColor="#0ea5e9" stopOpacity="0.28" />
                            <stop offset="100%" stopColor="#0ea5e9" stopOpacity="0.02" />
                        </linearGradient>
                        <linearGradient id="memoryGradient" x1="0" y1="0" x2="0" y2="1">
                            <stop offset="0%" stopColor="#8b5cf6" stopOpacity="0.22" />
                            <stop offset="100%" stopColor="#8b5cf6" stopOpacity="0.02" />
                        </linearGradient>
                    </defs>

                    {[0, 25, 50, 75, 100].map((tick) => {
                        const y = CHART_PADDING_Y + ((CHART_HEIGHT - (CHART_PADDING_Y * 2)) * (1 - (tick / 100)))
                        return (
                            <g key={tick}>
                                <line x1={CHART_PADDING_X} y1={y} x2={CHART_WIDTH - CHART_PADDING_X} y2={y} stroke="#e2e8f0" strokeDasharray="4 4" />
                                <text x={6} y={y + 4} fill="#94a3b8" fontSize="10">{tick}%</text>
                            </g>
                        )
                    })}

                    {memoryAreaPath && <path d={memoryAreaPath} fill="url(#memoryGradient)" />}
                    {cpuAreaPath && <path d={cpuAreaPath} fill="url(#cpuGradient)" />}

                    {memoryLinePath && <path d={memoryLinePath} fill="none" stroke="#8b5cf6" strokeWidth="3" strokeLinejoin="round" strokeLinecap="round" />}
                    {cpuLinePath && <path d={cpuLinePath} fill="none" stroke="#0ea5e9" strokeWidth="3" strokeLinejoin="round" strokeLinecap="round" />}
                </svg>
            </div>

            <div className="mt-3 flex items-center justify-between text-[11px] text-slate-400 font-mono">
                {labels.map((label) => (
                    <span key={label.key} className={cn(
                        label.align === 'start' && 'text-left',
                        label.align === 'middle' && 'text-center',
                        label.align === 'end' && 'text-right',
                    )} style={{ width: label.align === 'middle' ? '33%' : '33%' }}>
                        {label.label}
                    </span>
                ))}
            </div>

            <div className="mt-3 flex flex-wrap gap-3 text-xs">
                <div className="inline-flex items-center gap-2 rounded-full bg-sky-50 px-3 py-1 font-medium text-sky-700">
                    <span className="h-2 w-2 rounded-full bg-sky-500" />
                    CPU
                </div>
                <div className="inline-flex items-center gap-2 rounded-full bg-violet-50 px-3 py-1 font-medium text-violet-700">
                    <span className="h-2 w-2 rounded-full bg-violet-500" />
                    Memory
                </div>
            </div>
        </div>
    )
}

interface GoroutineChartProps {
    history: SystemStatsPoint[]
    serviceNames: string[]
}

const GoroutineChart = ({ history, serviceNames }: GoroutineChartProps) => {
    const maxValue = Math.max(1, ...history.map((point) => point.totalGoroutines))
    const labels = getChartLabels(history)
    const innerHeight = GOROUTINE_CHART_HEIGHT - (CHART_PADDING_Y * 2)
    const innerWidth = CHART_WIDTH - (CHART_PADDING_X * 2)
    const columnWidth = history.length > 0 ? innerWidth / history.length : innerWidth
    const barWidth = Math.max(6, columnWidth - 4)

    return (
        <div className="w-full">
            <div className="relative h-[160px] w-full overflow-hidden rounded-lg border border-slate-100 bg-white">
                <svg viewBox={`0 0 ${String(CHART_WIDTH)} ${String(GOROUTINE_CHART_HEIGHT)}`} className="h-full w-full" preserveAspectRatio="none" aria-label="서비스별 고루틴 추이">
                    {[0, 0.5, 1].map((ratio) => {
                        const y = CHART_PADDING_Y + (innerHeight * ratio)
                        const labelValue = Math.round(maxValue * (1 - ratio))
                        return (
                            <g key={ratio}>
                                <line x1={CHART_PADDING_X} y1={y} x2={CHART_WIDTH - CHART_PADDING_X} y2={y} stroke="#e2e8f0" strokeDasharray="4 4" />
                                <text x={6} y={y + 4} fill="#94a3b8" fontSize="10">{labelValue}</text>
                            </g>
                        )
                    })}

                    {history.map((point, pointIndex) => {
                        const x = CHART_PADDING_X + (pointIndex * columnWidth) + Math.max((columnWidth - barWidth) / 2, 1)
                        let stackOffset = 0

                        return serviceNames.map((serviceName) => {
                            const value = point.serviceValues[serviceName] ?? 0
                            if (value <= 0) {
                                return null
                            }

                            const height = (value / maxValue) * innerHeight
                            const y = GOROUTINE_CHART_HEIGHT - CHART_PADDING_Y - stackOffset - height
                            stackOffset += height

                            return (
                                <rect
                                    key={`${String(point.timestamp)}-${serviceName}`}
                                    x={x}
                                    y={y}
                                    width={barWidth}
                                    height={height}
                                    rx={Math.min(3, barWidth / 3)}
                                    fill={getServiceColor(serviceName)}
                                    opacity="0.88"
                                >
                                    <title>{`${serviceName}: ${String(value)} (${point.time})`}</title>
                                </rect>
                            )
                        })
                    })}
                </svg>
            </div>

            <div className="mt-3 flex items-center justify-between text-[11px] text-slate-400 font-mono">
                {labels.map((label) => (
                    <span key={label.key} className={cn(
                        label.align === 'start' && 'text-left',
                        label.align === 'middle' && 'text-center',
                        label.align === 'end' && 'text-right',
                    )} style={{ width: '33%' }}>
                        {label.label}
                    </span>
                ))}
            </div>
        </div>
    )
}

export const SystemStatsChart = () => {
    const [statsHistory, setStatsHistory] = useState<SystemStatsPoint[]>([])
    const [currentStats, setCurrentStats] = useState<SystemStats | null>(null)
    const isAuthenticated = useAuthStore((state) => state.isAuthenticated)

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/admin/api/ws/system-stats`
    const hasSessionCookie = getCookie('admin_session') != null

    const serviceNames = useMemo(() => {
        const names = new Set<string>()
        statsHistory.forEach((point) => {
            Object.keys(point.serviceValues).forEach((name) => {
                names.add(name)
            })
        })
        currentStats?.serviceGoroutines.forEach((service) => {
            names.add(service.name)
        })
        return [...names]
    }, [currentStats, statsHistory])

    const latestPoint = statsHistory[statsHistory.length - 1]

    const { isConnected } = useWebSocket<SystemStats>(wsUrl, {
        autoConnect: isAuthenticated && hasSessionCookie,
        parseMessage: (data) => parseSystemStats(data),
        onMessage: (data) => {
            const now = new Date()
            const timeStr = now.toLocaleTimeString('ko-KR', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })

            const serviceValues = data.serviceGoroutines.reduce<Record<string, number>>((acc, service) => {
                acc[service.name] = service.available ? service.goroutines : 0
                return acc
            }, {})

            const point: SystemStatsPoint = {
                ...data,
                serviceValues,
                time: timeStr,
                timestamp: now.getTime(),
            }

            setCurrentStats(data)
            setStatsHistory((prev) => [...prev, point].slice(-MAX_DATA_POINTS))
        },
        reconnectInterval: 5000,
    })

    return (
        <Card className="overflow-hidden">
            <Card.Header className="flex flex-row items-center justify-between border-b border-slate-100 pb-4 bg-slate-50/50">
                <div className="flex items-center gap-2">
                    <Activity className="text-slate-500" size={20} />
                    <h3 className="text-lg font-display font-bold text-slate-800">시스템 리소스</h3>
                    {isConnected ? (
                        <span className="flex h-2 w-2 relative ml-2">
                            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                            <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
                        </span>
                    ) : (
                        <span className="h-2 w-2 rounded-full bg-slate-300 ml-2"></span>
                    )}
                </div>

                {currentStats && (
                    <div className="flex gap-4 text-xs font-mono">
                        <div className="flex items-center gap-1.5 px-2 py-1 bg-white rounded border border-slate-100 shadow-sm">
                            <Cpu size={14} className="text-sky-500" />
                            <span className="font-bold text-slate-700">{currentStats.cpuUsage.toFixed(1)}%</span>
                        </div>
                        <div className="flex items-center gap-1.5 px-2 py-1 bg-white rounded border border-slate-100 shadow-sm">
                            <Layers size={14} className="text-violet-500" />
                            <span className="font-bold text-slate-700">{currentStats.memoryUsage.toFixed(1)}%</span>
                        </div>
                        <div className="flex items-center gap-1.5 px-2 py-1 bg-white rounded border border-slate-100 shadow-sm hidden sm:flex">
                            <CircuitBoard size={14} className="text-slate-400" />
                            <span className="font-bold text-slate-500">{currentStats.totalGoroutines} Goroutines</span>
                        </div>
                    </div>
                )}
            </Card.Header>

            <Card.Body className="p-0 relative">
                {statsHistory.length < 2 && (
                    <ChartSkeleton label="데이터 수집 중…" />
                )}

                <div className="p-4">
                    <ResourceChart history={statsHistory} />
                </div>

                <div className="px-4 py-3 border-t border-slate-100">
                    <div className="mb-4 flex items-center justify-between gap-3">
                        <div className="flex items-center gap-2">
                            <CircuitBoard size={16} className="text-slate-400" />
                            <h4 className="text-sm font-bold text-slate-700">서비스별 고루틴</h4>
                        </div>
                        {latestPoint && (
                            <span className="text-[11px] font-mono text-slate-400">
                                latest {latestPoint.time}
                            </span>
                        )}
                    </div>

                    <GoroutineChart history={statsHistory} serviceNames={serviceNames} />
                </div>

                {currentStats && (
                    <div className="px-4 py-3 bg-slate-50/50 border-t border-slate-100">
                        <div className="flex items-center gap-2 mb-2">
                            <Server size={14} className="text-slate-400" />
                            <span className="text-xs font-bold text-slate-600 uppercase tracking-wider">Service Status</span>
                        </div>
                        <div className="flex gap-2 flex-wrap">
                            {currentStats.serviceGoroutines.map((service) => (
                                <Badge
                                    key={service.name}
                                    variant="outline"
                                    className="text-[10px] py-0.5 px-2.5 h-6 font-mono bg-white border-slate-200 shadow-sm hover:border-slate-300 transition-colors"
                                >
                                    <span
                                        className={cn('mr-1.5 h-1.5 w-1.5 rounded-full', service.available ? 'animate-pulse' : 'bg-red-500')}
                                        style={{ backgroundColor: service.available ? getServiceColor(service.name) : undefined }}
                                    />
                                    <span style={{ color: service.available ? getServiceColor(service.name) : undefined, fontWeight: 600 }}>
                                        {service.name}
                                    </span>
                                    <span className="text-slate-600 ml-1">
                                        : {service.available ? service.goroutines : 'OFFLINE'}
                                    </span>
                                </Badge>
                            ))}
                        </div>
                    </div>
                )}
            </Card.Body>
        </Card>
    )
}
