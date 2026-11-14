import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { decode, keepDecoded, mix } from '@/components/timeline/audio'
import { newClip, newSound, newTimeline, type Timeline } from '@/components/timeline/doc'
import TimelineEditor from '@/components/timeline/editor'

import { type Call, mockApi, renderWithProviders } from './utils'

// WebCodecs and Mediabunny do not run in jsdom: the export is a stand-in that answers a small file.
vi.mock('@/components/timeline/export', () => ({
  canExportVideo: async () => true,
  exportVideo: async () => new Blob(['mp4'], { type: 'video/mp4' }),
  exportAudio: async () => new Blob(['m4a'], { type: 'audio/mp4' }),
}))

const project = { id: 'prj_1', name: 'Jingle', source_ids: ['med_a'], output_id: 'med_out' }
const saved = (id: string) => ({ id, original_name: `${id}.m4a`, mime_type: 'audio/mp4', size_bytes: 3 })
const sound = (id: string) => newSound({ id, src: `/api/v1/media/${id}/file`, name: `${id}.mp3`, duration: 4 })

/** A saved sound timeline with one clip. */
function jingle(): Timeline {
  return { ...newTimeline('audio'), name: 'Jingle', sounds: [sound('med_a')] }
}

describe('the video and sound editor', () => {
  it('a project that fails to load shows the error with Try again, and cannot be saved until it has opened', async () => {
    let down = true
    const api = mockApi({
      'GET /v1/media-projects/prj_1': project,
      'GET /v1/media-projects/prj_1/file': () =>
        down ? { status: 503, body: { error: { code: 'unavailable', message: 'Try again' } } } : jingle(),
      'POST /v1/media/med_out/replace': saved('med_new'),
      'PUT /v1/media-projects/prj_1': project,
      'GET /v1/*': { data: [] },
    })
    const user = userEvent.setup()

    renderWithProviders(<TimelineEditor kind="audio" search="?project=prj_1" />, { mode: 'local' })

    expect(await screen.findByRole('alert')).toHaveTextContent('Could not open it')
    expect(screen.getByTestId('timeline-save')).toBeDisabled()

    down = false
    await user.click(screen.getByRole('button', { name: 'Try again' }))
    await screen.findByText('1 sound clips')
    await user.click(screen.getByTestId('timeline-save'))

    // What is saved is the timeline that loaded, into its own project and over its own file.
    await waitFor(() => expect(api.find('PUT', '/v1/media-projects/prj_1')).toHaveLength(1))

    const file = (api.find('PUT', '/v1/media-projects/prj_1')[0]!.body as FormData).get('file') as File

    expect(JSON.parse(await file.text())).toMatchObject({ name: 'Jingle', sounds: [{ mediaId: 'med_a' }] })
  })

  it('a project that is not a timeline stays closed', async () => {
    mockApi({
      'GET /v1/media-projects/prj_1': project,
      'GET /v1/media-projects/prj_1/file': { version: 1, width: 1080, height: 1080, pages: [] },
      'GET /v1/*': { data: [] },
    })

    renderWithProviders(<TimelineEditor kind="audio" search="?project=prj_1" />, { mode: 'local' })

    expect(await screen.findByRole('alert')).toHaveTextContent('not a video or a sound')
    expect(screen.getByTestId('timeline-save')).toBeDisabled()
  })

  it('saving again after the project failed to save replaces the file it already made', async () => {
    let fail = true
    const api = mockApi({
      'GET /v1/media-projects/prj_1': project,
      'GET /v1/media-projects/prj_1/file': jingle(),
      // Replacing a file answers a new one (and the old one is gone).
      'POST /v1/media/med_out/replace': saved('med_new'),
      'POST /v1/media/med_new/replace': saved('med_newer'),
      'PUT /v1/media-projects/prj_1': () => (fail ? { status: 500, body: {} } : project),
      'GET /v1/*': { data: [] },
    })
    const user = userEvent.setup()

    renderWithProviders(<TimelineEditor kind="audio" search="?project=prj_1" />, { mode: 'local' })
    await screen.findByText('1 sound clips')
    await user.click(screen.getByTestId('timeline-save'))
    await screen.findByText('Could not save')

    fail = false
    await user.click(screen.getByTestId('timeline-save'))
    await waitFor(() => expect(api.find('PUT', '/v1/media-projects/prj_1')).toHaveLength(2))

    // Not the file that is gone: that would be saved as a second copy.
    expect(api.calls.filter((c: Call) => c.path.endsWith('/replace')).map((c) => c.path.split('/')[3])).toEqual([
      'med_out',
      'med_new',
    ])
  })
})

describe("the timeline's sound", () => {
  /** jsdom has no Web Audio: decoding answers a buffer unless the bytes say "silent", mixing answers one too. */
  class FakeContext {
    destination = {}
    decodeAudioData = async (b: ArrayBuffer) => {
      if (new TextDecoder().decode(b).includes('silent')) {
        throw new Error('no sound in this file')
      }

      return { duration: 4 }
    }
    createBufferSource = () => ({ connect: (n: unknown) => n, start: () => undefined })
    createGain = () => ({
      connect: (n: unknown) => n,
      gain: { setValueAtTime: () => undefined, linearRampToValueAtTime: () => undefined },
    })
    startRendering = async () => ({ mixed: true })
  }

  /** Files by address: a status for one that fails, or its bytes. */
  function files(by: Record<string, number | string>) {
    const fetched: string[] = []

    vi.stubGlobal('OfflineAudioContext', FakeContext)
    vi.stubGlobal('fetch', async (src: string) => {
      const f = by[src]

      fetched.push(src)

      return typeof f === 'number' ? new Response('gone', { status: f }) : new Response(f)
    })

    return fetched
  }

  afterEach(() => keepDecoded(new Set()))

  it('a sound that cannot be fetched fails the export, and the next try fetches it again', async () => {
    const t = { ...newTimeline('audio'), sounds: [sound('med_a')] }
    const src = t.sounds[0]!.src
    const by: Record<string, number | string> = { [src]: 503 }
    const fetched = files(by)

    await expect(mix(t)).rejects.toThrow('Could not read the sound of med_a.mp3')

    by[src] = 'music'
    await expect(mix(t)).resolves.toEqual({ mixed: true })
    expect(fetched).toEqual([src, src])
  })

  it('a sound clip whose file holds no sound fails the export; a silent video does not', async () => {
    const silent = { ...newTimeline('audio'), sounds: [sound('med_a')] }

    files({ [silent.sounds[0]!.src]: 'silent' })
    await expect(mix(silent)).rejects.toThrow('Could not read the sound of med_a.mp3')

    const clip = newClip({ id: 'med_v', kind: 'video', src: '/v.mp4', name: 'v.mp4', width: 1080, height: 1920 })

    files({ '/v.mp4': 'silent' })
    await expect(mix({ ...newTimeline('video'), clips: [clip] })).resolves.toEqual({ mixed: true })

    // A video that did not arrive is another matter.
    keepDecoded(new Set())
    files({ '/v.mp4': 404 })
    await expect(mix({ ...newTimeline('video'), clips: [clip] })).rejects.toThrow('Could not read the sound of v.mp4')
  })

  it('decoded sound is kept only while the timeline uses it', async () => {
    const fetched = files({ '/a.mp3': 'music', '/b.mp3': 'music' })

    await decode('/a.mp3')
    await decode('/b.mp3')
    await decode('/a.mp3')
    expect(fetched).toEqual(['/a.mp3', '/b.mp3'])

    // b left the timeline: its buffer is let go, and decoding it again starts over.
    keepDecoded(new Set(['/a.mp3']))
    await decode('/a.mp3')
    await decode('/b.mp3')
    expect(fetched).toEqual(['/a.mp3', '/b.mp3', '/b.mp3'])
  })
})
