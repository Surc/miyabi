export function TopRankBadge({ rank, name }: { rank: number; name?: string }) {
  return (
    <span className="inline-flex max-w-full overflow-hidden rounded-md text-xs font-medium">
      <span className="shrink-0 bg-zinc-800 px-2 py-1 text-white tabular-nums">No.{rank}</span>
      {name ? (
        <span className="min-w-0 bg-amber-200 px-2 py-1 text-amber-950 dark:bg-amber-300">
          {name}
        </span>
      ) : null}
    </span>
  )
}
