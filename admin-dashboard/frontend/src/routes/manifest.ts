import type { LucideIcon } from 'lucide-react'
import Bell from 'lucide-react/dist/esm/icons/bell'
import LayoutDashboard from 'lucide-react/dist/esm/icons/layout-dashboard'
import MessageSquare from 'lucide-react/dist/esm/icons/message-square'
import Radio from 'lucide-react/dist/esm/icons/radio'
import ScrollText from 'lucide-react/dist/esm/icons/scroll-text'
import Settings from 'lucide-react/dist/esm/icons/settings'
import Trophy from 'lucide-react/dist/esm/icons/trophy'
import Users from 'lucide-react/dist/esm/icons/users'
import { ROUTE_DEFINITIONS, prefetchRoute } from '@/routes/route-definitions'

export type RouteGroup = 'Overview' | 'Hololive Bot' | 'Infrastructure'

export interface RouteManifestItem {
  id: string
  path: string
  absolutePath: string
  label: string
  icon: LucideIcon
  group: RouteGroup
}

const ROUTE_METADATA: Record<string, Omit<RouteManifestItem, 'path' | 'absolutePath'>> = {
  stats: {
    id: 'stats',
    label: '통합 대시보드',
    icon: LayoutDashboard,
    group: 'Overview',
  },
  streams: {
    id: 'streams',
    label: '방송 현황',
    icon: Radio,
    group: 'Hololive Bot',
  },
  members: {
    id: 'members',
    label: '멤버 관리',
    icon: Users,
    group: 'Hololive Bot',
  },
  milestones: {
    id: 'milestones',
    label: '마일스톤',
    icon: Trophy,
    group: 'Hololive Bot',
  },
  alarms: {
    id: 'alarms',
    label: '알람 관리',
    icon: Bell,
    group: 'Hololive Bot',
  },
  rooms: {
    id: 'rooms',
    label: '방 관리',
    icon: MessageSquare,
    group: 'Hololive Bot',
  },
  logs: {
    id: 'logs',
    label: '로그',
    icon: ScrollText,
    group: 'Infrastructure',
  },
  settings: {
    id: 'settings',
    label: '설정',
    icon: Settings,
    group: 'Infrastructure',
  },
}

export const ROUTE_MANIFEST: RouteManifestItem[] = ROUTE_DEFINITIONS.map((route) => {
  const metadata = ROUTE_METADATA[route.id]
  if (!metadata) {
    throw new Error(`Route ${route.id} metadata not found`)
  }

  return {
    ...metadata,
    path: route.path,
    absolutePath: route.absolutePath,
  }
})

export { prefetchRoute }

export const getNavGroups = () => {
  const groups: { title: string; items: RouteManifestItem[] }[] = []
  const order: RouteGroup[] = ['Overview', 'Hololive Bot', 'Infrastructure']

  order.forEach((groupName) => {
    const items = ROUTE_MANIFEST.filter((route) => route.group === groupName)
    if (items.length > 0) {
      groups.push({ title: groupName, items })
    }
  })

  return groups
}
