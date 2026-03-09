import { useEffect, useRef, useCallback } from 'react'

const CHANNEL_NAME = 'admin_session'

interface TabSyncMessage {
    type: 'ACTIVITY' | 'LOGOUT'
    timestamp: number
}

/**
 * 멀티 탭 세션 동기화 훅
 * BroadcastChannel API를 사용하여 탭 간 활동 상태를 동기화합니다.
 *
 * - 한 탭에서 활동이 감지되면 다른 탭의 Idle 타이머도 리셋됩니다.
 * - 이를 통해 "모든 탭이 동시에 Idle 상태일 때만" idle=true가 전송됩니다.
 *
 * @param onActivityFromOtherTab 다른 탭에서 활동이 감지되었을 때 호출되는 콜백
 * @param onLogoutFromOtherTab 다른 탭에서 로그아웃되었을 때 호출되는 콜백
 * @returns broadcastActivity: 현재 탭에서 활동 발생 시 호출할 함수
 */
export function useTabSync(
    onActivityFromOtherTab: () => void,
    onLogoutFromOtherTab?: () => void
) {
    const channelRef = useRef<BroadcastChannel | null>(null)

    // 다른 탭에 활동 신호 전송
    const broadcastActivity = useCallback(() => {
        if (channelRef.current) {
            const message: TabSyncMessage = {
                type: 'ACTIVITY',
                timestamp: Date.now(),
            }
            channelRef.current.postMessage(message)
        }
    }, [])

    // 다른 탭에 로그아웃 신호 전송
    const broadcastLogout = useCallback(() => {
        if (channelRef.current) {
            const message: TabSyncMessage = {
                type: 'LOGOUT',
                timestamp: Date.now(),
            }
            channelRef.current.postMessage(message)
        }
    }, [])

    useEffect(() => {
        // BroadcastChannel 지원 여부 확인
        if (typeof BroadcastChannel === 'undefined') {
            console.warn('BroadcastChannel API is not supported in this browser')
            return
        }

        // 채널 생성
        channelRef.current = new BroadcastChannel(CHANNEL_NAME)

        // 다른 탭에서 메시지 수신 시 처리
        channelRef.current.onmessage = (event: MessageEvent<TabSyncMessage>) => {
            const { type } = event.data

            switch (type) {
                case 'ACTIVITY':
                    // 다른 탭에서 활동 감지 → 현재 탭의 Idle 타이머 리셋
                    onActivityFromOtherTab()
                    break
                case 'LOGOUT':
                    // 다른 탭에서 로그아웃 → 현재 탭도 로그아웃
                    if (onLogoutFromOtherTab) {
                        onLogoutFromOtherTab()
                    }
                    break
            }
        }

        return () => {
            channelRef.current?.close()
            channelRef.current = null
        }
    }, [onActivityFromOtherTab, onLogoutFromOtherTab])

    return { broadcastActivity, broadcastLogout }
}
