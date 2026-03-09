/**
 * SSR 데이터 유틸리티
 *
 * Go 서버에서 주입한 window.__SSR_DATA__를 활용합니다.
 * 이를 TanStack Query의 initialData로 사용하여 초기 로딩 시 데이터 페칭을 생략합니다.
 */

import type { Member, DockerContainer } from '@/types'

export interface SSRData {
    members?: MembersSSRData
    settings?: SettingsSSRData
    docker?: DockerHealthSSRData
    containers?: ContainersSSRData
}

interface MembersSSRData {
    status: string
    members?: Member[]
}

interface SettingsSSRData {
    status: string
    settings?: {
        alarmAdvanceMinutes: number
    }
}

interface DockerHealthSSRData {
    status: string
    available: boolean
}

interface ContainersSSRData {
    status: string
    containers?: DockerContainer[]
}

// 타입 선언 (window에 __SSR_DATA__ 추가)
declare global {
    interface Window {
        __SSR_DATA__?: SSRData
    }
}

/**
 * SSR 데이터 전체를 가져옵니다.
 * 데이터가 없으면 undefined를 반환합니다.
 */
export function getSSRData(): SSRData | undefined {
    if (typeof window === 'undefined') return undefined
    return window.__SSR_DATA__
}

/**
 * 특정 키의 SSR 데이터를 가져옵니다.
 * 데이터가 없거나 해당 키가 없으면 undefined를 반환합니다.
 */
export function getSSRDataFor<K extends keyof SSRData>(key: K): SSRData[K] | undefined {
    const data = getSSRData()
    if (!data) return undefined
    return data[key]
}

/**
 * SSR 데이터를 소비(consume)하고 제거합니다.
 * 한 번 사용된 SSR 데이터는 이후 클라이언트 페칭으로 대체되어야 하므로,
 * 초기 로드 후 제거하는 것이 안전합니다.
 *
 * @param key - 소비할 SSR 데이터 키
 * @returns 해당 키의 SSR 데이터 (있었다면)
 */
export function consumeSSRData<K extends keyof SSRData>(key: K): SSRData[K] | undefined {
    const data = getSSRDataFor(key)
    if (data && typeof window !== 'undefined' && window.__SSR_DATA__) {
        switch (key) {
            case 'members':
                window.__SSR_DATA__.members = undefined
                break
            case 'settings':
                window.__SSR_DATA__.settings = undefined
                break
            case 'docker':
                window.__SSR_DATA__.docker = undefined
                break
            case 'containers':
                window.__SSR_DATA__.containers = undefined
                break
        }
    }
    return data
}

/**
 * SSR 데이터가 존재하는지 확인합니다.
 */
export function hasSSRData(): boolean {
    return typeof window !== 'undefined' && window.__SSR_DATA__ !== undefined
}

