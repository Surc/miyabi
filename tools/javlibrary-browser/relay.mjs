import { setTimeout as delay } from 'node:timers/promises';
import { BrowserError } from './session.mjs';

export function relayURL(origin) {
  const url = new URL(origin);
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.href !== url.origin + '/') {
    throw new Error('miyabi_url must be an HTTP(S) origin');
  }
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  url.pathname = '/api/extensions/movie/javlibrary-browser';
  return url;
}

export async function runRelay(url, token, session, signal, setConnected) {
  while (!signal.aborted) {
    await connect(url, token, session, signal, setConnected);
    await delay(3000, undefined, { signal, ref: false }).catch(() => {});
  }
}

function connect(url, token, session, signal, setConnected) {
  return new Promise(resolve => {
    const socket = new WebSocket(url, { headers: { Authorization: 'Bearer ' + token } });
    let lastReceived = Date.now();
    let stopped = false;
    let active;
    let pending = Promise.resolve();
    const send = value => {
      if (socket.readyState !== WebSocket.OPEN) return;
      try { socket.send(JSON.stringify(value)); } catch { socket.close(); }
    };
    const stop = () => {
      if (stopped) return;
      stopped = true;
      clearInterval(heartbeat);
      signal.removeEventListener('abort', stop);
      active?.controller.abort();
      socket.close();
      setConnected(false);
      void pending.then(resolve);
    };
    const heartbeat = setInterval(() => {
      if (Date.now() - lastReceived > 40_000) stop();
      else send({});
    }, 15_000);
    heartbeat.unref();
    signal.addEventListener('abort', stop, { once: true });
    socket.addEventListener('error', stop, { once: true });
    socket.addEventListener('close', stop, { once: true });
    socket.addEventListener('open', () => { send({}); }, { once: true });
    socket.addEventListener('message', ({ data }) => {
      if (stopped) return;
      let message;
      try { message = JSON.parse(data); } catch { stop(); return; }
      if (!message || typeof message !== 'object') { stop(); return; }
      lastReceived = Date.now();
      setConnected(true);
      const { id, cancel, url, timeout_ms } = message;
      if (active && cancel === active.id) active.controller.abort();
      if (!id) return;
      if (!Number.isSafeInteger(id) || typeof url !== 'string' || !Number.isInteger(timeout_ms) || timeout_ms < 1 || timeout_ms > 60_000) {
        stop();
        return;
      }
      active?.controller.abort();
      const job = { id, controller: new AbortController() };
      active = job;
      const requestSignal = AbortSignal.any([job.controller.signal, AbortSignal.timeout(timeout_ms)]);
      pending = pending.then(async () => {
        try {
          requestSignal.throwIfAborted();
          send({ id, ...await session.fetch(url, requestSignal) });
        } catch (error) {
          if (!job.controller.signal.aborted) send({ id, error: error instanceof BrowserError ? error.code : 'browser_unavailable' });
        } finally {
          if (active === job) active = undefined;
        }
      });
    });
  });
}
