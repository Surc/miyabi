import { Link } from '@tanstack/react-router'

import { TOP_PAGE_SIZE, useMovieRankings, useTopConfig } from '@/api/top'
import { TopRankBadge } from '@/features/top/rank-badge'

export function MovieTopRankings({ code }: { code: string }) {
  const settings = useTopConfig()
  return settings.data?.show_rankings ? <Rankings code={code} /> : null
}

function Rankings({ code }: { code: string }) {
  const rankings = useMovieRankings(code)
  return rankings.data?.map(ranking => (
    <Link
      key={ranking.board_id}
      to="/top"
      search={{ board: ranking.board_id, page: Math.ceil(ranking.rank / TOP_PAGE_SIZE) }}
      className="inline-flex max-w-full rounded-md outline-ring transition-opacity hover:opacity-80"
      title={`查看${ranking.name}，第 ${ranking.rank} 名`}
    >
      <TopRankBadge rank={ranking.rank} name={ranking.name} />
    </Link>
  ))
}
