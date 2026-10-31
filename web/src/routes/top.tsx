import { createFileRoute } from '@tanstack/react-router'

import { AppPage } from '@/components/app-page'
import { ErrorState } from '@/components/error-state'
import { TopPage } from '@/features/top/page'
import { validateTopSearch } from '@/features/top/search'

export const Route = createFileRoute('/top')({
  validateSearch: validateTopSearch,
  component: TopPage,
  errorComponent: () => (
    <AppPage>
      <ErrorState message="榜单或页码无效" />
    </AppPage>
  )
})
