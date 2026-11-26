import { Link, useLocation } from '@tanstack/react-router'
import {
  BookmarkIcon,
  ClapperboardIcon,
  CompassIcon,
  FilmIcon,
  HistoryIcon,
  SearchIcon,
  SettingsIcon,
  TrophyIcon,
  UsersIcon,
  type LucideIcon
} from 'lucide-react'

import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar
} from '@/components/ui/sidebar'
import { useTopConfig } from '@/api/top'

type AppNavItem = {
  label: string
  icon: LucideIcon
  to:
    | '/'
    | '/movies'
    | '/actors'
    | '/history'
    | '/discover'
    | '/search'
    | '/subscriptions'
    | '/settings'
    | '/top'
}

export const APP_NAV_ITEMS: readonly AppNavItem[] = [
  { label: '媒体库', icon: FilmIcon, to: '/' },
  { label: '影片', icon: ClapperboardIcon, to: '/movies' },
  { label: '演员', icon: UsersIcon, to: '/actors' },
  { label: '关注', icon: BookmarkIcon, to: '/subscriptions' },
  { label: '探索', icon: CompassIcon, to: '/discover' },
  { label: 'JavDB 搜索', icon: SearchIcon, to: '/search' },
  { label: '观看历史', icon: HistoryIcon, to: '/history' },
  { label: 'TOP 榜单', icon: TrophyIcon, to: '/top' },
  { label: '设置', icon: SettingsIcon, to: '/settings' }
]

export function AppNavigation() {
  const pathname = useLocation({ select: location => location.pathname })
  const { setOpenMobile } = useSidebar()
  const topConfig = useTopConfig()

  return (
    <SidebarMenu className="gap-1">
      {APP_NAV_ITEMS.filter(item => item.to !== '/top' || topConfig.data?.show_navigation).map(
        item => {
          const active =
            item.to === '/'
              ? pathname === '/'
              : pathname === item.to || pathname.startsWith(`${item.to}/`)

          return (
            <SidebarMenuItem key={item.to}>
              <SidebarMenuButton
                render={
                  <Link
                    to={item.to}
                    activeOptions={{ exact: item.to === '/', includeSearch: false }}
                    onClick={() => setOpenMobile(false)}
                  />
                }
                isActive={active}
                tooltip={item.label}
              >
                <item.icon aria-hidden="true" />
                <span>{item.label}</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          )
        }
      )}
    </SidebarMenu>
  )
}
