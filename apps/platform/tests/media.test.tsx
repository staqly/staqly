import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { LibraryPickerButton } from '@/components/media/library-picker'
import { FOLDER_DRAG, isExternalFileDrag, MEDIA_DRAG } from '@/components/media/media-folders'
import { MediaPage } from '@/components/media/media-page'
import type { Media, MediaLibraryItem } from '@/lib/api'
import { cropShare, fitVerdict, folderTree, formatDuration, ratioLabel, typeLabel } from '@/lib/media'

import { PLANS, usage } from './plans-fixtures'
import { mockApi, renderWithProviders } from './utils'

function item(id: string, over: Partial<MediaLibraryItem> = {}): MediaLibraryItem {
  return {
    id,
    mime_type: 'image/png',
    size_bytes: 2048,
    original_name: `${id}.png`,
    url: `http://localhost/v1/media/${id}`,
    sha256: 'x',
    created_at: '2026-10-20T10:00:00Z',
    alt_text: '',
    width: 1080,
    height: 1350,
    duration_ms: 0,
    folder_id: '',
    tags: [],
    uses: 0,
    ...over,
  }
}

const SUNSET = item('med_sun', { original_name: 'Sunset.png', uses: 2 })
const CLIP = item('med_clip', {
  original_name: 'clip.mp4',
  mime_type: 'video/mp4',
  width: 1080,
  height: 1920,
  duration_ms: 15_000,
})

describe('media helpers', () => {
  it('reads ratios, crops and lengths', () => {
    expect(ratioLabel(1080, 1350)).toBe('4:5')
    expect(ratioLabel(1920, 1080)).toBe('16:9')
    expect(ratioLabel(1000, 523)).toBe('1.91:1')
    expect(cropShare(1080, 1080, 1, 1)).toBe(0)
    expect(Math.round(cropShare(1080, 1920, 16, 9) * 100)).toBe(68)
    expect(formatDuration(15_000)).toBe('0:15')
    expect(formatDuration(3_723_000)).toBe('1:02:03')
    expect(fitVerdict(0.004)).toEqual({ tone: 'fits', label: 'Fits' })
    expect(fitVerdict(0.2)).toEqual({ tone: 'some', label: 'Crops 20%' })
    expect(fitVerdict(0.44)).toEqual({ tone: 'much', label: 'Crops 44%' })
    expect(typeLabel('image/png')).toBe('PNG image')
    expect(typeLabel('image/jpeg')).toBe('JPG image')
    expect(typeLabel('application/pdf')).toBe('PDF file')
  })

  it('orders folders as a tree', () => {
    const tree = folderTree([
      { id: 'b', parent_id: 'a', name: 'ACME' },
      { id: 'a', parent_id: '', name: 'Clients' },
      { id: 'c', parent_id: '', name: 'Brand' },
      { id: 'd', parent_id: 'gone', name: 'Orphan' },
    ])

    expect(tree.map((f) => `${f.depth}:${f.name}`)).toEqual(['0:Brand', '0:Clients', '1:ACME', '0:Orphan'])
    expect(tree.find((f) => f.id === 'a')?.hasChildren).toBe(true)
  })
})

describe('media library page', () => {
  it('lists files, filters through the API, opens a file and saves its alt text', async () => {
    const api = mockApi({
      'GET /v1/media': ({ query }: { query: URLSearchParams }) => ({
        data: query.get('kind') === 'video' ? [CLIP] : [SUNSET, CLIP],
      }),
      'GET /v1/media/med_sun/usage': {
        data: [{ post_id: 'pst_1', collection_id: 'col_1', title: 'Race week', status: 'scheduled' }],
      },
      'PATCH /v1/media/med_sun': { ...SUNSET, alt_text: 'Sun over the city' },
      'GET /v1/workspace/usage': usage(PLANS[1]!),
    })
    const user = userEvent.setup()

    renderWithProviders(<MediaPage />, { mode: 'cloud' })
    const tiles = await screen.findAllByTestId('media-tile')

    expect(tiles).toHaveLength(2)
    expect(screen.getByText('In 2 posts')).toBeInTheDocument()
    expect(screen.getByText('0:15')).toBeInTheDocument()

    // LIBRARY in the pane filters by type (no separate type select).
    const pane = screen.getByTestId('media-pane')

    expect(screen.queryByRole('combobox', { name: 'Type filter' })).toBeNull()
    await user.click(within(pane).getByRole('button', { name: 'Videos' }))
    await waitFor(() => expect(screen.getAllByTestId('media-tile')).toHaveLength(1))
    expect(api.calls.some((c) => c.path === '/v1/media' && c.query.get('kind') === 'video')).toBe(true)

    await user.click(within(pane).getByRole('button', { name: /All files/ }))
    await user.click((await screen.findAllByRole('button', { name: 'Sunset.png' }))[0]!)
    const drawer = await screen.findByTestId('media-detail')

    // Above the tabs: the preview, one line of facts; the ID sits in the header with a copy button.
    expect(within(drawer).getByTestId('media-facts')).toHaveTextContent('PNG · 2 KB · 1080 × 1350 · 4:5')
    expect(within(drawer).getByTestId('media-id')).toHaveTextContent('med_sun')
    expect(within(drawer).getByRole('button', { name: 'Copy file ID' })).toBeInTheDocument()
    expect(within(drawer).getByRole('button', { name: 'Open full screen preview' })).toBeInTheDocument()

    // Details: a definition list.
    await user.click(within(drawer).getByRole('tab', { name: 'Details' }))
    const details = within(drawer).getByTestId('media-details')

    expect(details).toHaveTextContent('PNG image · image/png')
    expect(details).toHaveTextContent('Aspect ratio4:5 · portrait')

    // Where it fits: grouped by platform, with a count on the tab.
    await user.click(within(drawer).getByRole('tab', { name: /Where it fits/ }))
    expect(within(drawer).getByRole('tab', { name: /Where it fits/ })).toHaveTextContent('2/11')
    const fits = within(drawer).getAllByTestId('media-fit')

    expect(fits.find((f) => f.textContent?.includes('Feed portrait'))).toHaveTextContent('Fits')
    expect(fits.find((f) => f.textContent?.includes('Stories, Reels'))).toHaveTextContent('Crops 30%')
    expect(within(drawer).getByTestId('media-fits')).toHaveTextContent('Instagram1 of 3 fit')

    // Used in: the posts.
    await user.click(within(drawer).getByRole('tab', { name: /Used in/ }))
    expect(await within(drawer).findByText('Race week')).toBeInTheDocument()
    await user.click(within(drawer).getByRole('tab', { name: 'File' }))
    // Delete sits in the footer, left of Close and Save changes.
    const footer = within(drawer).getByRole('button', { name: 'Save changes' }).closest('div')!.parentElement!

    expect(within(footer).getByRole('button', { name: 'Delete file' })).toBeInTheDocument()
    await user.type(within(drawer).getByLabelText('Alt text'), 'Sun over the city')
    await user.click(within(drawer).getByRole('button', { name: 'Save changes' }))
    await waitFor(() => expect(api.find('PATCH', '/v1/media/med_sun')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/media/med_sun')[0]!.body).toEqual({
      name: 'Sunset.png',
      alt_text: 'Sun over the city',
    })
  })

  it('moves selected files to Deleted with Undo', async () => {
    const api = mockApi({
      'GET /v1/media': { data: [SUNSET, CLIP] },
      'POST /v1/media/delete': { deleted: ['med_sun', 'med_clip'], missing: [] },
      'POST /v1/media/med_sun/restore': SUNSET,
      'POST /v1/media/med_clip/restore': CLIP,
      'GET /v1/workspace/usage': usage(PLANS[1]!),
    })
    const user = userEvent.setup()

    renderWithProviders(<MediaPage />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: 'Select Sunset.png' }))
    await user.click(screen.getByRole('button', { name: 'Select clip.mp4' }))
    expect(screen.getByRole('toolbar', { name: 'Selected files' })).toHaveTextContent('2 selected')
    await user.click(screen.getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(api.find('POST', '/v1/media/delete')).toHaveLength(1))
    expect(api.find('POST', '/v1/media/delete')[0]!.body).toEqual({ ids: ['med_sun', 'med_clip'] })
    expect(await screen.findByText('Moved to Deleted')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Undo' }))
    await waitFor(() => expect(api.find('POST', '/v1/media/med_clip/restore')).toHaveLength(1))
    expect(api.find('POST', '/v1/media/med_sun/restore')).toHaveLength(1)
  })

  it('refuses a file over the plan limit before uploading, in plain words', async () => {
    mockApi({
      'GET /v1/media': { data: [] },
      'GET /v1/workspace/usage': usage(PLANS[0]!),
    })
    const user = userEvent.setup()

    renderWithProviders(<MediaPage />, { mode: 'cloud' })
    expect(await screen.findByText('No media yet')).toBeInTheDocument()
    const big = new File([new Uint8Array(1)], 'huge.mp4', { type: 'video/mp4' })

    Object.defineProperty(big, 'size', { value: 5 * 1024 * 1024 * 1024 })
    await user.upload(screen.getByTestId('media-upload-input'), big)
    expect(await screen.findByText(/Too big: .* Your plan takes files up to/)).toBeInTheDocument()
  })
})

const FOLDERS = {
  data: [
    { id: 'mfd_c', parent_id: '', name: 'Clients', files: 1, created_at: 't' },
    { id: 'mfd_a', parent_id: 'mfd_c', name: 'ACME', files: 2, created_at: 't' },
  ],
  root_files: 1,
  total_files: 4,
  tags: [{ tag: 'hero', files: 2 }],
}

describe('media folders and tags', () => {
  it('shows the folder tree, opens and closes it from the keyboard, filters by folder and tag', async () => {
    const api = mockApi({
      'GET /v1/media': { data: [SUNSET] },
      'GET /v1/media-folders': FOLDERS,
      'GET /v1/workspace/usage': usage(PLANS[1]!),
    })
    const user = userEvent.setup()
    const listed = (folder: string, tag?: string) =>
      api.calls.some(
        (c) => c.path === '/v1/media' && c.query.get('folder') === folder && (!tag || c.query.get('tag') === tag),
      )

    renderWithProviders(<MediaPage />, { mode: 'cloud' })
    const tree = await screen.findByRole('tree', { name: 'Folders' })
    const clients = await within(tree).findByRole('treeitem', { name: /Clients/ })

    expect(clients).toHaveAttribute('aria-expanded', 'true')
    // Root heads the tree: every folder sits inside it.
    expect(within(tree).getByRole('treeitem', { name: /^\// })).toHaveAttribute('aria-level', '1')
    expect(clients).toHaveAttribute('aria-level', '2')
    expect(within(tree).getByRole('treeitem', { name: /ACME/ })).toHaveAttribute('aria-level', '3')
    clients.focus()
    await user.keyboard('{ArrowLeft}')
    expect(clients).toHaveAttribute('aria-expanded', 'false')
    expect(within(tree).queryByRole('treeitem', { name: /ACME/ })).toBeNull()
    await user.keyboard('{ArrowRight}{ArrowDown}{Enter}')
    await waitFor(() => expect(listed('mfd_a')).toBe(true))
    expect(screen.getByTestId('media-place')).toHaveTextContent('ACME')
    // The path above the grid: every step but the current folder opens it.
    const path = screen.getByRole('navigation', { name: 'Folder path' })

    expect(within(path).getByRole('button', { name: 'All files' })).toBeInTheDocument()
    expect(within(path).getByRole('button', { name: 'Clients' })).toBeInTheDocument()
    expect(within(path).getByText('ACME')).toHaveAttribute('aria-current', 'page')

    await user.click(within(screen.getByTestId('media-tags')).getByRole('button', { name: /hero/ }))
    await waitFor(() => expect(listed('mfd_a', 'hero')).toBe(true))

    // Root lists the files in no folder.
    await user.click(within(tree).getByRole('treeitem', { name: /^\// }))
    await waitFor(() => expect(listed('root')).toBe(true))
    expect(screen.getByTestId('media-place')).toHaveTextContent('/')
  })

  it('hides Tags while no file has one', async () => {
    mockApi({
      'GET /v1/media': { data: [SUNSET] },
      'GET /v1/media-folders': { ...FOLDERS, tags: [] },
      'GET /v1/workspace/usage': usage(PLANS[1]!),
    })
    renderWithProviders(<MediaPage />, { mode: 'cloud' })
    await screen.findByRole('treeitem', { name: /^\// })
    expect(screen.queryByTestId('media-tags')).toBeNull()
    expect(screen.queryByText('Tags')).toBeNull()
  })

  it('moves selected files to a folder and tags them in bulk', async () => {
    const api = mockApi({
      'GET /v1/media': { data: [SUNSET, CLIP] },
      'GET /v1/media-folders': FOLDERS,
      'POST /v1/media/bulk': { updated: ['med_sun', 'med_clip'], missing: [] },
      'GET /v1/workspace/usage': usage(PLANS[1]!),
    })
    const user = userEvent.setup()

    renderWithProviders(<MediaPage />, { mode: 'cloud' })
    await screen.findByRole('treeitem', { name: /ACME/ })
    await user.click(await screen.findByRole('button', { name: 'Select Sunset.png' }))
    await user.click(screen.getByRole('button', { name: 'Select clip.mp4' }))
    await user.click(screen.getByRole('button', { name: 'Move to folder' }))
    const picker = await screen.findByTestId('media-folder-picker')

    expect(within(picker).getByRole('button', { name: 'No folder' })).toBeInTheDocument()
    await user.click(within(picker).getByRole('button', { name: 'ACME' }))
    await waitFor(() => expect(api.find('POST', '/v1/media/bulk')).toHaveLength(1))
    expect(api.find('POST', '/v1/media/bulk')[0]!.body).toEqual({ ids: ['med_sun', 'med_clip'], folder_id: 'mfd_a' })

    await user.click(screen.getByRole('button', { name: 'Select Sunset.png' }))
    await user.click(
      within(screen.getByRole('toolbar', { name: 'Selected files' })).getByRole('button', { name: 'Tags' }),
    )
    const dialog = await screen.findByTestId('media-tags-dialog')

    await user.click(within(dialog).getByRole('button', { name: 'Add tags' }))
    await user.keyboard('Summer Sale')
    await user.click(await screen.findByRole('option', { name: 'Add “summer-sale”' }))
    await user.click(within(dialog).getByRole('button', { name: 'Apply' }))
    await waitFor(() => expect(api.find('POST', '/v1/media/bulk')).toHaveLength(2))
    expect(api.find('POST', '/v1/media/bulk')[1]!.body).toEqual({
      ids: ['med_sun'],
      add_tags: ['summer-sale'],
      remove_tags: [],
    })
  })

  it('asks before deleting a folder and says no file is deleted', async () => {
    const api = mockApi({
      'GET /v1/media': { data: [] },
      'GET /v1/media-folders': FOLDERS,
      'DELETE /v1/media-folders/mfd_c': { status: 204 },
      'GET /v1/workspace/usage': usage(PLANS[1]!),
    })
    const user = userEvent.setup()

    renderWithProviders(<MediaPage />, { mode: 'cloud' })
    await user.click(await screen.findByRole('treeitem', { name: /Clients/ }))
    await user.click(screen.getByRole('button', { name: 'Folder actions' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Delete folder' }))
    const confirm = await screen.findByRole('alertdialog')

    expect(confirm).toHaveTextContent('Its 1 file and 1 folder move to No folder. No file is deleted.')
    await user.click(within(confirm).getByRole('button', { name: 'Delete folder' }))
    await waitFor(() => expect(api.find('DELETE', '/v1/media-folders/mfd_c')).toHaveLength(1))
  })
})

/** Drops one library file on an element, as a tile drag would. */
const dropMedia = (el: Element) => {
  const data: Record<string, string> = { [MEDIA_DRAG]: JSON.stringify(['med_sun']) }
  const dataTransfer = { types: [MEDIA_DRAG], getData: (k: string) => data[k] ?? '', dropEffect: 'none' }

  fireEvent.dragOver(el, { dataTransfer })
  fireEvent.drop(el, { dataTransfer })
}

describe('media drag and drop, Deleted and Duplicates', () => {
  it('shows the upload overlay only for files dragged in from outside', () => {
    expect(isExternalFileDrag(['Files'])).toBe(true)
    // A tile or a folder moved inside the page: no overlay, even when the browser adds Files.
    expect(isExternalFileDrag([MEDIA_DRAG])).toBe(false)
    expect(isExternalFileDrag(['Files', MEDIA_DRAG])).toBe(false)
    expect(isExternalFileDrag([FOLDER_DRAG])).toBe(false)
    expect(isExternalFileDrag(['text/plain'])).toBe(false)
  })

  it('moves files dropped on a folder, or on All files to no folder', async () => {
    const api = mockApi({
      'GET /v1/media': { data: [SUNSET] },
      'GET /v1/media-folders': FOLDERS,
      'POST /v1/media/bulk': { updated: ['med_sun'], missing: [] },
      'GET /v1/workspace/usage': usage(PLANS[1]!),
    })

    renderWithProviders(<MediaPage />, { mode: 'cloud' })
    await screen.findAllByTestId('media-tile')
    dropMedia(await screen.findByRole('treeitem', { name: /ACME/ }))
    await waitFor(() => expect(api.find('POST', '/v1/media/bulk')).toHaveLength(1))
    expect(api.find('POST', '/v1/media/bulk')[0]!.body).toEqual({ ids: ['med_sun'], folder_id: 'mfd_a' })
    expect(screen.queryByTestId('media-drop-overlay')).toBeNull()
    dropMedia(within(screen.getByTestId('media-pane')).getByRole('button', { name: /All files/ }))
    await waitFor(() => expect(api.find('POST', '/v1/media/bulk')).toHaveLength(2))
    expect(api.find('POST', '/v1/media/bulk')[1]!.body).toEqual({ ids: ['med_sun'], folder_id: '' })
  })

  it('lists Deleted with when each file goes, restores one and empties it', async () => {
    const when = { deleted_at: '2026-10-20T10:00:00Z', purge_at: '2099-11-19T10:00:00Z' }
    let deleted = [{ ...SUNSET, ...when }, item('med_old', { original_name: 'old.png', ...when })]
    const api = mockApi({
      'GET /v1/media': ({ query }: { query: URLSearchParams }) => ({
        data: query.get('deleted') === 'true' ? deleted : [CLIP],
      }),
      'GET /v1/media-folders': { ...FOLDERS, deleted_files: 2 },
      'POST /v1/media/med_sun/restore': () => {
        deleted = deleted.filter((m) => m.id !== 'med_sun')

        return SUNSET
      },
      'POST /v1/media/delete': { deleted: ['med_old'], missing: [] },
      'GET /v1/workspace/usage': usage(PLANS[1]!),
    })
    const user = userEvent.setup()

    renderWithProviders(<MediaPage />, { mode: 'cloud' })
    await user.click(await within(screen.getByTestId('media-pane')).findByRole('button', { name: /Deleted/ }))
    expect(
      await screen.findByText(/deleted for good after 30 days and still count toward your storage/),
    ).toBeInTheDocument()
    expect(screen.getAllByText(/^Deletes in/)).toHaveLength(2)
    await user.click(screen.getByRole('button', { name: 'Select Sunset.png' }))
    await user.click(
      within(screen.getByRole('toolbar', { name: 'Selected files' })).getByRole('button', { name: 'Restore' }),
    )
    await waitFor(() => expect(api.find('POST', '/v1/media/med_sun/restore')).toHaveLength(1))
    // The restored file leaves Deleted at once, without reloading the list.
    await waitFor(() => expect(screen.queryByRole('button', { name: /^Sunset\.png/ })).toBeNull())
    expect(api.calls.filter((c) => c.path === '/v1/media' && c.query.get('deleted') === 'true')).toHaveLength(1)

    await user.click(screen.getByRole('button', { name: 'Empty Deleted' }))
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Empty Deleted' }))
    await waitFor(() => expect(api.find('POST', '/v1/media/delete')).toHaveLength(1))
    expect(api.find('POST', '/v1/media/delete')[0]!.body).toEqual({ ids: ['med_old'], permanent: true })
    await waitFor(() => expect(screen.queryAllByTestId('media-tile')).toHaveLength(0))
  })

  it('groups identical files and keeps one', async () => {
    const copy = item('med_copy', { original_name: 'Sunset copy.png', sha256: 'x', created_at: '2026-10-20T11:00:00Z' })
    const api = mockApi({
      'GET /v1/media': ({ query }: { query: URLSearchParams }) => ({
        data: query.get('duplicates') === 'true' ? [SUNSET, copy] : [SUNSET, copy, CLIP],
      }),
      'GET /v1/media-folders': { ...FOLDERS, duplicate_files: 2 },
      'POST /v1/media/med_sun/keep': { deleted: ['med_copy'] },
      'GET /v1/workspace/usage': usage(PLANS[1]!),
    })
    const user = userEvent.setup()

    renderWithProviders(<MediaPage />, { mode: 'cloud' })
    await user.click(await within(screen.getByTestId('media-pane')).findByRole('button', { name: /Duplicates/ }))
    const group = await screen.findByRole('region', { name: '2 copies of Sunset.png' })

    expect(group).toHaveTextContent('2 copies · 2 KB each')
    expect(group).toHaveTextContent('Used in 2 posts')
    await user.click(within(group).getByRole('button', { name: 'Keep one' }))
    const confirm = await screen.findByRole('alertdialog')

    expect(confirm).toHaveTextContent('“Sunset copy.png” move to Deleted')
    await user.click(within(confirm).getByRole('button', { name: 'Keep one' }))
    await waitFor(() => expect(api.find('POST', '/v1/media/med_sun/keep')).toHaveLength(1))
  })
})

describe('library picker', () => {
  it('adds picked pictures and videos to a post', async () => {
    mockApi({
      'GET /v1/media': ({ query }: { query: URLSearchParams }) => ({
        data: query.get('kind') === 'video' ? [CLIP] : [SUNSET],
      }),
    })
    const onAdd = vi.fn<(m: Media) => void>()
    const user = userEvent.setup()

    renderWithProviders(<LibraryPickerButton onAdd={onAdd} />, { mode: 'cloud' })
    await user.click(screen.getByRole('button', { name: 'From library' }))
    const picker = await screen.findByTestId('media-picker')

    await user.click(await within(picker).findByRole('button', { name: 'Select Sunset.png' }))
    await user.click(within(picker).getByRole('button', { name: 'Select clip.mp4' }))
    await user.click(within(picker).getByRole('button', { name: 'Add 2 files' }))
    expect(onAdd.mock.calls.map(([m]) => m.id)).toEqual(['med_sun', 'med_clip'])
  })
})
