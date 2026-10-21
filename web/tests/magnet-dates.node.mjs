import { test } from 'node:test'
import assert from 'node:assert/strict'
import { magnetPublication, sortMagnets } from '../src/features/movie-detail/extensions/magnet-dates.ts'

test('date-only labels never shift with the local timezone', () => {
  assert.deepEqual(magnetPublication('2026-10-21'), { day:'2026-10-21',timestamp:Date.UTC(2026,9,21) })
})
test('source offsets are respected but the displayed calendar date stays original', () => {
  const p=magnetPublication('2026-10-21T00:30:00+08:00')
  assert.equal(p.day,'2026-10-21');assert.equal(p.timestamp,Date.parse('2026-10-20T16:30:00Z'))
})
test('naive timestamps have an explicit UTC interpretation', () => {
  assert.equal(magnetPublication('2026-10-21 12:34:56').timestamp,Date.parse('2026-10-21T12:34:56Z'))
})
test('invalid dates, rollover times and ambiguous formats stay unknown', () => {
  for(const raw of ['','unknown','2026-02-29','2026-04-31','10/21/2026','2026-10-21T24:00:00Z','2026-10-21T12:60:00Z','2026-10-21T12:00:00+24:00']) assert.equal(magnetPublication(raw),null,raw)
  assert.ok(magnetPublication('2024-02-29'))
})
const rows=Object.freeze([{id:'unknown',created_at:''},{id:'new',created_at:'2026-10-21'},{id:'old',created_at:'2026-01-01'},{id:'tie',created_at:'2026-10-21'}].map(Object.freeze))
test('newest sorts a copy with unknown last and stable equal dates', () => {
  assert.deepEqual(sortMagnets(rows,'newest').map(x=>x.id),['new','tie','old','unknown']);assert.equal(rows[0].id,'unknown')
})
test('oldest still puts unknown last and preserves ties', () => {
  assert.deepEqual(sortMagnets(rows,'oldest').map(x=>x.id),['old','new','tie','unknown'])
})
test('recommended preserves upstream order without returning its mutable array', () => {
  assert.deepEqual(sortMagnets(rows,'recommended'),rows);assert.notEqual(sortMagnets(rows,'recommended'),rows)
})
