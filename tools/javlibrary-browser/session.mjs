import { EventEmitter, once } from 'node:events';
import { access, mkdir, readFile, writeFile } from 'node:fs/promises';
import { spawn } from 'node:child_process';
import { createServer } from 'node:net';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';

export const initialPage = 'https://www.javlibrary.com/cn/vl_searchbyid.php?keyword=ABP-123';
const maxHTML = 2 * 1024 * 1024;

export function pageURL(value) {
  let url;
  try { url = new URL(value); } catch { return false; }
  if (url.origin !== 'https://www.javlibrary.com' || url.username || url.password || url.hash) return false;
  const entries = [...url.searchParams];
  const film = /^\/cn\/(jav[A-Za-z0-9_-]{1,77})\.html$/.exec(url.pathname);
  if (film) return entries.length === 0 || (entries.length === 1 && entries[0][0] === 'v' && entries[0][1] === film[1]);
  if (entries.length !== 1) return false;
  const [key, item] = entries[0];
  if (url.pathname === '/cn/vl_searchbyid.php') return key === 'keyword' && item.length > 0 && item.length <= 200;
  return url.pathname === '/cn/' && key === 'v' && /^[A-Za-z0-9_-]{1,80}$/.test(item);
}

function manualPageURL(value) {
  try {
    const url = new URL(value);
    return url.origin === 'https://www.javlibrary.com' && !url.username && !url.password;
  } catch { return false; }
}

function browserPort(config) {
  const port = config.debug_port ?? 18083;
  if (!Number.isInteger(port) || port < 1024 || port > 65535 || port === config.port) throw new Error('Invalid browser debug port');
  return port;
}

export function browserArguments(config, { minimized = false } = {}) {
  return [
    `--user-data-dir=${config.profile_dir}`, '--remote-debugging-address=127.0.0.1', `--remote-debugging-port=${browserPort(config)}`,
    '--no-first-run', '--no-default-browser-check', `--proxy-server=${config.proxy_url}`, '--new-window',
    ...(minimized ? ['--start-minimized', 'about:blank'] : [initialPage]),
  ];
}

export class BrowserError extends Error {
  constructor(code, status = 502) { super(code); this.code = code; this.status = status; }
}

export class CDP extends EventEmitter {
  constructor(socket) {
    super();
    this.socket = socket;
    this.nextID = 0;
    this.pending = new Map();
    socket.addEventListener('message', ({ data }) => {
      let message;
      try { message = JSON.parse(data); } catch { return; }
      if (!message.id) { this.emit(message.method, message.params); return; }
      const pending = this.pending.get(message.id);
      if (!pending) return;
      this.pending.delete(message.id);
      pending.cleanup();
      if (message.error) pending.reject(new BrowserError('browser_command_failed'));
      else pending.resolve(message.result);
    });
    socket.addEventListener('close', () => {
      for (const pending of this.pending.values()) {
        pending.cleanup();
        pending.reject(new BrowserError('browser_closed', 503));
      }
      this.pending.clear();
      this.emit('disconnected');
    });
    socket.addEventListener('error', () => {});
  }
  static async connect(address, signal) {
    const socket = new WebSocket(address);
    const client = new CDP(socket);
    try { await once(socket, 'open', { signal }); }
    catch { socket.close(); throw new BrowserError('browser_unavailable', 503); }
    return client;
  }
  send(method, params = {}, signal = AbortSignal.timeout(10_000)) {
    return new Promise((resolve, reject) => {
      if (signal.aborted || this.socket.readyState !== WebSocket.OPEN) { reject(new BrowserError('browser_unavailable', 503)); return; }
      const id = ++this.nextID;
      const abort = () => { this.pending.delete(id); reject(signal.reason); };
      this.pending.set(id, { resolve, reject, cleanup: () => signal.removeEventListener('abort', abort) });
      signal.addEventListener('abort', abort, { once: true });
      try { this.socket.send(JSON.stringify({ id, method, params })); }
      catch { this.pending.delete(id); signal.removeEventListener('abort', abort); reject(new BrowserError('browser_unavailable', 503)); }
    });
  }
  close() { this.socket.close(); }
}

// A concrete loopback port avoids Chromium's port-0 automation mode. Reconnect
// only to the browser endpoint recorded after launching our dedicated profile.
export async function connectBrowser(config, signal, { minimized = false, verify = false } = {}) {
  await mkdir(config.profile_dir, { recursive: true, mode: 0o700 });
  const port = browserPort(config);
  const recordPath = join(config.profile_dir, 'MiyabiDevTools.json');
  const discover = async expectedPath => {
    const base = `http://127.0.0.1:${port}`;
    const getJSON = async (path, method = 'GET') => {
      const requestSignal = AbortSignal.any([signal, AbortSignal.timeout(1000)]);
      return (await fetch(base + path, { method, signal: requestSignal })).json();
    };
    const version = await getJSON('/json/version');
    const endpoint = new URL(version.webSocketDebuggerUrl);
    if (endpoint.protocol !== 'ws:' || endpoint.hostname !== '127.0.0.1' || endpoint.port !== String(port) || !/^\/devtools\/browser\/[A-Za-z0-9-]+$/.test(endpoint.pathname) || (expectedPath && endpoint.pathname !== expectedPath)) throw new Error('browser identity mismatch');
    const pages = await getJSON('/json/list');
    if (verify) {
      const client = await verifiedTab(pages, port, signal);
      return { client, browserPath: endpoint.pathname };
    }
    let page = pages.find(item => item.type === 'page' && (manualPageURL(item.url) || (minimized && item.url === 'about:blank')));
    if (!page) {
      const target = minimized ? 'about:blank' : initialPage;
      page = await getJSON(`/json/new?${encodeURIComponent(target)}`, 'PUT');
    }
    return { client: await connectTab(page, port, signal), browserPath: endpoint.pathname };
  };
  try {
    const record = JSON.parse(await readFile(recordPath, 'utf8'));
    if (record.port !== port || typeof record.browserPath !== 'string' || !record.browserPath.startsWith('/devtools/browser/')) throw new Error('invalid browser record');
    return (await discover(record.browserPath)).client;
  } catch {
    signal.throwIfAborted();
    if (verify) throw new BrowserError('verification_required', 409);
  }
  await access(config.browser_path);
  const probe = createServer();
  await new Promise((resolve, reject) => {
    probe.once('error', () => reject(new BrowserError('debug_port_in_use', 503)));
    probe.listen(port, '127.0.0.1', () => probe.close(resolve));
  });
  signal.throwIfAborted();
  const child = spawn(config.browser_path, browserArguments(config, { minimized }), { detached: true, stdio: 'ignore', windowsHide: false });
  child.on('error', () => {});
  child.unref();
  while (!signal.aborted) {
    let connection;
    try {
      connection = await discover();
      await writeFile(recordPath, JSON.stringify({ port, browserPath: connection.browserPath }), { mode: 0o600 });
      return connection.client;
    } catch {
      connection?.client.close();
      signal.throwIfAborted();
    }
    await delay(200, undefined, { signal });
  }
  throw new BrowserError('browser_unavailable', 503);
}

const snapshotExpression = `(() => {
  const html = document.documentElement?.outerHTML || '';
  return {url:location.href, ready:document.readyState !== 'loading',
    oversized:html.length > ${maxHTML}, html:html.length <= ${maxHTML} ? html : '',
    challenge:html.includes('_cf_chl_opt') || !!document.querySelector('#challenge-form,#cf-challenge-running'),
    template:!!document.querySelector('#video_id,.videos')};
})()`;

async function readPage(cdp, signal) {
  const result = await cdp.send('Runtime.evaluate', { expression: snapshotExpression, returnByValue: true }, signal);
  if (!result.result?.value || result.exceptionDetails) throw new BrowserError('invalid_browser_page');
  return result.result.value;
}

function isVerifiedPage(page) {
  return manualPageURL(page.url) && page.ready && page.template && !page.challenge && !page.oversized;
}

function connectTab(page, port, signal) {
  const socket = new URL(page.webSocketDebuggerUrl);
  if (socket.hostname !== '127.0.0.1' || socket.port !== String(port) || socket.protocol !== 'ws:') throw new Error('invalid page endpoint');
  return CDP.connect(socket.href, signal);
}

async function verifiedTab(pages, port, signal) {
  for (const page of pages) {
    if (page.type !== 'page' || !manualPageURL(page.url)) continue;
    let client;
    let accepted = false;
    try {
      const probe = AbortSignal.any([signal, AbortSignal.timeout(1500)]);
      client = await connectTab(page, port, probe);
      accepted = isVerifiedPage(await readPage(client, probe));
      if (accepted) return client;
    } catch {
      signal.throwIfAborted();
    } finally {
      if (!accepted) client?.close();
    }
  }
  throw new BrowserError('verification_required', 409);
}

export class BrowserSession {
  constructor(connect, { pollMS = 150 } = {}) {
    this.connect = connect;
    this.pollMS = pollMS;
    this.cdp = null;
    this.verified = false;
    this.busy = false;
    this.navigation = null;
  }
  status() {
    if (this.busy) return { state: 'loading' };
    if (this.verified) return { state: this.cdp ? 'ready' : 'standby' };
    return { state: this.cdp ? 'verification_required' : 'closed' };
  }

  async open(signal) {
    if (this.busy) throw new BrowserError('busy', 503);
    this.busy = true;
    try {
      const cdp = await this.attach(signal);
      await cdp.send('Page.bringToFront', {}, signal);
    } finally {
      this.busy = false;
    }
  }

  async attach(signal, { minimized = false, verify = false } = {}) {
    if (this.cdp && !verify) return this.cdp;
    const cdp = await this.connect(signal, { minimized, verify });
    if (this.cdp === cdp) return cdp;
    this.cdp?.close();
    this.cdp = cdp;
    cdp.on('disconnected', () => { if (this.cdp === cdp) this.cdp = null; });
    try {
      await cdp.send('Page.enable', {}, signal);
      const tree = await cdp.send('Page.getFrameTree', {}, signal);
      const frameID = tree.frameTree.frame.id;
      cdp.on('Network.requestWillBeSent', event => {
        if (this.cdp !== cdp || !this.navigation || event.frameId !== frameID || event.type !== 'Document') return;
        this.navigation.url = event.request.url;
        if (event.redirectResponse) this.navigation.redirects.push(event.request.url);
        if (!pageURL(event.request.url) || this.navigation.redirects.length > 2) this.navigation.invalid = true;
      });
      cdp.on('Network.responseReceived', event => {
        if (this.cdp !== cdp || !this.navigation || event.frameId !== frameID || event.type !== 'Document') return;
        this.navigation.status = event.response.status;
        this.navigation.url = event.response.url;
      });
      cdp.on('Fetch.requestPaused', event => {
        const allowed = this.navigation ? pageURL(event.request.url) : manualPageURL(event.request.url);
        const blocked = event.frameId === frameID && !allowed;
        if (blocked && this.navigation) this.navigation.invalid = true;
        void cdp.send(blocked ? 'Fetch.failRequest' : 'Fetch.continueRequest', {
          requestId: event.requestId, ...(blocked ? { errorReason: 'BlockedByClient' } : {}),
        }).catch(() => {});
      });
      await cdp.send('Network.enable', {}, signal);
      await cdp.send('Fetch.enable', { patterns: [{ resourceType: 'Document', requestStage: 'Request' }] }, signal);
      return cdp;
    } catch (error) {
      cdp.close();
      this.cdp = null;
      throw error;
    }
  }
  async confirm(signal) {
    if (this.busy) throw new BrowserError('busy', 503);
    this.busy = true;
    try {
      let page;
      if (this.cdp) {
        try { page = await readPage(this.cdp, signal); }
        catch { signal.throwIfAborted(); }
      }
      if (!page || !isVerifiedPage(page)) {
        const cdp = await this.attach(signal, { verify: true });
        if (!isVerifiedPage(await readPage(cdp, signal))) throw new BrowserError('verification_required', 409);
      }
      this.verified = true;
    } catch (error) {
      if (error instanceof BrowserError && error.code === 'verification_required') this.verified = false;
      throw error;
    } finally {
      this.busy = false;
    }
  }
  async fetch(url, signal) {
    if (!pageURL(url)) throw new BrowserError('invalid_url', 400);
    if (!this.verified) throw new BrowserError('verification_required', 409);
    if (this.busy) throw new BrowserError('busy', 503);
    this.busy = true;
    try {
      const cdp = await this.attach(signal, { minimized: true });
      this.navigation = { url: '', status: 0, redirects: [], invalid: false };
      const navigation = await cdp.send('Page.navigate', { url }, signal);
      if (navigation.errorText) throw new BrowserError('navigation_failed');
      for (;;) {
        signal.throwIfAborted();
        if (this.navigation.invalid) throw new BrowserError('invalid_redirect');
        let page;
        try { page = await readPage(cdp, signal); }
        catch (error) { signal.throwIfAborted(); if (!this.cdp) throw error; }
        if (page && this.navigation.status && page.url === this.navigation.url && page.ready) {
          if (page.challenge) { this.verified = false; throw new BrowserError('verification_required', 409); }
          if (this.navigation.status !== 200 || !page.template || page.oversized || Buffer.byteLength(page.html) > maxHTML) throw new BrowserError('invalid_browser_page');
          return { url: page.url, status: 200, html: page.html, redirects: this.navigation.redirects };
        }
        await delay(this.pollMS, undefined, { signal });
      }
    } finally {
      // Never leave a canceled navigation racing with the next request.
      if (signal.aborted && this.navigation) await this.cdp?.send('Page.stopLoading').catch(() => {});
      this.navigation = null;
      this.busy = false;
    }
  }
}
