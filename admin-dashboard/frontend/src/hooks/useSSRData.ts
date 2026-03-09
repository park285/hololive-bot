/**
 * SSR 데이터 소비 훅
 * 반복되는 SSR 데이터 소비 패턴을 중앙화
 */

import { useMemo } from 'react'
import { consumeSSRData, type SSRData } from '@/utils/ssr'

/**
 * SSR 데이터를 소비하고 변환하는 훅
 *
 * @param key - SSR 데이터 키
 * @param validator - 데이터 유효성 검사 및 변환 함수
 * @returns 변환된 SSR 데이터 또는 undefined
 *
 * @example
 * ```tsx
 * const ssrData = useSSRData('members', (data) =>
 *   data?.status === 'ok' && data.members ? (data as MembersResponse) : undefined
 * )
 * ```
 */
export function useSSRData<K extends keyof SSRData, R>(
    key: K,
    validator: (data: SSRData[K] | undefined) => R | undefined
): R | undefined {
    return useMemo(
        () => {
            const ssrData = consumeSSRData(key)
            return validator(ssrData)
        },
        // eslint-disable-next-line react-hooks/exhaustive-deps -- SSR 데이터는 최초 마운트 시 한 번만 소비
        []
    )
}
