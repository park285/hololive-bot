import type { ReactNode } from 'react'

type ToastVariant = 'success' | 'error'

interface ToastOptions {
  id?: string
}

export interface ToastItem {
  id: string
  message: ReactNode
  variant: ToastVariant
}

const listeners = new Set<(toasts: ToastItem[]) => void>()
let toasts: ToastItem[] = []
const dismissTimers = new Map<string, number>()

export const subscribeToToasts = (listener: (toasts: ToastItem[]) => void) => {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export const getToastItems = () => toasts

const notify = () => {
  listeners.forEach((listener) => {
    listener(toasts)
  })
}

const clearDismissTimer = (id: string) => {
  const timer = dismissTimers.get(id)
  if (timer !== undefined) {
    window.clearTimeout(timer)
    dismissTimers.delete(id)
  }
}

const removeToast = (id: string) => {
  clearDismissTimer(id)
  toasts = toasts.filter((toast) => toast.id !== id)
  notify()
}

const scheduleDismiss = (id: string, variant: ToastVariant) => {
  clearDismissTimer(id)
  const timeoutMs = variant === 'success' ? 3000 : 4500
  const timer = window.setTimeout(() => {
    removeToast(id)
  }, timeoutMs)
  dismissTimers.set(id, timer)
}

const pushToast = (variant: ToastVariant, message: ReactNode, options?: ToastOptions) => {
  const id = options?.id ?? `${variant}-${String(Date.now())}-${Math.random().toString(36).slice(2, 8)}`
  const nextToast: ToastItem = { id, message, variant }

  toasts = [nextToast, ...toasts.filter((toast) => toast.id !== id)].slice(0, 4)
  notify()
  scheduleDismiss(id, variant)

  return id
}

const toast = {
  success: (message: ReactNode, options?: ToastOptions) => pushToast('success', message, options),
  error: (message: ReactNode, options?: ToastOptions) => pushToast('error', message, options),
  dismiss: (id?: string) => {
    if (id) {
      removeToast(id)
      return
    }

    Array.from(dismissTimers.keys()).forEach((toastId) => {
      clearDismissTimer(toastId)
    })
    toasts = []
    notify()
  },
}

export default toast
