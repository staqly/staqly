// The logic behind /tools, checked with plain Node (it strips the TypeScript types): `node scripts/check-tools.mjs`.
import assert from 'node:assert/strict'

import { bars, closest, crop, fits, ratioLabel } from '../lib/aspect-ratio.ts'
import { scheduleSuggestion, scoreGrid, shiftGrid, PLATFORMS } from '../lib/best-time.ts'
import { applyLinks, refang } from '../lib/links.ts'
import { check, nextRuns, parseList, parseRule, skippedTimes, zoned } from '../lib/schedule-validator.ts'

// Links: defang, remove, leave emails and file names alone, trailing punctuation stays outside.
assert.deepEqual(applyLinks('defang', 'Read https://www.example.com/a.b?q=1, then example.org.'), {
  text: 'Read www[.]example[.]com/a.b?q=1, then example[.]org.',
  found: ['https://www.example.com/a.b?q=1', 'example.org'],
})
assert.equal(applyLinks('remove', 'New post https://example.com/x today').text, 'New post today')
assert.equal(applyLinks('defang', 'mail ana@example.com, see notes.txt').text, 'mail ana@example.com, see notes.txt')
assert.equal(applyLinks('defang', '(see example.com)').text, '(see example[.]com)')
assert.equal(applyLinks('defang', 'example[.]com').text, 'example[.]com') // already plain text
assert.equal(refang('hxxps://www[.]example[.]com/a and example(.)org'), 'https://www.example.com/a and example.org')

// Best time: the grid moves with the zone difference and wraps across the week.
const grid = scoreGrid(PLATFORMS.find((p) => p.id === 'linkedin'))
const shifted = shiftGrid(grid, 120)

assert.equal(shifted[1][10], grid[1][8]) // Tuesday 08:00 there is 10:00 here, two hours ahead
assert.equal(shiftGrid(grid, -600)[6][22], grid[0][8]) // Monday 08:00 there is Sunday 22:00 ten hours behind
assert.deepEqual(scheduleSuggestion(grid), { weekdays: [2, 3, 4], times: ['08:00', '12:00'] })

// Schedule validator: the rule is read as the server reads it, runs follow the rule's own wall clock.
assert.deepEqual(parseRule('{"times":["18:00","09:00","09:00"],"weekdays":[5,1],"tz":"Europe/Warsaw"}').rule, {
  times: ['09:00', '18:00'],
  weekdays: [1, 5],
  tz: 'Europe/Warsaw',
})
assert.equal(parseRule('{"times":["9:00"]}').errors.length, 1) // HH:MM
assert.equal(parseRule('{"times":["09:00"],"tz":"Mars/Base"}').errors.length, 1)
assert.equal(parseRule('{"every_minutes":3}').errors.length, 1) // 5 to 10080
assert.deepEqual(parseRule('{"times":["09:00"]}').rule, { times: ['09:00'], tz: 'UTC' }) // no zone: UTC
assert.equal(parseRule('nope').errors.length, 1)

const from = new Date('2026-10-19T12:00:00Z') // a Monday
const runs = nextRuns({ times: ['09:00'], weekdays: [1, 3], tz: 'Europe/Warsaw' }, from, 3)

assert.deepEqual(
  runs.map((r) => r.toISOString()),
  ['2026-10-21T07:00:00.000Z', '2026-10-26T08:00:00.000Z', '2026-10-28T08:00:00.000Z'], // 09:00 stays 09:00 after the clocks go back
)
assert.equal(nextRuns({ every_minutes: 90 }, from, 2)[1].toISOString(), '2026-10-19T15:00:00.000Z')

// A time the clocks skip moves on by the jump, like the server: 02:30 on 29 March 2026 in Warsaw is 03:30.
const skipped = zoned(2026, 3, 29, 2, 30, 'Europe/Warsaw')

assert.equal(skipped.shifted, true)
assert.equal(skipped.at.toISOString(), '2026-03-29T01:30:00.000Z')
assert.deepEqual(skippedTimes({ times: ['02:30', '09:00'], tz: 'Europe/Warsaw' }, from), ['02:30 on 28 March']) // 2027: the last Sunday of March

// A list in the audience's zone; an explicit offset wins.
assert.deepEqual(
  parseList('2026-11-03 18:00\n2026-11-02T10:00:00Z', 'America/New_York').runs.map((r) => r.toISOString()),
  ['2026-11-02T10:00:00.000Z', '2026-11-03T23:00:00.000Z'],
)

// Checks: too busy for LinkedIn, fine for X; 03:00 posts are flagged for a New York audience.
const busy = nextRuns({ times: ['09:00', '10:00', '11:00'], tz: 'America/New_York' }, from, 21)
const result = check(busy, { platforms: ['linkedin', 'x'], audienceTz: 'America/New_York', ruleTz: 'America/New_York' })

assert.equal(result.find((c) => c.title.startsWith('LinkedIn')).level, 'warn')
assert.equal(result.find((c) => c.title.startsWith('X')).level, 'ok')
const night = check(nextRuns({ times: ['03:00'], tz: 'America/New_York' }, from, 7), {
  platforms: [],
  audienceTz: 'America/New_York',
})

assert.ok(night.some((c) => c.level === 'warn' && c.title.includes('sleeps')))
// Warsaw and New York change clocks a week apart: the posts move for the audience.
const drift = check(nextRuns({ times: ['15:00'], tz: 'Europe/Warsaw' }, from, 7), {
  platforms: [],
  audienceTz: 'America/New_York',
  ruleTz: 'Europe/Warsaw',
})

assert.ok(drift.some((c) => c.title.includes('move')))

// Aspect ratio: reduced labels, odd sizes named after the common ratio they're near, crop and bars, closest shape.
assert.equal(ratioLabel(1080, 1350), '4:5')
assert.equal(ratioLabel(1920, 1080), '16:9')
assert.equal(ratioLabel(1080, 1920), '9:16')
assert.equal(ratioLabel(1921, 1080), 'about 16:9')
assert.equal(ratioLabel(1000, 523), 'about 1.91:1')
assert.ok(fits(1080 / 1350, 4 / 5) && !fits(1, 4 / 5))
assert.deepEqual(crop(1, 1), { share: 0, from: 'none' })

const wide = crop(16 / 9, 1) // a landscape picture in a square: the sides go

assert.equal(wide.from, 'sides')
assert.ok(Math.abs(wide.share - 0.4375) < 1e-9)

const tall = crop(9 / 16, 4 / 5) // a vertical video in a 4:5 frame: top and bottom go

assert.equal(tall.from, 'top and bottom')
assert.ok(Math.abs(tall.share - (1 - 9 / 16 / (4 / 5))) < 1e-9)

assert.equal(bars(16 / 9, 1).where, 'top and bottom')
assert.ok(Math.abs(bars(16 / 9, 1).share - 0.4375) < 1e-9)
assert.deepEqual(closest(1080 / 1300, 1080), { shape: { w: 4, h: 5 }, width: 1080, height: 1350 })
assert.deepEqual(closest(2 / 1, 1000).shape, { w: 1.91, h: 1 })
assert.deepEqual(closest(1 / 2, 900).shape, { w: 9, h: 16 })

console.log('tools: ok')
