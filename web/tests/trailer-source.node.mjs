import { test } from 'node:test'
import assert from 'node:assert/strict'
import { resolveTrailer, startNativeTrailer } from '../src/features/movie-detail/extensions/trailer-source.ts'

test('only credential-free absolute HTTPS trailers are accepted', () => {
  for(const value of ['','/video.mp4','http://example.com/video.mp4','javascript:alert(1)','https://user:password@example.com/video.mp4']) assert.equal(resolveTrailer(value),null)
  assert.equal(resolveTrailer('https://example.com/video.mp4').hls,false)
})
test('HLS detection uses the pathname and preserves signed query strings', () => {
  const url='https://example.com/PREVIEW.M3U8?token=abc%2Fdef&expires=123'
  assert.deepEqual(resolveTrailer(url),{url,hls:true});assert.equal(resolveTrailer('https://example.com/video.mp4?name=x.m3u8').hls,false)
})
function media(support='',reject=false) {
  return {src:'',calls:[],canPlayType(type){this.calls.push(type);return support},load(){this.calls.push('load')},play(){this.calls.push('play');return reject?Promise.reject(new Error('blocked')):Promise.resolve()},pause(){this.calls.push('pause')},removeAttribute(name){this.calls.push(`remove:${name}`);this.src=''}}
}
test('unsupported HLS never assigns a URL or starts network loading', () => {
  const m=media();assert.equal(startNativeTrailer(m,resolveTrailer('https://example.com/video.m3u8')),null);assert.equal(m.src,'');assert.ok(!m.calls.includes('load'))
})
test('supported native HLS loads the original URL', () => {
  const m=media('maybe');const stop=startNativeTrailer(m,resolveTrailer('https://example.com/video.m3u8?token=1'));assert.equal(m.src,'https://example.com/video.m3u8?token=1');assert.equal(typeof stop,'function')
})
test('ordinary video does not require HLS codec detection', () => {
  const m=media();startNativeTrailer(m,resolveTrailer('https://example.com/video.mp4'));assert.deepEqual(m.calls,['load','play'])
})
test('closing, switching films and privacy unmount can release the media', () => {
  const m=media();const stop=startNativeTrailer(m,resolveTrailer('https://example.com/video.mp4'));stop();assert.equal(m.src,'');assert.deepEqual(m.calls.slice(-3),['pause','remove:src','load'])
})
test('blocked autoplay is contained so native controls remain usable', async () => {
  const m=media('',true);assert.equal(typeof startNativeTrailer(m,resolveTrailer('https://example.com/video.mp4')),'function');await Promise.resolve();assert.notEqual(m.src,'')
})
