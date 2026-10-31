import { test } from 'node:test';
import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { BrowserError, BrowserSession, CDP, browserArguments, initialPage, pageURL } from './session.mjs';

test('manual browser launch uses a concrete loopback debugging port', () => {
  const config = { profile_dir: 'dedicated-profile', browser_path: 'edge', proxy_url: 'http://127.0.0.1:7890', port: 18082 };
  const args = browserArguments(config);
  assert.ok(args.includes('--remote-debugging-address=127.0.0.1'));
  assert.ok(args.includes('--remote-debugging-port=18083'));
  assert.ok(args.includes('--user-data-dir=dedicated-profile'));
  for (const invalid of [0, 18082, 80, 65536, '18083']) assert.throws(() => browserArguments({ ...config, debug_port: invalid }));
  assert.ok(browserArguments({ ...config, debug_port: 19083 }).includes('--remote-debugging-port=19083'));
});

test('manual verification can navigate same-origin challenges while automatic page access stays strict', async () => {
  const cdp = new FakeCDP();
  const session = new BrowserSession(async () => cdp);
  const signal = AbortSignal.timeout(1000);
  await session.open(signal);
  for (const url of [initialPage + '&__cf_chl_tk=temporary', 'https://www.javlibrary.com/cdn-cgi/challenge-platform/verify']) {
    cdp.emit('Fetch.requestPaused', { frameId: 'main', requestId: 'manual', request: { url } });
    assert.equal(cdp.commands.at(-1).method, 'Fetch.continueRequest');
    assert.equal(pageURL(url), false);
  }
  cdp.page.url = initialPage + '&__cf_chl_tk=temporary';
  await session.confirm(signal);
  assert.equal(session.status().state, 'ready');
  cdp.noResponse = true;
  const controller = new AbortController();
  const pending = session.fetch(initialPage, controller.signal);
  cdp.emit('Fetch.requestPaused', { frameId: 'main', requestId: 'automatic', request: { url: 'https://www.javlibrary.com/cn/videocomments.php?v=film' } });
  assert.equal(cdp.commands.at(-1).method, 'Fetch.failRequest');
  controller.abort();
  await assert.rejects(pending);
});

class FakeCDP extends EventEmitter {
  constructor() {
    super();
    this.commands = [];
    this.page = { url: initialPage, ready: true, challenge: false, template: true, html: '<div class="videos"></div>' };
  }
  async send(method, params) {
    this.commands.push({ method, params });
    if (method === 'Page.getFrameTree') return { frameTree: { frame: { id: 'main' } } };
    if (method === 'Runtime.evaluate') return { result: { value: { ...this.page } } };
    if (method === 'Page.navigate') {
      this.page.url = params.url;
      if (!this.noResponse) {
        this.emit('Network.requestWillBeSent', { frameId: 'main', type: 'Document', request: { url: params.url } });
        this.emit('Network.responseReceived', { frameId: 'main', type: 'Document', response: { url: params.url, status: this.page.challenge ? 403 : 200 } });
      }
      return { loaderId: 'loader' };
    }
    return {};
  }
  close() { this.emit('disconnected'); }
}

test('manual confirmation gates requests and a renewed challenge does not get navigated away', async () => {
  const cdp = new FakeCDP();
  let connections = 0;
  const session = new BrowserSession(async () => { connections++; return cdp; });
  const signal = AbortSignal.timeout(3000);
  await assert.rejects(session.fetch(initialPage, signal), { code: 'verification_required' });
  await session.open(signal);
  await session.open(signal);
  assert.equal(connections, 1);
  assert.equal(session.status().state, 'verification_required');
  await session.confirm(signal);
  assert.equal((await session.fetch(initialPage, signal)).status, 200);
  cdp.page.challenge = true;
  await assert.rejects(session.fetch(initialPage, signal), { code: 'verification_required' });
  const navigations = cdp.commands.filter(command => command.method === 'Page.navigate').length;
  await assert.rejects(session.fetch(initialPage, signal), { code: 'verification_required' });
  assert.equal(cdp.commands.filter(command => command.method === 'Page.navigate').length, navigations);
  cdp.page.challenge = false;
  await session.confirm(signal);
  assert.equal((await session.fetch(initialPage, signal)).status, 200);
  cdp.close();
  assert.equal(session.status().state, 'closed');
});

test('a canceled browser request stops its own navigation and releases the slot', async () => {
  const cdp = new FakeCDP();
  const session = new BrowserSession(async () => cdp, { pollMS: 1 });
  await session.open(AbortSignal.timeout(1000));
  await session.confirm(AbortSignal.timeout(1000));
  cdp.noResponse = true;
  await assert.rejects(session.fetch(initialPage, AbortSignal.timeout(10)));
  assert.equal(session.busy, false);
  assert.equal(cdp.commands.at(-1).method, 'Page.stopLoading');
  cdp.noResponse = false;
  assert.equal((await session.fetch(initialPage, AbortSignal.timeout(1000))).status, 200);
});

test('only JavLibrary rating pages are navigable, including main-frame redirects', async () => {
  for (const target of ['http://www.javlibrary.com/cn/?v=film', 'https://user:secret@www.javlibrary.com/cn/?v=film', 'https://localhost/cn/?v=film', 'https://www.javlibrary.com/cn/videocomments.php?v=film', initialPage + '&keyword=other']) assert.equal(pageURL(target), false);
  assert.equal(pageURL('https://www.javlibrary.com/cn/?v=film'), true);
  const cdp = new FakeCDP();
  const session = new BrowserSession(async () => cdp);
  await session.open(AbortSignal.timeout(1000));
  cdp.emit('Fetch.requestPaused', { frameId: 'main', requestId: 'blocked', request: { url: 'http://127.0.0.1/private' } });
  assert.equal(cdp.commands.at(-1).method, 'Fetch.failRequest');
  cdp.emit('Fetch.requestPaused', { frameId: 'challenge-iframe', requestId: 'allowed', request: { url: 'https://challenges.cloudflare.com/' } });
  assert.equal(cdp.commands.at(-1).method, 'Fetch.continueRequest');
});

test('CDP correlates replies and rejects pending work when the browser closes', async () => {
  class FakeSocket extends EventTarget {
    readyState = WebSocket.OPEN;
    send(value) { this.message = JSON.parse(value); }
    close() { this.dispatchEvent(new Event('close')); }
  }
  const socket = new FakeSocket();
  const cdp = new CDP(socket);
  const result = cdp.send('Page.enable');
  socket.dispatchEvent(new MessageEvent('message', { data: JSON.stringify({ id: socket.message.id, result: { enabled: true } }) }));
  assert.deepEqual(await result, { enabled: true });
  const pending = cdp.send('Page.getFrameTree');
  socket.close();
  await assert.rejects(pending, { code: 'browser_closed' });
  assert.equal(cdp.pending.size, 0);
});
