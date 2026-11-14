import { act, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as raster from '@/components/design/raster'
import { AiVideoDialog } from '@/components/media/ai-video-dialog'
import { api, type AIJob, type Media } from '@/lib/api'

import { mockApi, renderWithProviders } from './utils'

function VideoDialog({ from }: { from?: Media }) {
  const [open, setOpen] = useState(true)

  return (
    <>
      <button onClick={() => setOpen(true)}>Open video</button>
      <AiVideoDialog open={open} onOpenChange={setOpen} from={from} />
    </>
  )
}

const startedJob = { id: 'aij_cancelled', status: 'queued' } as AIJob

afterEach(() => vi.restoreAllMocks())

describe('AI video cancellation', () => {
  it('does not start a paid job after cancelling picture preparation', async () => {
    mockApi({
      'GET /v1/ai': { configured: true, operations: [], usage: { used_usd: 0, limit_usd: 10 } },
      'GET /v1/media/med_1': {},
    })
    let finish!: (blob: Blob) => void
    const prepare = vi.spyOn(raster, 'downscale').mockImplementation(() => new Promise((resolve) => (finish = resolve)))
    const start = vi.spyOn(api, 'startAiJob').mockResolvedValue(startedJob)

    renderWithProviders(<VideoDialog from={{ id: 'med_1', original_name: 'Photo' } as Media} />)
    const user = userEvent.setup()

    await user.type(screen.getByRole('textbox', { name: 'How should it move?' }), 'Zoom in')
    await user.click(screen.getByRole('button', { name: /Make the video/ }))
    await waitFor(() => expect(prepare).toHaveBeenCalledTimes(1))
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    await user.click(screen.getByRole('button', { name: 'Open video' }))
    await act(async () => finish(new Blob(['image'], { type: 'image/png' })))

    expect(start).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: /Make the video/ })).toBeEnabled()
  })

  it('cancels a known running job when navigating away', async () => {
    const requests = mockApi({
      'GET /v1/ai': { configured: true, operations: [], usage: { used_usd: 0, limit_usd: 10 } },
      'GET /v1/ai/jobs/aij_cancelled': startedJob,
    })

    vi.spyOn(api, 'startAiJob').mockResolvedValue(startedJob)
    const discard = vi.spyOn(api, 'discardAiJob').mockResolvedValue(undefined)
    const view = renderWithProviders(<VideoDialog />)
    const user = userEvent.setup()

    await user.type(screen.getByRole('textbox', { name: 'What should the video show?' }), 'Waves')
    await user.click(screen.getByRole('button', { name: /Make the video/ }))
    await waitFor(() => expect(requests.find('GET', '/v1/ai/jobs/aij_cancelled')).toHaveLength(1))
    view.unmount()

    await waitFor(() => expect(discard).toHaveBeenCalledWith(startedJob.id))
  })

  it('keeps an accepted video when the dialog unmounts', async () => {
    mockApi({
      'GET /v1/ai': { configured: true, operations: [], usage: { used_usd: 0, limit_usd: 10 } },
      'GET /v1/ai/jobs/aij_cancelled': {
        ...startedJob,
        status: 'succeeded',
        result: { mime_type: 'video/mp4', width: 1280, height: 720 },
      },
      'GET /v1/media/counts': {},
    })
    vi.spyOn(api, 'startAiJob').mockResolvedValue(startedJob)
    const accept = vi.spyOn(api, 'acceptAiJob').mockResolvedValue({ id: 'med_1', original_name: 'Waves.mp4' } as Media)
    const discard = vi.spyOn(api, 'discardAiJob').mockResolvedValue(undefined)
    const view = renderWithProviders(<VideoDialog />)
    const user = userEvent.setup()

    await user.type(screen.getByRole('textbox', { name: 'What should the video show?' }), 'Waves')
    await user.click(screen.getByRole('button', { name: /Make the video/ }))
    await user.click(await screen.findByRole('button', { name: 'Keep it' }))
    await waitFor(() => expect(screen.queryByTestId('ai-video-dialog')).not.toBeInTheDocument())
    view.unmount()

    expect(accept).toHaveBeenCalledWith(startedJob.id)
    expect(discard).not.toHaveBeenCalled()
  })

  it.each(['close', 'unmount'] as const)(
    'discards a job returned after %s while its start request was pending',
    async (exit) => {
      mockApi({
        'GET /v1/ai': { configured: true, operations: [], usage: { used_usd: 0, limit_usd: 10 } },
        'GET /v1/ai/jobs/aij_cancelled': startedJob,
      })
      let finish!: (job: AIJob) => void
      const start = vi.spyOn(api, 'startAiJob').mockImplementation(() => new Promise((resolve) => (finish = resolve)))
      const discard = vi.spyOn(api, 'discardAiJob').mockResolvedValue(undefined)
      const view = renderWithProviders(<VideoDialog />)
      const user = userEvent.setup()

      await user.type(screen.getByRole('textbox', { name: 'What should the video show?' }), 'Waves')
      await user.click(screen.getByRole('button', { name: /Make the video/ }))
      await waitFor(() => expect(start).toHaveBeenCalledTimes(1))

      if (exit === 'close') {
        await user.click(screen.getByRole('button', { name: 'Cancel' }))
        await user.click(screen.getByRole('button', { name: 'Open video' }))
      } else {
        view.unmount()
      }

      await act(async () => finish(startedJob))
      await waitFor(() => expect(discard).toHaveBeenCalledWith(startedJob.id))

      expect(screen.queryByText(/Making the video/)).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /Make the video/ })?.hasAttribute('disabled') ?? false).toBe(false)
    },
  )
})
