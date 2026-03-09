import { useState, useEffect, useRef, useCallback } from 'react'

const CHANNEL_NAME = 'admin_session'
const THROTTLE_MS = 1000 // Local throttle: 1s
const BROADCAST_THROTTLE_MS = 5000 // Broadcast throttle: 5s

interface TabSyncMessage {
    type: 'ACTIVITY' | 'LOGOUT'
    timestamp: number
}

/**
 * 사용자 활동 감지 훅 (멀티 탭 동기화 포함)
 *
 * - 현재 탭에서 활동 감지 시 다른 탭에도 BroadcastChannel로 알림
 * - 다른 탭에서 활동 알림 수신 시 현재 탭의 Idle 타이머도 리셋
 * - 이를 통해 "모든 탭이 동시에 Idle 상태일 때만" idle=true 전송 (팀킬 방지)
 * - 성능 최적화를 위해 이벤트 핸들링과 브로드캐스팅을 스로틀링합니다.
 *
 * @param idleTimeoutMs 유휴 상태로 간주할 시간 (밀리초)
 * @returns isIdle: 유휴 상태 여부
 */
export function useActivityDetection(idleTimeoutMs: number) {
    const [isIdle, setIsIdle] = useState(false)
    const timeoutRef = useRef<number | null>(null)
    const channelRef = useRef<BroadcastChannel | null>(null)
    const lastActivityRef = useRef<number>(0)
    const lastBroadcastRef = useRef<number>(0)

    // 타이머 리셋 (로컬 전용, 브로드캐스트 안 함)
    const resetTimerInternal = useCallback(() => {
        setIsIdle(false)

        if (timeoutRef.current) {
            window.clearTimeout(timeoutRef.current)
        }

        timeoutRef.current = window.setTimeout(() => {
            setIsIdle(true)
        }, idleTimeoutMs)
    }, [idleTimeoutMs])

    // 타이머 리셋 + 다른 탭에 브로드캐스트 (Throttled)
    const resetTimer = useCallback(() => {
        const now = Date.now()

        // 1. Local Throttle
        if (now - lastActivityRef.current < THROTTLE_MS) {
            return
        }
        lastActivityRef.current = now
        resetTimerInternal()

        // 2. Broadcast Throttle
        if (now - lastBroadcastRef.current < BROADCAST_THROTTLE_MS) {
            return
        }

        // 다른 탭에 활동 알림 (BroadcastChannel)
        if (channelRef.current) {
            const message: TabSyncMessage = {
                type: 'ACTIVITY',
                timestamp: now,
            }
            channelRef.current.postMessage(message)
            lastBroadcastRef.current = now
        }
    }, [resetTimerInternal])

    // BroadcastChannel 설정 (다른 탭에서 활동 수신 시 타이머 리셋)
    useEffect(() => {
        if (typeof BroadcastChannel === 'undefined') {
            // BroadcastChannel 미지원 브라우저
            return
        }

        channelRef.current = new BroadcastChannel(CHANNEL_NAME)

        channelRef.current.onmessage = (event: MessageEvent<TabSyncMessage>) => {
            if (event.data.type === 'ACTIVITY') {
                // 다른 탭에서 활동 감지 → 현재 탭 타이머 리셋 (브로드캐스트 안 함)
                resetTimerInternal()
            }
        }

        return () => {
            channelRef.current?.close()
            channelRef.current = null
        }
    }, [resetTimerInternal])

    // 이벤트 리스너 설정
    useEffect(() => {
        const events = ['mousemove', 'keydown', 'click', 'scroll', 'touchstart']

        // Passive listener for better scroll performance
        events.forEach(event => { document.addEventListener(event, resetTimer, { passive: true }); })

        // 초기 타이머 시작
        resetTimerInternal()

        return () => {
            events.forEach(event => { document.removeEventListener(event, resetTimer); })
            if (timeoutRef.current) {
                window.clearTimeout(timeoutRef.current)
            }
        }
    }, [resetTimer, resetTimerInternal])

    return isIdle
}
