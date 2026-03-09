import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

type StatVariant = 'blue' | 'green' | 'yellow' | 'rose' | 'indigo' | 'cyan'

interface StatCardProps {
  label: string
  value: number | string
  icon: ReactNode
  variant?: StatVariant
  className?: string
}

const VARIANTS: Record<StatVariant, { bg: string; text: string; ring: string }> = {
  blue: { bg: 'bg-blue-50', text: 'text-blue-600', ring: 'ring-blue-100' },
  green: { bg: 'bg-emerald-50', text: 'text-emerald-600', ring: 'ring-emerald-100' },
  yellow: { bg: 'bg-amber-50', text: 'text-amber-600', ring: 'ring-amber-100' },
  rose: { bg: 'bg-rose-50', text: 'text-rose-600', ring: 'ring-rose-100' },
  indigo: { bg: 'bg-indigo-50', text: 'text-indigo-600', ring: 'ring-indigo-100' },
  cyan: { bg: 'bg-cyan-50', text: 'text-cyan-600', ring: 'ring-cyan-100' },
}

export function StatCard({
  label,
  value,
  icon,
  variant = 'blue',
  className,
}: StatCardProps) {
  const style = VARIANTS[variant]

  return (
    <div
      className={cn(
        'relative overflow-hidden rounded-2xl border border-slate-100 bg-white p-6 shadow-sm transition-all duration-200 hover:-translate-y-1 hover:shadow-md',
        className,
      )}
    >
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm font-medium text-slate-500 mb-1">{label}</p>
          <h3 className="text-3xl font-bold text-slate-800 tracking-tight tabular-nums">
            {typeof value === 'number' ? value.toLocaleString() : value}
          </h3>
        </div>
        <div className={cn('p-3 rounded-xl ring-4 ring-opacity-50', style.bg, style.text, style.ring)}>
          {icon}
        </div>
      </div>

      {/* Decorative background circle */}
      <div className={cn('absolute -bottom-6 -right-6 w-24 h-24 rounded-full opacity-10', style.bg.replace('50', '200'))} />
    </div>
  )
}
