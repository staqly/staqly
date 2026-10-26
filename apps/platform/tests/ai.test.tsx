import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AiPanel, discardAiRun, expandPlacement } from '@/components/design/ai-panel'
import { newDesign, newImage, type Page } from '@/components/design/doc'
import type { PanelCtx } from '@/components/design/panels'
import type { DesignStore } from '@/components/design/store'
import { allowanceNote, madeBy } from '@/lib/ai'
import type { AIJob, AIOperation, AIStatus } from '@/lib/api'

import { mockApi, renderWithProviders } from './utils'

function op(id: AIOperation['id'], over: Partial<AIOperation> = {}): AIOperation {
  const image = id !== 'generate' && id !== 'text_to_video'

  return {
    id,
    kind: id.endsWith('video') ? 'video' : 'image',
    prompt: 'required',
    image,
    mask: id === 'erase' || id === 'inpaint',
    provider: 'fal',
    model: 'fal-ai/x',
    fallback: '',
    ...over,
  }
}

function status(ops: AIOperation[], used = 0): AIStatus {
  return {
    configured: ops.length > 0,
    providers: ops.length ? ['fal'] : [],
    operations: ops,
    usage: { period: '2026-10', used_usd: used, limit_usd: 10 },
  }
}

function job(over: Partial<AIJob> = {}): AIJob {
  return {
    id: 'aij_1',
    operation: 'generate',
    kind: 'image',
    status: 'queued',
    provider: 'fal',
    model: 'fal-ai/flux/schnell',
    fallback_from: '',
    error: null,
    result: null,
    media_id: '',
    estimated_cost_usd: 0.006,
    duration_ms: 0,
    created_at: '2026-10-26T10:00:00Z',
    finished_at: '',
    ...over,
  }
}

/** A panel context over a 1080 x 1350 page; add and updatePage are spies (nothing changes the page by itself). */
function ctxWith(page: Page, selected: string[] = []) {
  const design = newDesign(1080, 1350)

  design.pages[0] = page

  const add = vi.fn<PanelCtx['add']>()
  const updatePage = vi.fn<PanelCtx['updatePage']>()
  const ctx = {
    store: { design } as unknown as DesignStore,
    page,
    updatePage,
    selected,
    onSelect: vi.fn<PanelCtx['onSelect']>(),
    add,
  } as PanelCtx

  return { ctx, add, updatePage }
}

afterEach(() => {
  discardAiRun()
  vi.unstubAllGlobals()
})

describe('the AI panel', () => {
  it('says AI is off when the server offers no picture tools', async () => {
    mockApi({ 'GET /v1/ai': status([op('text_to_video')]) })
    const { ctx } = ctxWith(newDesign().pages[0]!)

    renderWithProviders(<AiPanel ctx={ctx} />)

    expect(await screen.findByText('AI tools are not available on this server.')).toBeInTheDocument()
  })

  it('offers only what the server offers, the photo tools only with a photo selected', async () => {
    mockApi({ 'GET /v1/ai': status([op('generate'), op('edit'), op('erase', { prompt: 'none' }), op('upscale')]) })
    const d = newDesign(1080, 1350)
    const pic = newImage(d, '/api/v1/media/med_1', 800, 600, { mediaId: 'med_1' })
    const page = { ...d.pages[0]!, elements: [pic] }

    const { ctx } = ctxWith(page)
    const first = renderWithProviders(<AiPanel ctx={ctx} />)

    expect(await screen.findByRole('radio', { name: 'New picture' })).toBeChecked()
    expect(screen.queryByRole('radio', { name: 'Remove an object' })).not.toBeInTheDocument()
    expect(screen.getByText('Select a photo on the page to change it with AI.')).toBeInTheDocument()
    first.unmount()

    const { ctx: withPhoto } = ctxWith(page, [pic.id])

    renderWithProviders(<AiPanel ctx={withPhoto} />)

    expect(await screen.findByRole('radio', { name: 'Change it' })).toBeChecked()
    await userEvent.click(screen.getByRole('radio', { name: 'Remove an object' }))
    // Painting comes first; it has no prompt.
    expect(screen.getByRole('button', { name: 'Paint the area' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Remove it' })).toBeDisabled()
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  })

  it('runs a request, previews the result and changes the page only when it is kept', async () => {
    let polls = 0
    const api = mockApi({
      'GET /v1/ai': status([op('generate')]),
      'POST /v1/ai/jobs': { status: 202, body: job() },
      'GET /v1/ai/jobs/aij_1': () => {
        polls++

        return polls < 2
          ? job({ status: 'running' })
          : job({
              status: 'succeeded',
              provider: 'higgsfield',
              fallback_from: 'fal',
              result: { mime_type: 'image/png', width: 1024, height: 1280 },
            })
      },
      'POST /v1/ai/jobs/aij_1/accept': {
        status: 201,
        body: { id: 'med_ai', width: 1024, height: 1280, mime_type: 'image/png', original_name: 'AI generate.png' },
      },
    })
    const { ctx, add, updatePage } = ctxWith(newDesign(1080, 1350).pages[0]!)

    renderWithProviders(<AiPanel ctx={ctx} />)
    await userEvent.type(await screen.findByRole('textbox'), 'A lighthouse at dawn')
    await userEvent.click(screen.getByRole('button', { name: 'Make the picture' }))

    const sent = api.find('POST', '/v1/ai/jobs')[0]!
    const form = sent.body as FormData

    expect(form.get('operation')).toBe('generate')
    expect(form.get('prompt')).toBe('A lighthouse at dawn')
    // The page's shape (4:5), about 1.3 megapixels.
    expect([form.get('width'), form.get('height')]).toEqual(['1075', '1344'])
    expect(sent.headers['Idempotency-Key']).toMatch(/^[0-9a-f]{32}$/)

    expect(await screen.findByRole('img', { name: 'What AI made' }, { timeout: 5000 })).toHaveAttribute(
      'src',
      '/api/v1/ai/jobs/aij_1/result',
    )
    expect(screen.getByText('Made by Higgsfield; fal.ai was not available.')).toBeInTheDocument()
    expect(add).not.toHaveBeenCalled()
    expect(updatePage).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Use it' }))
    await waitFor(() => expect(add).toHaveBeenCalledTimes(1))
    expect(add.mock.calls[0]![0]).toMatchObject({ type: 'image', mediaId: 'med_ai', src: '/api/v1/media/med_ai' })
    expect(api.find('DELETE', '/v1/ai/jobs/aij_1')).toHaveLength(0)
  })

  it('discards a running request: the job is cancelled and the page stays as it was', async () => {
    const api = mockApi({
      'GET /v1/ai': status([op('generate')]),
      'POST /v1/ai/jobs': { status: 202, body: job() },
      'GET /v1/ai/jobs/aij_1': job({ status: 'running' }),
      'DELETE /v1/ai/jobs/aij_1': { status: 204 },
    })
    const { ctx, add, updatePage } = ctxWith(newDesign().pages[0]!)

    renderWithProviders(<AiPanel ctx={ctx} />)
    await userEvent.type(await screen.findByRole('textbox'), 'A cat')
    await userEvent.click(screen.getByRole('button', { name: 'Make the picture' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Cancel' }))

    await waitFor(() => expect(api.find('DELETE', '/v1/ai/jobs/aij_1')).toHaveLength(1))
    expect(await screen.findByRole('button', { name: 'Make the picture' })).toBeInTheDocument()
    expect(add).not.toHaveBeenCalled()
    expect(updatePage).not.toHaveBeenCalled()
  })

  it('shows why a request failed and offers to try again', async () => {
    mockApi({
      'GET /v1/ai': status([op('generate')]),
      'POST /v1/ai/jobs': { status: 202, body: job() },
      'GET /v1/ai/jobs/aij_1': job({
        status: 'failed',
        error: { code: 'ai_refused', message: 'fal.ai refused the request: prompt was flagged' },
      }),
    })
    const { ctx } = ctxWith(newDesign().pages[0]!)

    renderWithProviders(<AiPanel ctx={ctx} />)
    await userEvent.type(await screen.findByRole('textbox'), 'Something')
    await userEvent.click(screen.getByRole('button', { name: 'Make the picture' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('prompt was flagged')
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })
})

describe('AI helpers', () => {
  it('grows a picture to the page shape, centred, within 4096 px', () => {
    expect(expandPlacement(1000, 1000, { width: 1920, height: 1080 })).toEqual({
      x: 389,
      y: 0,
      w: 1000,
      h: 1000,
      cw: 1778,
      ch: 1000,
    })
    expect(expandPlacement(1000, 1250, { width: 1080, height: 1350 })).toEqual({
      x: 250,
      y: 313,
      w: 1000,
      h: 1250,
      cw: 1500,
      ch: 1875,
    })
    const big = expandPlacement(4000, 1000, { width: 1, height: 1 })

    expect(Math.max(big.cw, big.ch)).toBeLessThanOrEqual(4096)
    expect(big.cw).toBe(big.ch)
  })

  it('names who made a result and the allowance once half is used', () => {
    expect(madeBy({ provider: 'fal', fallback_from: '' } as AIJob)).toBe('Made by fal.ai.')
    expect(allowanceNote(status([op('generate')], 2))).toBe('')
    expect(allowanceNote(status([op('generate')], 7.5))).toBe(
      "This workspace has used 75% of this month's AI allowance.",
    )
  })
})
