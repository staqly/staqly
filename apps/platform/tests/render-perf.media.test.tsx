import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { MediaPage } from '@/components/media/media-page'
import type { Media, MediaLibraryItem } from '@/lib/api'

import { renders } from './render-count'
import { mockApi, renderWithProviders } from './utils'

// The upload itself (an XMLHttpRequest with progress events) is driven by the test.
const upload = vi.hoisted(() => ({
  progress: (_share: number) => {},
  done: (_m: Media) => {},
}))

vi.mock('@/lib/api', async (original) => ({
  ...(await original<typeof import('@/lib/api')>()),
  uploadMediaWithProgress: (_file: File, onProgress: (share: number) => void) =>
    new Promise<Media>((resolve) => {
      upload.progress = onProgress
      upload.done = resolve
    }),
}))

function item(i: number): MediaLibraryItem {
  return {
    id: `med_${i}`,
    mime_type: 'image/png',
    size_bytes: 2048,
    original_name: `picture-${i}.png`,
    url: `http://localhost/v1/media/med_${i}`,
    sha256: `sha_${i}`,
    // Newest first, like the server's default order.
    created_at: new Date(Date.UTC(2026, 9, 20, 10, 0) - i * 60_000).toISOString(),
    alt_text: '',
    width: 1080,
    height: 1080,
    duration_ms: 0,
    folder_id: '',
    tags: [],
    uses: 0,
  }
}

const ITEMS = Array.from({ length: 60 }, (_, i) => item(i))
const FOLDERS = { data: [], root_files: 60, total_files: 60, deleted_files: 0, duplicate_files: 0, tags: [] }

async function renderLibrary() {
  const api = mockApi({
    'GET /v1/media': { data: ITEMS },
    'GET /v1/media-folders': FOLDERS,
    'GET /v1/workspace/usage': { plan: { limits: {} } },
  })

  renderWithProviders(<MediaPage />, { mode: 'local' })
  await waitFor(() => expect(screen.getAllByTestId('media-tile')).toHaveLength(60))
  // Let the queries settle (folders, usage) before counting.
  await act(async () => {
    await new Promise((r) => setTimeout(r, 50))
  })

  return api
}

const frame = () =>
  act(async () => {
    await new Promise((r) => requestAnimationFrame(() => r(undefined)))
  })

describe('render budgets: the media library (60 files)', () => {
  it('selecting a file renders that tile alone', async () => {
    await renderLibrary()
    renders.reset()
    fireEvent.click(screen.getByRole('button', { name: 'Select picture-5.png' }))
    expect(await screen.findByText('1 selected')).toBeInTheDocument()
    expect(renders.of('MediaTile')).toBeLessThanOrEqual(2)
    expect(renders.of('MediaPreview')).toBe(0)

    renders.reset()
    fireEvent.click(screen.getByRole('button', { name: 'Select picture-9.png' }))
    expect(await screen.findByText('2 selected')).toBeInTheDocument()
    expect(renders.of('MediaTile')).toBeLessThanOrEqual(2)
  })

  it('upload progress renders the upload card, not the grid; the new file joins the list without a reload', async () => {
    const api = await renderLibrary()
    const lists = () => api.calls.filter((c) => c.method === 'GET' && c.path === '/v1/media').length
    const before = lists()

    fireEvent.change(screen.getByTestId('media-upload-input'), {
      target: { files: [new File([new Uint8Array(4)], 'new.png', { type: 'image/png' })] },
    })
    expect(await screen.findByText('Uploading 1 file')).toBeInTheDocument()
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0))
    })

    renders.reset()

    for (let i = 1; i <= 10; i++) {
      act(() => upload.progress(i / 11))
      await frame()
    }

    expect(screen.getByRole('progressbar', { name: 'Uploading new.png' })).toHaveAttribute('aria-valuenow', '91')
    expect(renders.of('UploadCard')).toBeGreaterThan(0)
    expect(renders.of('MediaGrid')).toBe(0)
    expect(renders.of('MediaPreview')).toBe(0)
    expect(renders.of('MediaTile')).toBe(0)

    // Several progress events in one frame are one update.
    renders.reset()
    act(() => {
      upload.progress(0.95)
      upload.progress(0.96)
      upload.progress(0.97)
    })
    await frame()
    expect(renders.of('UploadCard')).toBe(1)

    renders.reset()
    await act(async () => {
      upload.done({ ...item(99), id: 'med_new', original_name: 'new.png', created_at: '2026-10-21T08:00:00Z' })
      await new Promise((r) => setTimeout(r, 0))
    })
    await waitFor(() => expect(screen.getAllByTestId('media-tile')).toHaveLength(61))
    expect(screen.getAllByTestId('media-tile')[0]).toHaveAccessibleName('new.png')
    // Only the new tile renders; the other 60 keep their identity.
    expect(renders.of('MediaTile')).toBeLessThanOrEqual(2)
    // No list reload: the answer went into the cache. The folder counts reload once.
    expect(lists()).toBe(before)
    await waitFor(() => expect(api.find('GET', '/v1/media-folders')).toHaveLength(2))
  })
})
