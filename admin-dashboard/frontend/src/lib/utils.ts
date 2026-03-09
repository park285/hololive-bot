import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs))
}

/**
 * 시간 변환 유틸리티
 * 백엔드: Unix Timestamp (초 단위)
 * 프론트엔드: JavaScript Date / 밀리초 단위
 */

/** Unix timestamp (초) → Date 객체 변환 */
export function unixToDate(unixSeconds: number): Date {
  return new Date(unixSeconds * 1000)
}

/** Unix timestamp (초) → 밀리초 변환 */
export function unixToMs(unixSeconds: number): number {
  return unixSeconds * 1000
}

/** Date 객체 → Unix timestamp (초) 변환 */
export function dateToUnix(date: Date): number {
  return Math.floor(date.getTime() / 1000)
}

/** Unix timestamp (초)까지 남은 시간 계산 (밀리초) */
export function getRemainingMs(unixSeconds: number): number {
  return unixSeconds * 1000 - Date.now()
}

/** Unix timestamp (초)까지 남은 시간 계산 (분) */
export function getRemainingMinutes(unixSeconds: number): number {
  return Math.floor(getRemainingMs(unixSeconds) / 1000 / 60)
}
