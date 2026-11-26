import { createFileRoute } from '@tanstack/react-router'

import { Separator } from '@/components/ui/separator'
import { BrowseCacheSection } from '@/features/discover/extensions/cache-settings'
import { DataSection } from '@/features/settings/data-section'
import { MediaSyncSection } from '@/features/settings/media-sync-section'
import { TasksSection } from '@/features/settings/tasks-section'
import { SettingsCard } from '@/features/settings/shared'

export const Route = createFileRoute('/settings/data')({
  component: SettingsDataPage
})

function SettingsDataPage() {
  return (
    <SettingsCard>
      <MediaSyncSection />
      <Separator />
      <TasksSection />
      <Separator />
      <DataSection />
      <Separator />
      <BrowseCacheSection />
    </SettingsCard>
  )
}
