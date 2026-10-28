import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useEffect } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { placeSaved } from '@/components/design/embed'
import { Composer } from '@/components/posts/composer'
import type { Media } from '@/lib/api'

import { collection } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

// ProseMirror does not type in jsdom (see composer.test.tsx).
vi.mock('@/components/posts/post-editor', () => ({
  PostEditor: (p: {
    id: string
    value: string
    onChange: (t: string) => void
    testId?: string
    below?: React.ReactNode
    start?: React.ReactNode
  }) => (
    <>
      <textarea id={p.id} value={p.value} onChange={(e) => p.onChange(e.target.value)} data-testid={p.testId} />
      {p.below}
      {p.start}
    </>
  ),
}))

// Konva does not draw in jsdom: the canvas is a stand-in, and saving draws a one-pixel file.
vi.mock('@/components/design/canvas', () => ({
  preloadPage: async () => undefined,
  DesignCanvas: () => <div data-testid="design-canvas" />,
  DesignView: ({ stageRef }: { stageRef?: { current: unknown } }) => {
    useEffect(() => {
      if (stageRef) {
        stageRef.current = { toBlob: async () => new Blob(['png'], { type: 'image/png' }) }
      }
    })

    return null
  },
}))

const image = (id: string, name = `${id}.png`) =>
  ({ id, original_name: name, mime_type: 'image/png', width: 1080, height: 1080, size_bytes: 10 }) as Media

function setup(extra: Record<string, unknown> = {}) {
  let n = 0

  return mockApi({
    'GET /v1/collections': { data: [collection()] },
    'GET /v1/channels': { data: [] },
    'GET /v1/providers': { data: [] },
    'GET /v1/media-projects': { data: [] },
    'GET /v1/media': { data: [] },
    'GET /v1/media/*': (c: { path: string }) => image(c.path.split('/')[3]!),
    'POST /v1/media': () => image(`med_saved_${++n}`, `Design ${n}.png`),
    'POST /v1/media-projects': { id: 'prj_1', name: 'Design' },
    ...extra,
  })
}

// By test id: a question over it hides it from the accessibility tree, not from the page.
const drawer = () => screen.queryByTestId('design-drawer')

afterEach(() => vi.restoreAllMocks())

describe('the design editor in a drawer over the post', () => {
  it('Design opens it in the dashboard, and Save and use in the post adds the picture and closes it', async () => {
    const api = setup()
    const open = vi.spyOn(window, 'open')
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    const design = await screen.findByTestId('design-new')

    expect(design.tagName).toBe('BUTTON')
    await user.click(design)
    const editor = await screen.findByRole('dialog', { name: 'Design' })

    expect(open).not.toHaveBeenCalled()
    await user.click(await within(editor).findByRole('button', { name: /Save and use in the post/ }))
    await waitFor(() => expect(drawer()).not.toBeInTheDocument())
    expect(api.find('POST', '/v1/media')).toHaveLength(1)
    expect(await screen.findByRole('img', { name: 'Design 1.png' })).toBeInTheDocument()
    await waitFor(() => expect(design).toHaveFocus())
  })

  it('Edit on a picture replaces it in place', async () => {
    setup({ 'POST /v1/media': () => image('med_edited', 'photo (edited).png') })
    const user = userEvent.setup()

    renderWithProviders(
      <Composer open onOpenChange={() => {}} collectionId="col_1" defaultMedia={[image('med_a'), image('med_b')]} />,
      { mode: 'local' },
    )
    await user.click(await screen.findByRole('button', { name: 'Edit med_a.png' }))
    const editor = await screen.findByRole('dialog', { name: 'Design' })

    await user.click(await within(editor).findByRole('button', { name: /Save and use in the post/ }))
    await waitFor(() => expect(drawer()).not.toBeInTheDocument())
    const names = screen.getAllByRole('img').map((i) => i.getAttribute('alt'))

    expect(names).toEqual(['photo (edited).png', 'med_b.png'])
  })

  it('asks before closing with unsaved changes, and Escape asks the same', async () => {
    setup()
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await user.click(await screen.findByTestId('design-new'))
    const editor = await screen.findByRole('dialog', { name: 'Design' })

    // Unchanged: Back closes straight away.
    await user.click(await within(editor).findByRole('button', { name: 'Back to the post' }))
    await waitFor(() => expect(drawer()).not.toBeInTheDocument())

    await user.click(screen.getByTestId('design-new'))
    const again = await screen.findByRole('dialog', { name: 'Design' })

    await user.click(await within(again).findByRole('button', { name: /^Rename/ }))
    await user.keyboard('Spring sale')
    // The first Escape leaves the name field, the next one asks.
    await user.keyboard('{Escape}')
    await user.keyboard('{Escape}')
    const ask = await screen.findByRole('alertdialog', { name: 'Leave without saving?' })

    expect(drawer()).toBeInTheDocument()
    await user.click(within(ask).getByRole('button', { name: 'Cancel' }))
    expect(drawer()).toBeInTheDocument()

    await user.click(within(again).getByRole('button', { name: 'Back to the post' }))
    await user.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Leave without saving' }),
    )
    await waitFor(() => expect(drawer()).not.toBeInTheDocument())
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  })

  it("a comment's Design puts the picture in that comment", async () => {
    setup()
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await user.click(await screen.findByTestId('add-comment'))
    const part = screen.getByTestId('thread-part-0')

    await user.click(within(part).getByTestId('design-new'))
    const editor = await screen.findByRole('dialog', { name: 'Design' })

    await user.click(await within(editor).findByRole('button', { name: /Save and use in the post/ }))
    await waitFor(() => expect(drawer()).not.toBeInTheDocument())
    expect(await within(part).findByRole('img', { name: 'Design 1.png' })).toBeInTheDocument()
    expect(screen.getAllByRole('img')).toHaveLength(1)
  })
})

describe('where saved pictures go', () => {
  const a = image('a')
  const b = image('b')
  const n1 = image('n1')
  const n2 = image('n2')

  it('a new design comes last', () => {
    expect(placeSaved([a], [{ after: n1 }, { after: n2 }]).map((m) => m.id)).toEqual(['a', 'n1', 'n2'])
  })

  it('an edited picture becomes the saved pages where it was', () => {
    expect(placeSaved([a, b], [{ after: n1 }, { after: n2 }], 'a').map((m) => m.id)).toEqual(['n1', 'n2', 'b'])
  })

  it('a page saved before replaces its own file, and pages the post does not have stay out', () => {
    expect(
      placeSaved(
        [a, b],
        [
          { before: 'x', after: n1 },
          { before: 'b', after: n2 },
        ],
        'b',
      ).map((m) => m.id),
    ).toEqual(['a', 'n2'])
  })
})
