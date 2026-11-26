import { createFileRoute } from '@tanstack/react-router'

import { Separator } from '@/components/ui/separator'
import { AppearanceSection } from '@/features/settings/appearance-section'
import { PrivacySection } from '@/features/settings/privacy-section'
import { TopSection } from '@/features/settings/top-section'
import { SettingsCard } from '@/features/settings/shared'

export const Route = createFileRoute('/settings/')({
  component: SettingsGeneralPage
})

function SettingsGeneralPage() {
  return (
    <SettingsCard>
      <AppearanceSection />
      <Separator />
      <PrivacySection />
      <Separator />
      <TopSection />
    </SettingsCard>
  )
}
