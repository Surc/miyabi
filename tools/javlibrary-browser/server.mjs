import { createServer } from 'node:http';
import { timingSafeEqual } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { isIP } from 'node:net';
import { BrowserError, BrowserSession, connectBrowser } from './session.mjs';

const local = value => value === '127.0.0.1' || value === '::1' || value === '::ffff:127.0.0.1';
export function createBrowserServer(config, session) {
  const expected = Buffer.from(`Bearer ${config.api_token}`);
  return createServer(async (request, response) => {
    response.setHeader('Cache-Control', 'no-store');
    response.setHeader('X-Content-Type-Options', 'nosniff');
    const send = (status, data) => {
      if (response.destroyed) return;
      response.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8' });
      response.end(JSON.stringify(data));
    };
    const control = request.url === '/' || request.url.startsWith('/control/');
    if (control) {
      if (!local(request.socket.remoteAddress) || !/^(127\.0\.0\.1|localhost):\d+$/.test(request.headers.host || '')) { send(403, { error: 'local_control_only' }); return; }
      if (request.method === 'POST' && (request.headers.origin !== `http://${request.headers.host}` || request.headers['content-type'] !== 'application/json')) { send(403, { error: 'invalid_origin' }); return; }
    } else {
      if (!local(request.socket.remoteAddress) && request.socket.remoteAddress !== config.nas_ip && request.socket.remoteAddress !== `::ffff:${config.nas_ip}`) { send(403, { error: 'client_not_allowed' }); return; }
      const supplied = Buffer.from(request.headers.authorization || '');
      if (request.headers.origin || supplied.length !== expected.length || !timingSafeEqual(supplied, expected)) { send(401, { error: 'unauthorized' }); return; }
    }
    const controller = new AbortController();
    response.on('close', () => { if (!response.writableEnded) controller.abort(); });
    try {
      if (request.method === 'GET' && request.url === '/') {
        const html = await readFile(new URL('./verify.html', import.meta.url));
        response.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Content-Security-Policy': "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'" });
        response.end(html); return;
      }
      if (request.method === 'GET' && request.url === '/control/status') { send(200, session.status()); return; }
      const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(30_000)]);
      if (request.method === 'POST' && request.url === '/control/open') { await session.open(signal); send(200, session.status()); return; }
      if (request.method === 'POST' && request.url === '/control/confirm') { await session.confirm(signal); send(200, session.status()); return; }
      if (request.method !== 'POST' || request.url !== '/v1/page') { send(404, { error: 'not_found' }); return; }
      if (request.headers['content-type'] !== 'application/json') throw new BrowserError('invalid_content_type', 400);
      const chunks = []; let size = 0;
      for await (const chunk of request) {
        size += chunk.length;
        if (size > 4096) throw new BrowserError('request_too_large', 413);
        chunks.push(chunk);
      }
      let body;
      try { body = JSON.parse(Buffer.concat(chunks)); } catch { throw new BrowserError('invalid_json', 400); }
      if (!body || typeof body.url !== 'string' || !Number.isInteger(body.timeout_ms) || body.timeout_ms < 1 || body.timeout_ms > 60_000) throw new BrowserError('invalid_request', 400);
      const page = await session.fetch(body.url, AbortSignal.any([controller.signal, AbortSignal.timeout(body.timeout_ms)]));
      response.setHeader('X-Miyabi-Browser-Page', '1');
      send(200, page);
    } catch (error) {
      send(error instanceof BrowserError ? error.status : 503, { error: error instanceof BrowserError ? error.code : 'browser_unavailable' });
    }
  });
}

export async function start(config) {
  if (!/^[a-f0-9]{64}$/.test(config.api_token) || !Number.isInteger(config.port) || config.port < 1024 || config.port > 65535) throw new Error('Invalid browser service configuration');
  const proxy = new URL(config.proxy_url);
  if (!['http:', 'https:'].includes(proxy.protocol) || proxy.username || proxy.password || proxy.pathname !== '/' || proxy.search || proxy.hash) throw new Error('Invalid browser proxy');
  const session = new BrowserSession(signal => connectBrowser(config, signal));
  const server = createBrowserServer(config, session);
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(config.port, '127.0.0.1', resolve); });
  if (config.lan_host || config.nas_ip) {
    if (isIP(config.lan_host) !== 4 || isIP(config.nas_ip) !== 4 || config.lan_host === '0.0.0.0' || config.nas_ip === '0.0.0.0') {
      server.close();
      throw new Error('Explicit PC and NAS addresses are required for LAN access');
    }
    const lan = createBrowserServer(config, session);
    try {
      await new Promise((resolve, reject) => { lan.once('error', reject); lan.listen(config.port, config.lan_host, resolve); });
    } catch (error) { server.close(); throw error; }
    server.on('close', () => lan.close());
  }
  console.log(`JavLibrary verification: http://127.0.0.1:${config.port}/`);
  return server;
}
