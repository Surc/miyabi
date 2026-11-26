import { createFileRoute } from '@tanstack/react-router'

import { MagnetSourcesSection } from '@/features/settings/magnet-sources-section'
import { ActorSourcesSection } from '@/features/settings/scraping-section'
import { JavDBSessionSection } from '@/features/settings/extensions/javdb-session'
import { ScrapingSettings } from '@/features/settings/extensions/scraping-settings'
import { SubtitleSection } from '@/features/settings/subtitle-section'
import { SettingsCard } from '@/features/settings/shared'

export const Route = createFileRoute('/settings/metadata')({
  component: SettingsMetadataPage
})

function SettingsMetadataPage() {
  return (
    <SettingsCard>
      <JavDBSessionSection />
      <ScrapingSettings />
      <ActorSourcesSection />
      <MagnetSourcesSection />
      <SubtitleSection />
    </SettingsCard>
  )
}
