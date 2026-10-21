// Cancellable FIFO slots reserve the next token before waking its waiter.
// This avoids bursts when several mounted cards refresh ratings together.
export class RequestSlots {
  private active = 0
  private waiters: Array<() => void> = []
  constructor(private limit: number) {}
  private canceled() {
    return new DOMException('Canceled', 'AbortError')
  }
  private acquire(signal: AbortSignal): Promise<() => void> {
    if (signal.aborted) return Promise.reject(this.canceled())
    if (this.active < this.limit) {
      this.active++
      return Promise.resolve(() => this.release())
    }
    return new Promise((resolve, reject) => {
      const enter = () => {
        signal.removeEventListener('abort', cancel)
        resolve(() => this.release())
      }
      const cancel = () => {
        const index = this.waiters.indexOf(enter)
        if (index >= 0) this.waiters.splice(index, 1)
        reject(this.canceled())
      }
      this.waiters.push(enter)
      signal.addEventListener('abort', cancel, { once: true })
    })
  }
  private release() {
    const next = this.waiters.shift()
    if (next) next()
    else this.active--
  }
  async run<T>(signal: AbortSignal, request: () => Promise<T>): Promise<T> {
    const release = await this.acquire(signal)
    try {
      if (signal.aborted) throw this.canceled()
      return await request()
    } finally {
      release()
    }
  }
}
