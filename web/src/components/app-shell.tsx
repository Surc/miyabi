import type { PropsWithChildren } from 'react'
import { BellIcon, CompassIcon, FilmIcon, SearchIcon, SettingsIcon, TrophyIcon } from 'lucide-react'

import { FloatingNav, type FloatingNavItem } from '@/components/floating-nav'
import { Toaster } from '@/components/ui/sonner'
import { TaskEventsProvider } from '@/features/tasks/task-events'
import { TaskNotifications } from '@/features/tasks/task-notifications'
import { useTopConfig } from '@/api/top'

const NAV_ITEMS: FloatingNavItem[] = [
  { id: 'library', label: '媒体库', icon: FilmIcon, to: '/' },
  { id: 'subscriptions', label: '订阅', icon: BellIcon, to: '/subscriptions' },
  { id: 'discover', label: '发现', icon: CompassIcon, to: '/discover' },
  { id: 'top', label: 'TOP 榜单', icon: TrophyIcon, to: '/top' },
  { id: 'search', label: '搜索', icon: SearchIcon, to: '/search' },
  { id: 'settings', label: '设置', icon: SettingsIcon, to: '/settings' }
]

export function AppShell({ children }: PropsWithChildren) {
  const settings = useTopConfig()
  const showTopNavigation = settings.data?.show_navigation ?? false

  return (
    <TaskEventsProvider>
      <div className="relative min-h-dvh">
        <FloatingNav items={NAV_ITEMS.filter(item => item.id !== 'top' || showTopNavigation)} />
        {children}
        <Toaster position="top-right" closeButton duration={6000} />
        <TaskNotifications />
      </div>
    </TaskEventsProvider>
  )
}
