import { useQuery } from '@tanstack/react-query'
import { dockerApi } from '@/api/core'
import { logsApi } from '@/api/logs'
import { queryKeys } from '@/api/queryKeys'
import ScrollText from 'lucide-react/dist/esm/icons/scroll-text';
import RefreshCw from 'lucide-react/dist/esm/icons/refresh-cw';
import AlertTriangle from 'lucide-react/dist/esm/icons/alert-triangle';
import Shield from 'lucide-react/dist/esm/icons/shield';
import Activity from 'lucide-react/dist/esm/icons/activity';
import ChevronDown from 'lucide-react/dist/esm/icons/chevron-down';
import ChevronRight from 'lucide-react/dist/esm/icons/chevron-right';
import Server from 'lucide-react/dist/esm/icons/server';
import Terminal from 'lucide-react/dist/esm/icons/terminal';
import LayoutList from 'lucide-react/dist/esm/icons/layout-list';
import FileText from 'lucide-react/dist/esm/icons/file-text';
import { useState } from 'react'
import clsx from 'clsx'
import type { LogEntry } from '@/types'
import { LogTerminal } from '@/components/docker/LogTerminal'
import { useSSRData } from '@/hooks/useSSRData'
import { Label } from '@/components/ui/Label'

// --- 헬퍼 함수 & 서브 컴포넌트 ---

const dateFormatter = new Intl.DateTimeFormat('ko-KR', {
    year: '2-digit', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
    hour12: false
})

const numberFormatter = new Intl.NumberFormat('ko-KR')

const formatDetailValue = (value: unknown): string => {
    if (value === null) return 'null'
    if (value === undefined) return 'undefined'

    switch (typeof value) {
        case 'string':
            return value
        case 'number':
        case 'boolean':
        case 'bigint':
            return String(value)
        case 'symbol':
            return value.toString()
        case 'function':
            return '[function]'
        case 'object':
            if (value instanceof Date) return value.toISOString()
            try { return JSON.stringify(value) } catch { return '[unserializable]' }
        default:
            return ''
    }
}

const LogItem = ({ log }: { log: LogEntry }) => {
    const [expanded, setExpanded] = useState(false)

    const getTypeConfig = (type: string) => {
        if (type.includes('error') || type.includes('fail')) return { icon: AlertTriangle, color: 'text-rose-600', bg: 'bg-rose-50', border: 'border-rose-100' }
        if (type.includes('auth')) return { icon: Shield, color: 'text-amber-600', bg: 'bg-amber-50', border: 'border-amber-100' }
        if (type.includes('system') || type.includes('watchdog')) return { icon: Server, color: 'text-slate-600', bg: 'bg-slate-100', border: 'border-slate-200' }
        return { icon: Activity, color: 'text-sky-600', bg: 'bg-sky-50', border: 'border-sky-100' }
    }

    const { icon: Icon, color, border } = getTypeConfig(log.type)
    const details = log.details ?? {}
    const hasDetails = Object.keys(details).length > 0

    return (
        <div className={clsx(
            "log-item group text-sm bg-white border-b border-slate-100 last:border-0 hover:bg-slate-50 transition-colors [content-visibility:auto] contain-intrinsic-size-[50px]",
            expanded && "expanded"
        )}>
            <div
                className={clsx(
                    "grid grid-cols-[140px_1fr] md:grid-cols-[150px_140px_1fr] items-start gap-4 p-3 cursor-pointer focus:outline-none focus:bg-slate-100 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-sky-200",
                    expanded && "bg-slate-50"
                )}
                onClick={() => {
                    if (!hasDetails) return
                    setExpanded((prev) => !prev)
                }}
                role="button"
                tabIndex={hasDetails ? 0 : undefined}
                aria-expanded={expanded}
                onKeyDown={(e) => {
                    if (!hasDetails) return
                    if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        setExpanded((prev) => !prev)
                    }
                }}
            >
                <div className="font-mono text-xs text-slate-400 pt-0.5 tabular-nums">
                    {dateFormatter.format(new Date(log.timestamp))}
                </div>
                <div className="hidden md:flex items-center gap-1.5">
                    <div className={clsx("p-1 rounded bg-white border shrink-0", border)} aria-hidden="true">
                        <Icon size={12} className={color} />
                    </div>
                    <span className={clsx("text-xs font-bold uppercase tracking-wide", color)}>
                        {log.type.replace(/_/g, ' ')}
                    </span>
                </div>
                <div className="min-w-0 flex items-center gap-2">
                    <div className="md:hidden shrink-0" aria-hidden="true">
                        <Icon size={14} className={color} />
                    </div>
                    <span className="font-medium text-slate-700 truncate">{log.summary}</span>
                    {hasDetails && (
                        <div className="ml-auto text-slate-300">
                            {expanded ? <ChevronDown size={14} aria-hidden="true" /> : <ChevronRight size={14} aria-hidden="true" />}
                        </div>
                    )}
                </div>
            </div>
            {expanded && hasDetails && (
                <div className="px-3 pb-3 pl-7 md:pl-76">
                    <div className="bg-slate-100 rounded-lg p-3 border border-slate-200 text-xs font-mono overflow-x-auto">
                        <table className="w-full text-left border-collapse" aria-label="로그 상세 정보">
                            <tbody>
                                {Object.entries(details).map(([key, value]) => (
                                    <tr key={key} className="border-b border-slate-200/50 last:border-0">
                                        <th scope="row" className="py-1 pr-4 text-slate-500 font-semibold align-top whitespace-nowrap">{key}</th>
                                        <td className="py-1 text-slate-700 break-all tabular-nums">
                                            {formatDetailValue(value)}
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                </div>
            )}
        </div>
    )
}

// 시스템 로그 라인 컬러링 (로그 레벨에 따라)
const getLogLineColor = (line: string): string => {
    if (line.includes(' ERR ') || line.includes('error')) return 'text-rose-600 font-medium'
    if (line.includes(' WRN ') || line.includes('warn')) return 'text-amber-600 font-medium'
    if (line.includes(' DBG ') || line.includes('debug')) return 'text-slate-400'
    return 'text-slate-700'
}

const LogsTab = () => {
    const [viewMode, setViewMode] = useState<'audit' | 'system'>('audit')
    const [logSource, setLogSource] = useState<string>('combined') // 'combined' or containerName

    // --- Audit 로그 데이터 fetching ---
    const { data: logsData, isLoading: logsLoading, refetch: refetchLogs, isRefetching: logsRefetching } = useQuery({
        queryKey: queryKeys.logs.all,
        queryFn: logsApi.get,
        refetchInterval: 5000,
        enabled: viewMode === 'audit'
    })

    // --- 시스템 로그 데이터 fetching (Combined only) ---
    const { data: systemLogsData, isLoading: systemLogsLoading, refetch: refetchSystemLogs, isRefetching: systemLogsRefetching } = useQuery({
        queryKey: queryKeys.logs.system('combined'),
        queryFn: () => logsApi.getSystemLogs('combined', 300),
        refetchInterval: 5000,
        enabled: viewMode === 'system' && logSource === 'combined'
    })

    // --- Data Fetching for Docker Containers (useSSRData 훅 활용) ---
    const ssrContainersData = useSSRData('containers', (data) =>
        data?.status === 'ok' && Array.isArray(data.containers)
            ? { status: data.status, containers: data.containers }
            : undefined
    )

    const { data: containersData } = useQuery({
        queryKey: queryKeys.docker.containers,
        queryFn: dockerApi.getContainers,
        refetchInterval: 15000,
        initialData: ssrContainersData,
        enabled: viewMode === 'system' // Always fetch containers in system mode for dropdown
    })

    const containers = containersData?.containers ?? []
    const managedContainers = containers.filter(c => c.managed)

    return (
        <div className="space-y-4 max-w-full h-[calc(100vh-140px)] flex flex-col">
            {/* Top Toolbar / Tabs */}
            <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 bg-white p-2 rounded-xl border border-slate-200 shadow-sm shrink-0">
                <div className="flex items-center gap-1 bg-slate-100 p-1 rounded-lg w-full sm:w-auto" role="tablist" aria-label="로그 뷰 모드">
                    <button
                        role="tab"
                        aria-selected={viewMode === 'audit'}
                        onClick={() => { setViewMode('audit') }}
                        className={clsx(
                            "flex items-center gap-2 px-3 py-2 rounded-md text-sm font-bold transition-colors flex-1 sm:flex-none justify-center focus-visible:ring-2 focus-visible:ring-indigo-200 outline-none",
                            viewMode === 'audit'
                                ? "bg-white text-indigo-600 shadow-sm"
                                : "text-slate-500 hover:text-slate-700 hover:bg-slate-200/50"
                        )}
                    >
                        <LayoutList size={16} aria-hidden="true" />
                        활동 기록
                    </button>
                    <button
                        role="tab"
                        aria-selected={viewMode === 'system'}
                        onClick={() => { setViewMode('system') }}
                        className={clsx(
                            "flex items-center gap-2 px-3 py-2 rounded-md text-sm font-bold transition-colors flex-1 sm:flex-none justify-center focus-visible:ring-2 focus-visible:ring-emerald-200 outline-none",
                            viewMode === 'system'
                                ? "bg-white text-emerald-600 shadow-sm"
                                : "text-slate-500 hover:text-slate-700 hover:bg-slate-200/50"
                        )}
                    >
                        <Terminal size={16} aria-hidden="true" />
                        시스템 로그
                    </button>
                </div>

                {viewMode === 'audit' ? (
                    <div className="flex items-center gap-3 w-full sm:w-auto justify-end px-2">
                        <span className="text-xs text-slate-500 font-medium hidden sm:inline tabular-nums">
                            최근 {numberFormatter.format(logsData?.logs.length ?? 0)}개의 활동
                        </span>
                        <button
                            onClick={() => { void refetchLogs() }}
                            className="p-2 hover:bg-slate-50 rounded-lg text-slate-500 transition-colors border border-transparent hover:border-slate-200 focus-visible:ring-2 focus-visible:ring-indigo-200 outline-none"
                            title="새로고침"
                            aria-label="활동 기록 새로고침"
                        >
                            <RefreshCw size={18} className={logsRefetching ? "animate-spin text-indigo-500" : ""} aria-hidden="true" />
                        </button>
                    </div>
                ) : (
                    <div className="flex items-center gap-3 w-full sm:w-auto px-2 justify-end">
                        <div className="relative">
                            <Label htmlFor="log-source-select" className="sr-only">로그 소스 선택</Label>
                            <select
                                id="log-source-select"
                                value={logSource}
                                onChange={(e) => { setLogSource(e.target.value) }}
                                className="bg-slate-50 text-slate-700 text-sm font-medium rounded-lg pl-3 pr-8 py-2 border border-slate-200 focus:outline-none focus:ring-2 focus:ring-sky-500 w-full appearance-none cursor-pointer"
                            >
                                <option value="combined">통합 로그 (Combined)</option>
                                <optgroup label="컨테이너 (실시간)">
                                    {managedContainers.map(c => (
                                        <option key={c.name} value={c.name}>
                                            {c.name}
                                        </option>
                                    ))}
                                </optgroup>
                            </select>
                            <ChevronDown size={14} className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-400 pointer-events-none" aria-hidden="true" />
                        </div>

                        {logSource === 'combined' && (
                            <button
                                onClick={() => { void refetchSystemLogs() }}
                                className="p-2 hover:bg-slate-50 rounded-lg text-slate-500 transition-colors border border-transparent hover:border-slate-200 focus-visible:ring-2 focus-visible:ring-emerald-200 outline-none"
                                title="새로고침"
                                aria-label="시스템 로그 새로고침"
                            >
                                <RefreshCw size={18} className={systemLogsRefetching ? "animate-spin text-emerald-500" : ""} aria-hidden="true" />
                            </button>
                        )}
                    </div>
                )}
            </div>

            {/* Main Content Area */}
            <div className="flex-1 min-h-0 bg-white rounded-xl shadow-sm border border-slate-200 flex flex-col overflow-hidden relative">

                {viewMode === 'audit' && (
                    <>
                        <div className="flex-1 overflow-auto bg-slate-50 scrollbar-thin scrollbar-thumb-slate-200 scrollbar-track-transparent" aria-busy={logsLoading}>
                            {logsLoading ? (
                                <div className="flex flex-col items-center justify-center h-full text-slate-400 gap-2">
                                    <RefreshCw className="animate-spin opacity-50" aria-hidden="true" />
                                    <span className="text-sm">로그를 불러오는 중…</span>
                                </div>
                            ) : (!logsData?.logs || logsData.logs.length === 0) ? (
                                <div className="flex flex-col items-center justify-center h-full text-slate-400 gap-3">
                                    <div className="w-12 h-12 bg-slate-100 rounded-full flex items-center justify-center">
                                        <ScrollText className="text-slate-300" aria-hidden="true" />
                                    </div>
                                    <p className="text-sm font-medium">기록된 로그가 없습니다.</p>
                                </div>
                            ) : (
                                <div className="bg-white min-w-[600px] md:min-w-0" role="list">
                                    <div className="hidden md:grid md:grid-cols-[150px_140px_1fr] border-b border-slate-100 bg-slate-50/50 p-2 pl-3 text-xs font-bold text-slate-500 uppercase tracking-wider sticky top-0 z-10">
                                        <div>Timestamp</div>
                                        <div>Type</div>
                                        <div>Activity</div>
                                    </div>
                                    {[...(logsData.logs)].reverse().map((log, idx) => (
                                        <div key={`${log.timestamp}-${String(idx)}`} role="listitem">
                                            <LogItem log={log} />
                                        </div>
                                    ))}
                                </div>
                            )}
                        </div>
                        <div className="p-2 border-t border-slate-100 bg-white text-[11px] text-slate-400 text-center shrink-0">
                            Updates automatically every 5s • Showing last 100 system events
                        </div>
                    </>
                )}

                {viewMode === 'system' && (
                    <>
                        {logSource === 'combined' ? (
                            <div className="flex flex-col h-full">
                                <div className="flex-1 overflow-auto bg-slate-50 scrollbar-thin scrollbar-thumb-slate-200 scrollbar-track-transparent" aria-busy={systemLogsLoading}>
                                    {systemLogsLoading ? (
                                        <div className="flex flex-col items-center justify-center h-full text-slate-400 gap-2">
                                            <RefreshCw className="animate-spin opacity-50" aria-hidden="true" />
                                            <span className="text-sm">로그를 불러오는 중…</span>
                                        </div>
                                    ) : (!systemLogsData?.lines || systemLogsData.lines.length === 0) ? (
                                        <div className="flex flex-col items-center justify-center h-full text-slate-400 gap-3">
                                            <div className="w-12 h-12 bg-slate-100 rounded-full flex items-center justify-center">
                                                <FileText className="text-slate-300" aria-hidden="true" />
                                            </div>
                                            <p className="text-sm font-medium">
                                                {systemLogsData?.error ?? '로그가 없습니다.'}
                                            </p>
                                        </div>
                                    ) : (
                                        <div className="font-mono text-xs leading-relaxed p-4 select-all">
                                            {systemLogsData.lines.map((line, idx) => (
                                                <div
                                                    key={idx}
                                                    className={clsx(
                                                        "py-0.5 hover:bg-slate-100 px-2 -mx-2 rounded",
                                                        getLogLineColor(line)
                                                    )}
                                                >
                                                    {line}
                                                </div>
                                            ))}
                                        </div>
                                    )}
                                </div>
                                <div className="p-2 border-t border-slate-200 bg-white text-[11px] text-slate-400 text-center shrink-0 tabular-nums">
                                    통합 로그 • {numberFormatter.format(systemLogsData?.count ?? 0)} lines • Updates automatically every 5s
                                </div>
                            </div>
                        ) : (
                            <div className="flex-1 p-0 bg-white flex flex-col min-h-0">
                                {/* LogTerminal 컴포넌트: React key를 주어 컨테이너 변경 시 완전히 remount 되도록 함 */}
                                <LogTerminal key={logSource} containerName={logSource} />
                            </div>
                        )}
                    </>
                )}
            </div>
        </div>
    )
}

export default LogsTab
