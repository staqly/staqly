import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useEffect } from 'react'
import { toast } from 'sonner'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { newDesign, newPage, type Design } from '@/components/design/doc'
import { draftKey, draftScope, writeDraft } from '@/components/design/drafts'
import DesignEditor from '@/components/design/editor'
import type { SessionInfo } from '@/lib/api'
import { qk } from '@/lib/queries'

import { type Call, makeClient, mockApi, renderWithProviders, sessionFixture } from './utils'

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

afterEach(() => {
  localStorage.clear()
  toast.dismiss()
})

const project = { id: 'prj_1', name: 'Autumn campaign', source_ids: [], output_id: 'med_1' }
const saved = (id: string) => ({ id, original_name: `${id}.png`, mime_type: 'image/png', size_bytes: 3 })

/** A saved design of two pages, each with the file it saved as. */
function twoPages(): Design {
  const d = newDesign()

  d.name = 'Autumn campaign'
  d.pages = [
    { ...newPage(), mediaId: 'med_1' },
    { ...newPage(), mediaId: 'med_2' },
  ]

  return d
}

describe('opening a saved design', () => {
  it('a design that fails to load shows the error with Try again, and cannot be saved until it has opened', async () => {
    let down = true
    const api = mockApi({
      'GET /v1/providers': { data: [] },
      'GET /v1/media-projects/prj_1': project,
      'GET /v1/media-projects/prj_1/file': () =>
        down ? { status: 503, body: { error: { code: 'unavailable', message: 'Try again' } } } : twoPages(),
      'POST /v1/media/med_1/replace': saved('med_3'),
      'POST /v1/media/med_2/replace': saved('med_4'),
      'PUT /v1/media-projects/prj_1': project,
      'GET /v1/*': { data: [] },
    })
    const user = userEvent.setup()

    renderWithProviders(<DesignEditor search="?project=prj_1" />, { mode: 'local' })

    expect(await screen.findByRole('alert')).toHaveTextContent('Could not open it')
    // No blank canvas to work on, and nothing to save over the project.
    expect(screen.queryByTestId('design-canvas')).toBeNull()
    expect(screen.getByTestId('design-save')).toBeDisabled()

    down = false
    await user.click(screen.getByRole('button', { name: 'Try again' }))
    await screen.findByTestId('design-canvas')
    await user.click(screen.getByTestId('design-save'))

    // What is saved is the design that loaded: both its pages, into its own project.
    await waitFor(() => expect(api.find('PUT', '/v1/media-projects/prj_1')).toHaveLength(1))
    expect(api.find('POST', '/v1/media/med_2/replace')).toHaveLength(1)
  })

  it('a save that failed half way goes on from the files it already replaced', async () => {
    let fail = true
    const api = mockApi({
      'GET /v1/providers': { data: [] },
      'GET /v1/media-projects/prj_1': project,
      'GET /v1/media-projects/prj_1/file': twoPages(),
      // Replacing a file answers a new one (and the old one is gone: posts follow the new id).
      'POST /v1/media/med_1/replace': saved('med_3'),
      'POST /v1/media/med_2/replace': () => (fail ? { status: 500, body: {} } : saved('med_4')),
      'POST /v1/media/med_3/replace': saved('med_5'),
      'PUT /v1/media-projects/prj_1': project,
      'GET /v1/*': { data: [] },
    })
    const user = userEvent.setup()

    renderWithProviders(<DesignEditor search="?project=prj_1" />, { mode: 'local' })
    await screen.findByTestId('design-canvas')
    await user.click(screen.getByTestId('design-save'))
    await screen.findByText('Could not save')

    fail = false
    await user.click(screen.getByTestId('design-save'))
    await waitFor(() => expect(api.find('PUT', '/v1/media-projects/prj_1')).toHaveLength(1))

    // Page 1 replaces the file the first try made, not the one that is gone (which would add a file no post has).
    const replaced = api.calls.filter((c: Call) => c.path.endsWith('/replace')).map((c) => c.path.split('/')[3])

    expect(replaced).toEqual(['med_1', 'med_2', 'med_3', 'med_2'])

    const form = api.find('PUT', '/v1/media-projects/prj_1')[0]!.body as FormData

    expect(form.get('page_ids')).toBe('med_5,med_4')
  })
})

/** A new design opened by whoever the session names. */
function openNew(session: object) {
  mockApi({ 'GET /v1/providers': { data: [] }, 'GET /v1/*': { data: [] } })

  const client = makeClient()

  renderWithProviders(<DesignEditor search="" />, { client, mode: 'cloud' })
  client.setQueryData(qk.session, session)

  return client
}

describe('unsaved changes kept in the browser', () => {
  const draft = { ...newDesign(), name: 'Secret launch' }
  const other = { ...sessionFixture('cloud'), user: { id: 'usr_2' } } as unknown as SessionInfo

  it('are offered to the person who left them, in the workspace they left them in', async () => {
    writeDraft(draftKey('staqly-design-draft:', draftScope(other), { project: '', ids: [] }), draft)
    openNew(other)

    expect(await screen.findByText(/Unsaved changes from/)).toBeInTheDocument()
  })

  it('are not offered to another person or in another workspace, and drafts from before they named either are dropped', async () => {
    writeDraft(draftKey('staqly-design-draft:', draftScope(other), { project: '', ids: [] }), draft)
    // How drafts were kept before: one key for every new design in this browser.
    localStorage.setItem('staqly-design-draft:new', JSON.stringify({ at: Date.now(), doc: draft }))

    const client = openNew({ ...other, user: { id: 'usr_3' } })

    await waitFor(() => expect(localStorage.getItem('staqly-design-draft:new')).toBeNull())
    client.setQueryData(qk.session, { ...other, workspace: { ...other.workspace, id: 'ws_2' } })
    await new Promise((r) => setTimeout(r, 100))

    expect(screen.queryByText(/Unsaved changes from/)).toBeNull()
  })
})
