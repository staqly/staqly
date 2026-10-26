import { describe, expect, it } from 'vitest'

import { wav } from '@/components/timeline/audio'
import {
  clipAt,
  clipStarts,
  clock,
  gainAt,
  length,
  newClip,
  newSound,
  newTimeline,
  parseTimeline,
  soundLanes,
  splitClip,
  splitSound,
  transitionLength,
} from '@/components/timeline/doc'

const pic = (id: string) => newClip({ id, kind: 'image', src: `/m/${id}`, name: id, width: 1000, height: 1000 })

describe('the video and sound timeline', () => {
  it('plays clips back to back and knows which one is on screen', () => {
    const t = newTimeline('video')

    t.clips = [pic('a'), { ...pic('b'), duration: 2 }, pic('c')]

    expect(clipStarts(t)).toEqual([0, 3, 5])
    expect(length(t)).toBe(8)
    expect(clipAt(t, 4)).toMatchObject({ index: 1, local: 1 })
    expect(clipAt(t, 0)?.index).toBe(0)
    // A transition never takes more than half of either clip; the first clip and a cut have none.
    expect(transitionLength(t, 0)).toBe(0)
    expect(transitionLength(t, 1)).toBe(0.5)
    t.clips[2]!.transition = 'cut'
    expect(transitionLength(t, 2)).toBe(0)
  })

  it('ends with whatever plays last, music included', () => {
    const t = newTimeline('video')

    t.clips = [pic('a')]
    t.sounds = [newSound({ id: 's', src: '/s', name: 's', duration: 10 }, 1)]

    expect(length(t)).toBe(11)
  })

  it('fades a sound in and out around its volume', () => {
    const s = { ...newSound({ id: 's', src: '/s', name: 's', duration: 10 }), volume: 0.8, fadeIn: 2, fadeOut: 4 }

    expect(gainAt(s, 0)).toBe(0)
    expect(gainAt(s, 1)).toBeCloseTo(0.4)
    expect(gainAt(s, 5)).toBeCloseTo(0.8)
    expect(gainAt(s, 8)).toBeCloseTo(0.4)
  })

  it('splits sounds and video clips at the playhead, keeping where they are in their files', () => {
    const s = { ...newSound({ id: 's', src: '/s', name: 's', duration: 10 }, 2), in: 1 }
    const [a, b] = splitSound(s, 5)!

    expect([a.start, a.in, a.duration]).toEqual([2, 1, 3])
    expect([b.start, b.in, b.duration]).toEqual([5, 4, 7])
    expect(splitSound(s, 2)).toBeNull()

    const v = {
      ...newClip({ id: 'v', kind: 'video', src: '/v', name: 'v', width: 1920, height: 1080, duration: 8 }),
      in: 2,
    }
    const [c, d] = splitClip(v, 3)!

    expect([c.in, c.duration, d.in, d.duration, d.transition]).toEqual([2, 3, 5, 5, 'cut'])
  })

  it('puts overlapping sounds on separate lanes', () => {
    const a = newSound({ id: 'a', src: '/a', name: 'a', duration: 5 }, 0)
    const b = newSound({ id: 'b', src: '/b', name: 'b', duration: 5 }, 3)
    const c = newSound({ id: 'c', src: '/c', name: 'c', duration: 2 }, 6)
    const { lane, count } = soundLanes([a, b, c])

    expect(count).toBe(2)
    expect([lane.get(a.id), lane.get(b.id), lane.get(c.id)]).toEqual([0, 1, 0])
  })

  it('opens its own files only', () => {
    const t = newTimeline('audio')

    t.sounds = [newSound({ id: 's', src: '/s', name: 's', duration: 3 })]

    expect(parseTimeline(JSON.stringify(t))?.sounds).toHaveLength(1)
    expect(parseTimeline(JSON.stringify({ version: 1, width: 10, height: 10, pages: [] }))).toBeNull()
    expect(parseTimeline('not json')).toBeNull()
    expect(clock(62.25)).toBe('1:02.3')
  })

  it('writes a WAV file the size of its samples', async () => {
    const buf = { numberOfChannels: 2, length: 100, sampleRate: 48000, getChannelData: () => new Float32Array(100) }
    const file = wav(buf as unknown as AudioBuffer)
    const head = new TextDecoder().decode(new Uint8Array(await file.arrayBuffer()).slice(0, 12))

    expect(file.size).toBe(44 + 100 * 2 * 2)
    expect(head.startsWith('RIFF') && head.endsWith('WAVE')).toBe(true)
  })
})

describe('the beat of a sound', () => {
  it('finds the tempo and the first beat of clicks at 120 a minute', async () => {
    const { beats } = await import('@/components/timeline/audio')
    const rate = 8000
    const s = new Float32Array(rate * 12)

    // A short click every half second, from 0.25 s on.
    for (let t = 0.25; t < 12; t += 0.5) {
      for (let j = 0; j < 200; j++) {
        s[Math.round(t * rate) + j] = Math.sin(j / 3) * 0.8
      }
    }

    const b = beats(s, rate)

    expect(b.bpm).toBeGreaterThan(117)
    expect(b.bpm).toBeLessThan(123)
    expect(Math.abs(((b.first - 0.25 + b.period / 2) % b.period) - b.period / 2)).toBeLessThan(0.07)
  })
})

describe('captions from speech', () => {
  it('splits a long sentence into short captions that share its time', async () => {
    const { splitSpoken } = await import('@/components/timeline/speech')
    const parts = splitSpoken([{ text: 'one two three four five six seven eight nine ten', start: 0, end: 10 }], 4)

    expect(parts.map((p) => p.text)).toEqual(['one two three four', 'five six seven eight', 'nine ten'])
    expect(parts.map((p) => [p.start, p.end])).toEqual([
      [0, 4],
      [4, 8],
      [8, 10],
    ])
  })
})
