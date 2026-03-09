import { useEffect, useState, type CSSProperties, type ReactNode } from 'react'
import CheckCircle2 from 'lucide-react/dist/esm/icons/circle-check-big'
import CircleAlert from 'lucide-react/dist/esm/icons/circle-alert'
import X from 'lucide-react/dist/esm/icons/x'

type ToastVariant = 'success' | 'error'

interface ToastOptions {
  id?: string
}

interface ToastItem {
  id: string
  message: ReactNode
  variant: ToastVariant
}

interface ToasterProps {
  position?: 'top-center'
  reverseOrder?: boolean
  toastOptions?: {
    className?: string
    style?: CSSProperties
    success?: {
      iconTheme?: { primary?: string; secondary?: string }
    }
    error?: {
      iconTheme?: { primary?: string; secondary?: string }
    }
  }
}

const listeners = new Set<(toasts: ToastItem[]) => void>()
let toasts: ToastItem[] = []
const dismissTimers = new Map<string, number>()

const notify = () => {
  listeners.forEach((listener) => listener(toasts))
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
  const id = options?.id ?? `${variant}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
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

    Array.from(dismissTimers.keys()).forEach((toastId) => clearDismissTimer(toastId))
    toasts = []
    notify()
  },
}

const getVariantStyles = (
  variant: ToastVariant,
  toastOptions?: ToasterProps['toastOptions'],
) => {
  if (variant === 'success') {
    return {
      color: toastOptions?.success?.iconTheme?.primary ?? '#0ea5e9',
      Icon: CheckCircle2,
    }
  }

  return {
    color: toastOptions?.error?.iconTheme?.primary ?? '#ef4444',
    Icon: CircleAlert,
  }
}

export const Toaster = ({
  position = 'top-center',
  reverseOrder = false,
  toastOptions,
}: ToasterProps) => {
  const [items, setItems] = useState<ToastItem[]>(toasts)

  useEffect(() => {
    const listener = (nextToasts: ToastItem[]) => {
      setItems(nextToasts)
    }
    listeners.add(listener)
    return () => {
      listeners.delete(listener)
    }
  }, [])

  if (items.length === 0) {
    return null
  }

  const orderedItems = reverseOrder ? [...items].reverse() : items
  const positionClassName = position === 'top-center'
    ? 'top-4 left-1/2 -translate-x-1/2'
    : 'top-4 left-1/2 -translate-x-1/2'

  return (
    <div className={`pointer-events-none fixed z-[100] flex w-full max-w-md flex-col gap-3 px-4 ${positionClassName}`}>
      {orderedItems.map((item) => {
        const { color, Icon } = getVariantStyles(item.variant, toastOptions)
        return (
          <div
            key={item.id}
            className={`pointer-events-auto flex items-start gap-3 rounded-xl border bg-white p-4 shadow-lg ${toastOptions?.className ?? ''}`}
            style={toastOptions?.style}
            role="status"
            aria-live="polite"
          >
            <Icon size={18} style={{ color }} className="mt-0.5 shrink-0" aria-hidden="true" />
            <div className="min-w-0 flex-1 text-sm text-slate-700">
              {item.message}
            </div>
            <button
              type="button"
              className="rounded p-1 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-600"
              onClick={() => { removeToast(item.id) }}
              aria-label="알림 닫기"
            >
              <X size={14} aria-hidden="true" />
            </button>
          </div>
        )
      })}
    </div>
  )
}

export default toast
