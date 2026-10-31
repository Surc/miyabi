import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createBrowserServer } from './server.mjs';
import { BrowserError, initialPage } from './session.mjs';

test('HTTP bridge protects browser control and page access without exposing credentials', async t => {
  const token = 'b'.repeat(64);
  let verified = false;
  const session = {
    status: () => ({ state: verified ? 'ready' : 'verification_required' }),
    open: async () => {},
    confirm: async () => { verified = true; },
    fetch: async url => {
      if (!verified) throw new BrowserError('verification_required', 409);
      return { url, status: 200, html: '<div class="videos"></div>', redirects: [] };
    },
  };
  const server = createBrowserServer({ api_token: token }, session);
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => new Promise(resolve => server.close(resolve)));
  const base = `http://127.0.0.1:${server.address().port}`;
  const page = headers => fetch(base + '/v1/page', { method: 'POST', headers: { 'Content-Type': 'application/json', ...headers }, body: JSON.stringify({ url: initialPage, timeout_ms: 1000 }) });
  assert.equal((await page({})).status, 401);
  assert.equal((await page({ Authorization: `Bearer ${token}`, Origin: 'https://www.javlibrary.com' })).status, 401);
  assert.equal((await page({ Authorization: `Bearer ${token}` })).status, 409);
  const foreign = await fetch(base + '/control/confirm', { method: 'POST', headers: { Origin: 'https://www.javlibrary.com', 'Content-Type': 'application/json' }, body: '{}' });
  assert.equal(foreign.status, 403);
  assert.equal(verified, false);
  const confirm = await fetch(base + '/control/confirm', { method: 'POST', headers: { Origin: base, 'Content-Type': 'application/json' }, body: '{}' });
  assert.equal(confirm.status, 200);
  const success = await page({ Authorization: `Bearer ${token}` });
  assert.equal(success.status, 200);
  assert.equal(success.headers.get('X-Miyabi-Browser-Page'), '1');
  assert.equal((await success.json()).url, initialPage);
  const ui = await (await fetch(base)).text();
  assert.ok(ui.includes('验证完成'));
  assert.ok(!ui.includes(token));
});
