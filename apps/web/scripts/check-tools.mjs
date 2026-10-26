// The logic behind /tools, checked with plain Node (it strips the TypeScript types): `node scripts/check-tools.mjs`.
import assert from 'node:assert/strict'

import { bars, closest, crop, fits, ratioLabel } from '../lib/aspect-ratio.ts'
import { scheduleSuggestion, scoreGrid, shiftGrid, PLATFORMS } from '../lib/best-time.ts'
import { applyLinks, refang } from '../lib/links.ts'
import { check, nextRuns, parseList, parseRule, skippedTimes, zoned } from '../lib/schedule-validator.ts'
import {
  carouselPieces,
  coverRect,
  cropShare,
  gridPieces,
  outSize,
  pieceName,
  suggestSlides,
} from '../lib/split-image.ts'
import {
  applyStyle,
  foldPreview,
  hasStyle,
  toggleList,
  toggleRange,
  toPlain,
  visibleLength,
} from '../lib/text-format.ts'
import { checkTitle, cutTitle, titleLength } from '../lib/youtube-title.ts'
import { crc32, zip } from '../lib/zip.ts'

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

// Image splitting: the crop keeps the shape and the focus moves it; pieces cover the crop in posting order.
assert.deepEqual(coverRect(2000, 1000, 1), { x: 500, y: 0, w: 1000, h: 1000 })
assert.deepEqual(coverRect(2000, 1000, 1, 0), { x: 0, y: 0, w: 1000, h: 1000 })
assert.deepEqual(coverRect(1000, 2000, 1, 0.5, 1), { x: 0, y: 1000, w: 1000, h: 1000 })

const grid3 = gridPieces(3000, 4000, 1, 3 / 4) // one row of three 3:4 posts from a 3:4 picture: the sides go

assert.equal(grid3.pieces.length, 3)
assert.ok(Math.abs(grid3.crop.w / grid3.crop.h - 9 / 4) < 1e-9)
assert.deepEqual(grid3.pieces[1].src, grid3.pieces[1].view) // a 3:4 post is all on show
assert.deepEqual(
  grid3.pieces.map((p) => p.order),
  [3, 2, 1], // the right-hand post goes first, so the left one ends up top left
)

// Classic squares, with the profile showing squares: nine posts side by side, nothing overlaps.
const grid9 = gridPieces(3000, 3000, 3, 1, 0.5, 0.5, 1)

assert.deepEqual(grid9.pieces[0].src, { x: 0, y: 0, w: 1000, h: 1000 })
assert.deepEqual(grid9.pieces[8].src, { x: 2000, y: 2000, w: 1000, h: 1000 })
assert.equal(grid9.pieces[8].order, 1)
assert.equal(cropShare(3000, 3000, grid9.crop), 0)

// 4:5 posts on today's 3:4 profile: 3:4 cells side by side, each post reaching 1/40 of a row into its neighbours.
const grid45 = gridPieces(5000, 2000, 1, 4 / 5)
const [left, mid] = grid45.pieces

assert.ok(Math.abs(grid45.crop.w - (3 * 0.75 + 0.05) * 2000) < 1e-6)
assert.ok(Math.abs(mid.view.x - (left.view.x + left.view.w)) < 1e-9) // the shown parts join up
assert.ok(Math.abs(left.view.x - left.src.x - 50) < 1e-9) // 0.025 of 2000 trimmed each side
assert.ok(Math.abs(left.src.w / left.src.h - 4 / 5) < 1e-9)
assert.ok(Math.abs(left.view.w / left.view.h - 3 / 4) < 1e-9)

const slides = carouselPieces(5000, 2000, 3, 4 / 5)

assert.equal(slides.pieces.length, 3)
assert.deepEqual(
  slides.pieces.map((p) => p.order),
  [1, 2, 3],
)
assert.ok(Math.abs(slides.pieces[1].src.x - (slides.crop.x + 1600)) < 1e-9) // 3 slides of 1600 × 2000
assert.equal(suggestSlides(5000, 2000, 4 / 5), 3)
assert.equal(suggestSlides(1000, 2000, 1), 2) // at least two
assert.equal(suggestSlides(40000, 1000, 1), 20) // at most twenty
assert.deepEqual(outSize(4 / 5), { width: 1080, height: 1350 })
assert.deepEqual(outSize(3 / 4), { width: 1080, height: 1440 })
assert.equal(pieceName('grid', 3), 'grid-03.jpg')

// ZIP: the standard CRC-32 and a well-formed stored archive.
const enc = new TextEncoder()

assert.equal(crc32(enc.encode('123456789')), 0xcbf43926)

const archive = zip([
  { name: 'a.txt', data: enc.encode('hello') },
  { name: 'b.txt', data: enc.encode('') },
])
const view = new DataView(archive.buffer)

assert.equal(view.getUint32(0, true), 0x04034b50)
assert.equal(view.getUint32(archive.length - 22, true), 0x06054b50)
assert.equal(view.getUint16(archive.length - 12, true), 2) // two entries
assert.equal(view.getUint32(14, true), crc32(enc.encode('hello')))
assert.equal(archive.length, 30 + 5 + 5 + 30 + 5 + (46 + 5) * 2 + 22)

// Text styles: look-alike letters both ways, marks, toggling a selection, lists, the fold.
assert.equal(applyStyle('Hi 2', 'bold'), '𝗛𝗶 𝟮')
assert.equal(applyStyle('Hi 2', 'italic'), '𝘏𝘪 2') // no italic digits
assert.equal(applyStyle('h', 'serifItalic'), 'ℎ') // the letter Unicode had already
assert.equal(applyStyle('𝗛𝗶', 'mono'), '𝙷𝚒') // one letter style replaces another
assert.equal(applyStyle('a b', 'underline'), 'a̲ b̲')
assert.equal(applyStyle(applyStyle('ab', 'strike'), 'strike'), 'a̶b̶') // no double marks
assert.equal(toPlain('𝗛𝗶 𝘛𝘩𝘦𝘳𝘦 𝟮 ℎ a̲'), 'Hi There 2 h a')
assert.ok(hasStyle('𝗛𝗶!', 'bold') && !hasStyle('𝗛i', 'bold') && !hasStyle('!', 'bold'))
assert.ok(hasStyle('a̶̲ b̲', 'underline') && !hasStyle('a̲ b', 'underline'))

const toggled = toggleRange('Say hello now', 4, 9, 'bold')

assert.equal(toggled.text, 'Say 𝗵𝗲𝗹𝗹𝗼 now')
assert.deepEqual([toggled.start, toggled.end], [4, 14]) // two UTF-16 units a letter
assert.equal(toggleRange(toggled.text, 4, 14, 'bold').text, 'Say hello now') // again: off
assert.equal(toggleRange('ab', 1, 1, 'bold').text, '𝗮𝗯') // nothing selected: all of it
assert.equal(visibleLength('𝗮̲b'), 2)

assert.equal(toggleList('One\n\nTwo', 'bullet'), '• One\n\n• Two')
assert.equal(toggleList('• One\n• Two', 'bullet'), 'One\nTwo')
assert.equal(toggleList('• One\n• Two', 'number'), '1. One\n2. Two')

assert.deepEqual(foldPreview('Short', 210, 3), { shown: 'Short', cut: false })
assert.deepEqual(foldPreview('a\nb\nc\nd', 210, 3), { shown: 'a\nb\nc', cut: true })
assert.deepEqual(foldPreview('one two three', 9, 3), { shown: 'one two', cut: true })

// YouTube titles: length tiers, YouTube's refusals, habits.
assert.equal(checkTitle('')[0].level, 'error')
assert.equal(checkTitle('How I learned to bake bread in a week')[0].level, 'ok')
assert.equal(checkTitle('x'.repeat(80))[0].level, 'warn')
assert.equal(checkTitle('x'.repeat(101))[0].level, 'error')
assert.ok(checkTitle('Best <script> tips').some((c) => c.title === 'Angle brackets'))
assert.ok(checkTitle('THE BEST BREAD RECIPE EVER').some((c) => c.title === 'Mostly capitals'))
assert.ok(checkTitle('Bread!!!').some((c) => c.title === 'Repeated punctuation'))
assert.ok(checkTitle('Bread 🍞🍞🍞').some((c) => c.title === '3 emoji'))
assert.ok(checkTitle('Bread #baking').some((c) => c.title === 'A hashtag in the title'))
assert.ok(checkTitle(' Bread').some((c) => c.title === 'Extra spaces'))
assert.equal(titleLength('🍞 bread'), 7)
assert.equal(cutTitle('How I learned to bake bread', 100), 'How I learned to bake bread')
assert.equal(cutTitle('How I learned to bake bread', 15), 'How I learned...')

console.log('tools: ok')
