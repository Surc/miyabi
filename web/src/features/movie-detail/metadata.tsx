import { cn } from 'cn'
import type { ReactNode } from 'react'

import type { EntityType, Zone } from '@/api/discover'
import { Badge } from '@/components/ui/badge'
import { MetadataLink } from './metadata-link'

type MetadataEntity = { provider?: string; id?: string; name: string }

export type MovieMetadataValues = {
  maker?: MetadataEntity
  series?: MetadataEntity
  director?: MetadataEntity
  actors: (MetadataEntity & { gender?: string })[]
  tags: MetadataEntity[]
  zone?: Zone | 'unknown'
}

export function MovieMetadata({ movie }: { movie: MovieMetadataValues }) {
  const actors = movie.actors ?? []
  const tags = movie.tags ?? []
  return (
    <dl className="space-y-3 text-sm">
      <EntityRow label="厂牌">
        {movie.maker ? (
          <EntityLink kind="maker" entity={movie.maker} />
        ) : (
          <span className="text-muted-foreground">N/A</span>
        )}
      </EntityRow>
      <EntityRow label="系列">
        {movie.series ? (
          <EntityLink kind="series" entity={movie.series} />
        ) : (
          <span className="text-muted-foreground">N/A</span>
        )}
      </EntityRow>
      {actors.length > 0 ? (
        <EntityRow label="演员">
          {actors.map((actor, index) => (
            <span
              key={`${actor.provider}:${actor.id || actor.name}:${index}`}
              className={cn(
                'inline-flex max-w-full items-center gap-3',
                actor.gender === 'female' && 'text-red-700 dark:text-red-400',
                actor.gender === 'male' && 'text-blue-700 dark:text-blue-400'
              )}
            >
              {index > 0 ? <span className="text-muted-foreground">/</span> : null}
              <EntityLink kind="actor" entity={actor} />
            </span>
          ))}
        </EntityRow>
      ) : null}
      <EntityRow label="导演">
        {movie.director ? (
          <EntityLink kind="director" entity={movie.director} />
        ) : (
          <span className="text-muted-foreground">N/A</span>
        )}
      </EntityRow>
      {tags.length > 0 ? (
        <div className="flex items-start gap-4">
          <dt className="w-10 shrink-0 pt-0.5 leading-5 text-muted-foreground">标签</dt>
          <dd className="flex min-w-0 flex-wrap gap-2">
            {tags.map((tag, index) =>
              tag.id && tag.provider === 'javdb' && movie.zone !== 'unknown' ? (
                <MetadataLink
                  key={tag.id}
                  kind="tag"
                  id={tag.id}
                  name={tag.name}
                  zone={movie.zone}
                  badge
                />
              ) : (
                <Badge
                  key={`${tag.provider}:${tag.id || tag.name}:${index}`}
                  variant="outline"
                  className="h-auto max-w-full break-words whitespace-normal"
                >
                  {tag.name}
                </Badge>
              )
            )}
          </dd>
        </div>
      ) : null}
    </dl>
  )
}

function EntityLink({ kind, entity }: { kind: EntityType; entity: MetadataEntity }) {
  return entity.id && entity.provider === 'javdb' ? (
    <MetadataLink kind={kind} id={entity.id} name={entity.name} />
  ) : (
    <span className="max-w-full break-words">{entity.name}</span>
  )
}

function EntityRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-start gap-4">
      <dt className="w-10 shrink-0 leading-6 text-muted-foreground">{label}</dt>
      <dd className="flex min-w-0 flex-wrap gap-x-3 gap-y-1 leading-6">{children}</dd>
    </div>
  )
}
