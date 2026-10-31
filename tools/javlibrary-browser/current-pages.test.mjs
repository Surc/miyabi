import { test } from 'node:test';
import assert from 'node:assert/strict';
import { pageURL } from './session.mjs';

test('canonical film pages retain same-origin and identity restrictions', () => {
  for (const path of ['javme4ivte.html', 'javme33a5a.html', 'javme4ivte.html?v=javme4ivte']) {
    assert.equal(pageURL(`https://www.javlibrary.com/cn/${path}`), true, path);
  }
  for (const path of [
    'javme4ivte.html?v=other',
    'javme4ivte.html?v=javme4ivte&v=javme4ivte',
    'javme4ivte.html?unused=value',
    'javme4ivte.html?unused=%zz',
    'redirect.html',
    'javme4ivte.html/extra',
  ]) {
    assert.equal(pageURL(`https://www.javlibrary.com/cn/${path}`), false, path);
  }
  assert.equal(pageURL('https://evil.example/cn/javme4ivte.html'), false);
  assert.equal(pageURL('http://www.javlibrary.com/cn/javme4ivte.html'), false);
  assert.equal(pageURL('https://www.javlibrary.com/ja/javme4ivte.html'), false);
});
