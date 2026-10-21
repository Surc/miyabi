import type { DiscoverMovie } from '../../../api/discover'
import type { Cached, ResourceSnapshot, ResourceStatus } from '../../../api/browse-cache'

export type PreloadPhase = 'loading' | 'paused' | 'error' | 'complete'
export type PreloadRow = { movie: DiscoverMovie; resources?: Cached<ResourceStatus> }
type Entry = PreloadRow & { failed?: boolean }
export type PreloadState = {
  rows: PreloadRow[]
  phase: PreloadPhase
  target: number
  ready: number
  exhausted: boolean
  error?: string
}
type Fetchers = {
  page: (
    page: number,
    signal: AbortSignal,
    preview: (movies: DiscoverMovie[]) => void
  ) => Promise<DiscoverMovie[]>
  resources: (
    id: string,
    signal: AbortSignal,
    cached: (data: Cached<ResourceSnapshot>) => void
  ) => Promise<Cached<ResourceSnapshot>>
}

// Source pages and display pages have different meanings. Only complete,
// fresh, matching resources count toward the configurable display-page target.
export class BrowsePreloader {
  private entries: Entry[] = []
  private ids = new Set<string>()
  private sourcePage = 1
  private ended = false
  private target = 120
  private phase: PreloadPhase = 'paused'
  private error?: string
  private preview: DiscoverMovie[] = []
  private abort?: AbortController
  private running?: Promise<void>
  private userPaused = false
  private leases = 0
  private listeners = new Set<() => void>()
  private state: PreloadState = {
    rows: [],
    phase: 'paused',
    target: 120,
    ready: 0,
    exhausted: false
  }

  constructor(
    private fetchers: Fetchers,
    private subtitlesOnly: boolean
  ) {}
  subscribe = (listener: () => void) => {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }
  getSnapshot = () => this.state
  // React StrictMode releases and reacquires effects synchronously. Only abort
  // when no mounted consumer remains after that cycle.
  attach() {
    this.leases++
    return () => {
      this.leases--
      queueMicrotask(() => {
        if (!this.leases) this.destroy()
      })
    }
  }
  private matches(entry: Entry) {
    return !this.subtitlesOnly || entry.resources?.data.has_subtitle === true
  }
  private ready(entry: Entry) {
    return !!entry.resources && !entry.resources.stale && entry.resources.data.complete
  }
  private emit() {
    const rows = this.entries.filter(entry => this.matches(entry))
    this.state = {
      rows: rows.length || this.subtitlesOnly ? rows : this.preview.map(movie => ({ movie })),
      phase: this.phase,
      target: this.target,
      ready: this.entries.filter(entry => this.matches(entry) && this.ready(entry)).length,
      exhausted: this.ended,
      error: this.error
    }
    for (const listener of this.listeners) listener()
  }
  private acceptResource(entry: Entry, result: Cached<ResourceSnapshot>) {
    entry.movie = {
      ...entry.movie,
      title: result.data.movie.title || entry.movie.title,
      cover: result.data.movie.cover || entry.movie.cover
    }
    entry.resources = {
      ...result,
      data: {
        complete: result.data.complete,
        has_subtitle: result.data.has_subtitle,
        has_cracked: result.data.has_cracked,
        magnet_count: result.data.magnets.length
      }
    }
  }
  setTarget(page: number, pages: number) {
    this.target = Math.ceil(page / pages) * pages * 20
    if (!this.userPaused && this.phase !== 'error') void this.start()
    this.emit()
  }
  expireResources(ttl: number, now = Date.now()) {
    let expired = false
    for (const entry of this.entries) {
      if (
        entry.resources &&
        !entry.resources.stale &&
        now - new Date(entry.resources.updated_at).getTime() >= ttl
      ) {
        entry.resources = { ...entry.resources, stale: true }
        expired = true
      }
    }
    if (expired) this.emit()
    return expired
  }
  start() {
    if (this.running) return this.running
    this.userPaused = false
    this.phase = 'loading'
    this.error = undefined
    const abort = new AbortController()
    this.abort = abort
    this.emit()
    const run = this.pump(abort.signal)
      .catch(error => {
        if (!abort.signal.aborted) {
          this.phase = 'error'
          this.error = error instanceof Error ? error.message : '预加载失败，请重试'
        }
      })
      .finally(() => {
        this.running = undefined
        this.emit()
      })
    this.running = run
    return run
  }
  pause() {
    this.userPaused = true
    this.phase = 'paused'
    this.abort?.abort()
    this.emit()
  }
  async resume() {
    if (this.running) await this.running
    return this.start()
  }
  destroy() {
    this.pause()
    this.listeners.clear()
  }
  private async pump(signal: AbortSignal) {
    while (!signal.aborted) {
      const pending = this.entries.filter(entry => !this.ready(entry))
      if (pending.length) {
        let cursor = 0
        await Promise.all(
          Array.from({ length: 2 }, async () => {
            while (!signal.aborted) {
              const entry = pending[cursor++]
              if (!entry) break
              try {
                const resources = await this.fetchers.resources(entry.movie.id, signal, value => {
                  this.acceptResource(entry, value)
                  this.emit()
                })
                if (signal.aborted) break
                this.acceptResource(entry, resources)
                entry.failed = !this.ready(entry)
              } catch (error) {
                if (signal.aborted) break
                entry.failed = true
                this.error = error instanceof Error ? error.message : '资源状态待确认'
              }
              this.emit()
            }
          })
        )
        if (signal.aborted) return
        if (this.entries.some(entry => entry.failed))
          throw new Error(this.error ?? '部分磁力来源未完成，请重试；已有数据已保留')
        if (this.subtitlesOnly) this.entries = this.entries.filter(entry => this.matches(entry))
      }
      const count = this.entries.filter(entry => this.ready(entry) && this.matches(entry)).length
      if (count >= this.target || this.ended) {
        this.phase = 'complete'
        return
      }
      const movies = await this.fetchers.page(this.sourcePage, signal, items => {
        if (!this.entries.length) {
          this.preview = items
          this.emit()
        }
      })
      if (signal.aborted) return
      if (!movies.length) {
        this.ended = true
        this.preview = []
        continue
      }
      let added = 0
      for (const movie of movies) {
        if (this.ids.has(movie.id)) continue
        this.ids.add(movie.id)
        this.entries.push({
          movie: { ...movie, actors: [], tags: [], preview_images: [], preview_video: '' }
        })
        added++
      }
      // A non-empty repeated page is a pagination failure, not proof of EOF.
      if (!added) throw new Error('来源返回了重复页，请刷新列表或稍后重试')
      this.sourcePage++
      this.preview = []
      this.emit()
    }
  }
}
