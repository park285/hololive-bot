import { lazy, type ComponentType, type LazyExoticComponent } from 'react'

export interface RouteDefinition {
  id: string
  path: string
  absolutePath: string
  load: () => Promise<{ default: ComponentType }>
}

export const ROUTE_DEFINITIONS: RouteDefinition[] = [
  {
    id: 'stats',
    path: 'stats',
    absolutePath: '/dashboard/stats',
    load: () => import('@/components/StatsTab'),
  },
  {
    id: 'streams',
    path: 'streams',
    absolutePath: '/dashboard/streams',
    load: () => import('@/components/StreamsTab'),
  },
  {
    id: 'members',
    path: 'members',
    absolutePath: '/dashboard/members',
    load: () => import('@/components/MembersTab'),
  },
  {
    id: 'milestones',
    path: 'milestones',
    absolutePath: '/dashboard/milestones',
    load: () => import('@/components/MilestonesTab'),
  },
  {
    id: 'alarms',
    path: 'alarms',
    absolutePath: '/dashboard/alarms',
    load: () => import('@/components/AlarmsTab'),
  },
  {
    id: 'rooms',
    path: 'rooms',
    absolutePath: '/dashboard/rooms',
    load: () => import('@/components/RoomsTab'),
  },
  {
    id: 'logs',
    path: 'logs',
    absolutePath: '/dashboard/logs',
    load: () => import('@/components/LogsTab'),
  },
  {
    id: 'settings',
    path: 'settings',
    absolutePath: '/dashboard/settings',
    load: () => import('@/components/SettingsTab'),
  },
]

const lazyCache: Record<string, LazyExoticComponent<ComponentType>> = {}

export const getLazyComponent = (id: string) => {
  if (!lazyCache[id]) {
    const route = ROUTE_DEFINITIONS.find((item) => item.id === id)
    if (!route) {
      throw new Error(`Route ${id} not found in route definitions`)
    }

    lazyCache[id] = lazy(route.load)
  }

  return lazyCache[id]
}

const prefetchedSet = new Set<string>()

export const prefetchRoute = (id: string) => {
  if (prefetchedSet.has(id)) return

  const route = ROUTE_DEFINITIONS.find((item) => item.id === id)
  if (!route) return

  prefetchedSet.add(id)
  void route.load()
}
