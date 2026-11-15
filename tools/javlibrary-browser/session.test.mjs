import { test } from 'node:test';
import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { createServer } from 'node:http';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { BrowserError, BrowserSession, CDP, browserArguments, connectBrowser, initialPage, pageURL } from './session.mjs';

test('dedicated browser starts without a window on a concrete loopback debugging port', () => {
  const config = { profile_dir: 'dedicated-profile', browser_path: 'edge', proxy_url: 'http://127.0.0.1:7890', port: 18082 };
  const args = browserArguments(config);
  assert.ok(args.includes('--remote-debugging-address=127.0.0.1'));
  assert.ok(args.includes('--remote-debugging-port=18083'));
  assert.ok(args.includes('--user-data-dir=dedicated-profile'));
  assert.ok(args.includes('--no-startup-window'));
  assert.ok(!args.includes('--new-window'));
  assert.ok(!args.includes(initialPage));
  for (const invalid of [0, 18082, 80, 65536, '18083']) assert.throws(() => browserArguments({ ...config, debug_port: invalid }));
  assert.ok(browserArguments({ ...config, debug_port: 19083 }).includes('--remote-debugging-port=19083'));
});

test('manual verification can navigate same-origin challenges while automatic page access stays strict', async () => {
  const cdp = new FakeCDP();
  const rating = new FakeCDP();
  const session = new BrowserSession(async (signal, { background }) => background ? rating : cdp);
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
  rating.noResponse = true;
  const controller = new AbortController();
  const pending = session.fetch(initialPage, controller.signal);
  rating.emit('Fetch.requestPaused', { frameId: 'main', requestId: 'automatic', request: { url: 'https://www.javlibrary.com/cn/videocomments.php?v=film' } });
  assert.equal(rating.commands.at(-1).method, 'Fetch.failRequest');
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

test('ratings use the hidden page after the visible window closes and stop when verification expires', async () => {
  const connections = [];
  const session = new BrowserSession(async (signal, options) => {
    const cdp = new FakeCDP();
    if (options.background) cdp.targetId = 'hidden';
    connections.push({ ...options, cdp });
    return cdp;
  });
  const signal = AbortSignal.timeout(3000);
  await assert.rejects(session.fetch(initialPage, signal), { code: 'verification_required' });
  await session.open(signal);
  await session.open(signal);
  assert.equal(connections.length, 1);
  assert.equal(session.status().state, 'verification_required');
  await session.confirm(signal);
  const manual = connections[0].cdp;
  const rating = connections[1].cdp;
  manual.close();
  assert.equal(session.status().state, 'ready');
  await session.open(signal);
  assert.equal((await session.fetch(initialPage, signal)).status, 200);
  assert.deepEqual(connections.map(connection => connection.background), [false, true]);
  assert.equal(manual.commands.some(command => command.method === 'Page.navigate'), false);
  assert.equal(rating.commands.some(command => command.method === 'Page.bringToFront'), false);

  rating.page.challenge = true;
  await assert.rejects(session.fetch(initialPage, signal), { code: 'verification_required' });
  const navigations = rating.commands.filter(command => command.method === 'Page.navigate').length;
  await assert.rejects(session.fetch(initialPage, signal), { code: 'verification_required' });
  assert.equal(rating.commands.filter(command => command.method === 'Page.navigate').length, navigations);
  assert.equal(connections.length, 2);
  await session.open(signal);
  assert.equal(connections[2].background, false);
  assert.equal(connections[2].exclude, 'hidden');
  assert.equal(connections[2].cdp.commands.at(-1).method, 'Page.bringToFront');
  await session.confirm(signal);
  assert.equal((await session.fetch(initialPage, signal)).status, 200);
  connections[3].cdp.close();
  await assert.rejects(session.fetch(initialPage, signal), { code: 'verification_required' });
  assert.deepEqual(connections.map(connection => connection.background), [false, true, false, true]);
});

test('confirmation attaches to a valid film in another tab of the dedicated browser', async t => {
  const signal = AbortSignal.timeout(3000);
  const profile = await mkdtemp(join(tmpdir(), 'miyabi-session-'));
  t.after(() => rm(profile, { recursive: true, force: true }));
  const filmURL = 'https://www.javlibrary.com/cn/javme4ivte.html';
  const browserPath = '/devtools/browser/dedicated';
  const checked = [];
  let hiddenSupported = false;
  let owner;
  const server = createServer((request, response) => {
    const origin = `ws://127.0.0.1:${server.address().port}`;
    response.setHeader('Content-Type', 'application/json');
    if (request.url === '/json/version') {
      response.end(JSON.stringify({ webSocketDebuggerUrl: origin + browserPath }));
    } else if (request.url === '/json/protocol') {
      const parameters = hiddenSupported ? [{ name: 'hidden' }] : [];
      response.end(JSON.stringify({ domains: [{ domain: 'Target', commands: [{ name: 'createTarget', parameters }] }] }));
    } else {
      response.end(JSON.stringify([
        { type: 'page', url: initialPage, webSocketDebuggerUrl: origin + '/challenge' },
        { type: 'page', url: filmURL, webSocketDebuggerUrl: origin + '/film' },
        { type: 'page', url: 'https://example.test/', webSocketDebuggerUrl: origin + '/other' },
      ]));
    }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise(resolve => server.close(resolve)));
  const debugPort = server.address().port;
  const config = { profile_dir: profile, port: debugPort === 18082 ? 18084 : 18082, debug_port: debugPort };
  await writeFile(join(profile, 'MiyabiDevTools.json'), JSON.stringify({ port: config.debug_port, browserPath, keepAlive: true }));
  t.mock.method(CDP, 'connect', async address => {
    const cdp = new FakeCDP();
    const path = new URL(address).pathname;
    checked.push(path);
    cdp.page.challenge = path === '/challenge';
    if (path === '/film') cdp.page.url = filmURL;
    if (path === browserPath) {
      owner = cdp;
      t.mock.method(owner, 'send', async (method, params) => {
        assert.equal(method, 'Target.createTarget');
        assert.deepEqual(params, { url: 'about:blank', hidden: true, background: true });
        return { targetId: 'hidden-rating' };
      });
    }
    return cdp;
  });

  const session = new BrowserSession((signal, options) => connectBrowser(config, signal, options));
  await session.open(signal);
  await assert.rejects(session.confirm(signal), { code: 'hidden_page_unsupported' });
  assert.equal(session.status().state, 'verification_required');
  assert.equal(session.cdp.page.url, filmURL);
  hiddenSupported = true;
  await session.confirm(signal);
  assert.equal(session.status().state, 'ready');
  assert.deepEqual(checked, ['/challenge', '/challenge', '/film', browserPath, '/devtools/page/hidden-rating']);

  owner.close();
  assert.equal(session.status().state, 'closed');
});

test('a canceled browser request stops its own navigation and releases the slot', async () => {
  const session = new BrowserSession(async () => new FakeCDP(), { pollMS: 1 });
  await session.open(AbortSignal.timeout(1000));
  await session.confirm(AbortSignal.timeout(1000));
  const cdp = session.cdp;
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
