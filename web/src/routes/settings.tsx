import { createFileRoute } from '@tanstack/react-router'

import { AppPage } from '@/components/app-page'
import { PageHeader } from '@/components/page-header'
import { Card, CardContent } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { AppearanceSection } from '@/features/settings/appearance-section'
import { DataSection } from '@/features/settings/data-section'
import { BrowseCacheSection } from '@/features/discover/extensions/cache-settings'
import { EmbySection } from '@/features/settings/emby-section'
import { JavDBSessionSection } from '@/features/settings/extensions/javdb-session'
import { NetworkSection } from '@/features/settings/network-section'
import { PanSection } from '@/features/settings/pan-section'
import { PrivacySection } from '@/features/settings/privacy-section'
import { SubscriptionSection } from '@/features/settings/subscription-section'
import { SubtitleSection } from '@/features/settings/subtitle-section'
import { ScrapingSettings } from '@/features/settings/extensions/scraping-settings'
import { TasksSection } from '@/features/settings/tasks-section'
import { TopSection } from '@/features/settings/top-section'

export const Route = createFileRoute('/settings')({
  component: SettingsPage
})

function SettingsPage() {
  return (
    <AppPage showBackTop={false}>
      <PageHeader title="设置" description="管理应用选项和其他配置" />

      <Card>
        <CardContent className="space-y-8">
          <AppearanceSection />
          <Separator />
          <PrivacySection />
          <Separator />
          <TopSection />
          <Separator />
          <NetworkSection />
          <Separator />
          <JavDBSessionSection />
          <Separator />
          <ScrapingSettings />
          <Separator />
          <SubtitleSection />
          <Separator />
          <EmbySection />
          <Separator />
          <PanSection />
          <Separator />
          <SubscriptionSection />
          <Separator />
          <TasksSection />
          <Separator />
          <DataSection />
          <Separator />
          <BrowseCacheSection />
        </CardContent>
      </Card>
    </AppPage>
  )
}
