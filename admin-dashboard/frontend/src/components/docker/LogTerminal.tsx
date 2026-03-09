import { useState, useRef, useEffect, useCallback } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { Loader2, XCircle } from 'lucide-react'
import { useWebSocket } from '@/hooks/useWebSocket'
import { CONFIG } from '@/config/constants'

const stripAnsi = (input: string): string => {
    let output = ''
    let i = 0

    while (i < input.length) {
        const code = input.charCodeAt(i)

        // ESC (0x1B)
        if (code === 0x1b) {
            i += 1

            // CSI: ESC [
            if (i < input.length && input.charCodeAt(i) === 0x5b) {
                i += 1
                while (i < input.length) {
                    const finalByte = input.charCodeAt(i)
                    if (finalByte >= 0x40 && finalByte <= 0x7e) {
                        i += 1
                        break
                    }
                    i += 1
                }
                continue
            }

            // Non-CSI escape sequence: 다음 바이트 존재 시 스킵
            if (i < input.length) i += 1
            continue
        }

        // CSI (0x9B)
        if (code === 0x9b) {
            i += 1
            while (i < input.length) {
                const finalByte = input.charCodeAt(i)
                if (finalByte >= 0x40 && finalByte <= 0x7e) {
                    i += 1
                    break
                }
                i += 1
            }
            continue
        }

        output += input[i] ?? ''
        i += 1
    }

    return output
}

// 로그 라인 파싱 및 포맷팅 컴포넌트 (외부 정의로 재생성 방지)
const LOG_REGEX = /^(\d{4}-\d{2}-\d{2}T[\d:.]+Z)?\s*(\d{4}-\d{2}-\d{2}T[\d:]+[+-]\d{2}:\d{2})?\s*(INF|WRN|ERR|DBG|TRC|FTL|WARN|INFO|ERROR|DEBUG|TRACE|FATAL)\s+(\S+\.go:\d+)?\s*(.*)/i

const FormattedLogLine = ({ line }: { line: string }) => {
    const cleanLine = stripAnsi(line)
    const match = cleanLine.match(LOG_REGEX)

    if (match) {
        const [, , appTs, level, source, content] = match
        if (!level) return <span>{cleanLine}</span>

        let levelColor = "text-slate-700"
        const upperLevel = (level || '').toUpperCase()

        if (['ERR', 'ERROR', 'FATAL', 'FTL'].includes(upperLevel)) levelColor = "text-rose-600 font-bold"
        else if (['WRN', 'WARN'].includes(upperLevel)) levelColor = "text-amber-600 font-bold"
        else if (['INF', 'INFO'].includes(upperLevel)) levelColor = "text-emerald-600 font-bold"
        else if (['DBG', 'DEBUG', 'TRC', 'TRACE'].includes(upperLevel)) levelColor = "text-sky-600"

        return (
            <span>
                {appTs && <span className="text-slate-400">{appTs} </span>}
                <span className={levelColor}>{level}</span>
                {source && <span className="text-slate-500 ml-1">{source}</span>}
                <span className="ml-1 text-slate-700">{content || ''}</span>
            </span>
        )
    }

    return <span>{cleanLine}</span>
}
FormattedLogLine.displayName = 'FormattedLogLine'

interface LogTerminalProps {
    containerName: string
    isConnected?: boolean // 외부에서 연결 상태를 알 수 있게 (선택적)
    onConnectionChange?: (connected: boolean) => void
}

export const LogTerminal = ({ containerName, onConnectionChange }: LogTerminalProps) => {
    const [logs, setLogs] = useState<string[]>([])
    const parentRef = useRef<HTMLDivElement>(null)
    const pendingLogsRef = useRef<string[]>([])
    const flushFrameRef = useRef<number | null>(null)
    const autoScrollEnabledRef = useRef(true)

    // Virtualizer 설정
    const rowVirtualizer = useVirtualizer({
        count: logs.length,
        getScrollElement: () => parentRef.current,
        estimateSize: () => 24, // 한 줄 대략 높이 (px)
        overscan: 20,
    })

    // WebSocket URL
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = containerName
        ? `${protocol}//${window.location.host}/admin/api/docker/containers/${containerName}/logs/stream`
        : ''

    const flushPendingLogs = useCallback(() => {
        flushFrameRef.current = null

        if (pendingLogsRef.current.length === 0) {
            return
        }

        const pending = pendingLogsRef.current
        pendingLogsRef.current = []

        setLogs(prev => {
            const nextLogs = prev.concat(pending)
            if (nextLogs.length > CONFIG.logs.maxLines) {
                return nextLogs.slice(-CONFIG.logs.maxLines)
            }
            return nextLogs
        })
    }, [])

    const scheduleFlush = useCallback(() => {
        if (flushFrameRef.current !== null) {
            return
        }

        flushFrameRef.current = window.requestAnimationFrame(() => {
            flushPendingLogs()
        })
    }, [flushPendingLogs])

    const { isConnected, isConnecting } = useWebSocket<string>(wsUrl, {
        autoConnect: !!containerName,
        parseMessage: (data) => {
            if (typeof data === 'string') return data
            try {
                return JSON.stringify(data)
            } catch {
                return String(data)
            }
        },
        onMessage: (data) => {
            pendingLogsRef.current.push(data)
            scheduleFlush()
        },
        onOpen: () => {
            pendingLogsRef.current = []
            setLogs(['--- Connected to log stream ---'])
        },
    })

    // 상위 컴포넌트에 연결 상태 전달
    useEffect(() => {
        onConnectionChange?.(isConnected)
    }, [isConnected, onConnectionChange])

    // Auto-scroll to bottom (새 로그 추가 시)
    useEffect(() => {
        if (logs.length > 0 && autoScrollEnabledRef.current) {
            window.requestAnimationFrame(() => {
                rowVirtualizer.scrollToIndex(logs.length - 1, { align: 'end' })
            })
        }
    }, [logs.length, rowVirtualizer])

    useEffect(() => {
        const parent = parentRef.current
        if (!parent) {
            return
        }

        const updateAutoScrollState = () => {
            const distanceFromBottom = parent.scrollHeight - parent.scrollTop - parent.clientHeight
            autoScrollEnabledRef.current = distanceFromBottom < 48
        }

        updateAutoScrollState()
        parent.addEventListener('scroll', updateAutoScrollState, { passive: true })

        return () => {
            parent.removeEventListener('scroll', updateAutoScrollState)
        }
    }, [])

    // container 변경 시 로그 초기화
    useEffect(() => {
        pendingLogsRef.current = []
        if (flushFrameRef.current !== null) {
            window.cancelAnimationFrame(flushFrameRef.current)
            flushFrameRef.current = null
        }
        autoScrollEnabledRef.current = true
        setLogs([])
    }, [containerName])

    useEffect(() => () => {
        if (flushFrameRef.current !== null) {
            window.cancelAnimationFrame(flushFrameRef.current)
        }
    }, [])

    return (
        <div className="flex flex-col h-full bg-slate-50 rounded-lg overflow-hidden border border-slate-200 shadow-inner">
            {/* 상태 헤더 */}
            <div className="bg-slate-50 border-b border-slate-200 px-3 py-1.5 flex justify-between items-center text-[10px] text-slate-500 shrink-0">
                <span className="font-mono font-medium">{containerName || 'No container selected'}</span>
                <div className="flex items-center gap-1.5">
                    {isConnecting ? (
                        <>
                            <Loader2 size={10} className="animate-spin text-amber-500" />
                            <span className="text-amber-600 font-medium">Connecting…</span>
                        </>
                    ) : isConnected ? (
                        <>
                            <div className="w-1.5 h-1.5 rounded-full bg-emerald-500 shadow-[0_0_6px_rgba(16,185,129,0.5)]" />
                            <span className="text-emerald-600 font-medium">Live</span>
                        </>
                    ) : (
                        <>
                            <XCircle size={10} className="text-rose-500" />
                            <span className="text-rose-600 font-medium">Disconnected</span>
                        </>
                    )}
                </div>
            </div>

            {/* 로그 콘텐츠 (Virtualized) */}
            <div
                ref={parentRef}
                className="flex-1 overflow-auto bg-slate-50 p-3 font-mono text-xs md:text-sm custom-scrollbar"
            >
                {logs.length === 0 && !isConnected && !isConnecting && (
                    <div className="text-slate-400 text-center py-10 opacity-70">
                        Waiting for log stream…
                    </div>
                )}

                <div
                    style={{
                        height: `${String(rowVirtualizer.getTotalSize())}px`,
                        width: '100%',
                        position: 'relative',
                    }}
                >
                    {rowVirtualizer.getVirtualItems().map((virtualRow) => (
                        <div
                            key={virtualRow.key}
                            style={{
                                position: 'absolute',
                                top: 0,
                                left: 0,
                                width: '100%',
                                height: `${String(virtualRow.size)}px`,
                                transform: `translateY(${String(virtualRow.start)}px)`,
                            }}
                        >
                            <div className="text-slate-700 whitespace-pre hover:bg-slate-100 px-1 rounded leading-relaxed w-fit min-w-full font-mono truncate">
                                <FormattedLogLine line={logs[virtualRow.index] || ''} />
                            </div>
                        </div>
                    ))}
                </div>
            </div>

            {/* 푸터 상태 */}
            <div className="px-3 py-1 bg-slate-50 border-t border-slate-200 text-[10px] text-slate-400 text-right shrink-0">
                {logs.length} lines • {autoScrollEnabledRef.current ? 'Auto-scroll enabled' : 'Auto-scroll paused'}
            </div>
        </div>
    )
}
