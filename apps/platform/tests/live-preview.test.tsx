import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { Composer } from '@/components/posts/composer'
import { foldText, segments, splitAtLimit } from '@/components/posts/preview/preview-text'
import { xLength } from '@/lib/posts'

import { channel, collection, provider } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

// ProseMirror does not type in jsdom: the editor is a plain textarea here, as in composer.test.tsx.
vi.mock('@/components/posts/post-editor', () => ({
  PostEditor: (p: {
    id: string
    value: string
    onChange: (t: string) => void
    labelledBy?: string
    testId?: string
    below?: React.ReactNode
    start?: React.ReactNode
    end?: React.ReactNode
  }) => (
    <>
      <textarea
        id={p.id}
        value={p.value}
        onChange={(e) => p.onChange(e.target.value)}
        aria-labelledby={p.labelledBy}
        data-testid={p.testId}
      />
      {p.below}
      {p.start}
      {p.end}
    </>
  ),
}))

const routes = {
  'GET /v1/collections': { data: [collection()] },
  'GET /v1/channels': {
    data: [
      channel(),
      channel({ id: 'ch_2', name: 'Brand on Bluesky', provider: 'bluesky', handle: '@brand.bsky.social' }),
      channel({ id: 'ch_3', name: 'Brand on Kick', provider: 'kick', handle: 'brandkick' }),
    ],
  },
  'GET /v1/providers': {
    data: [
      provider(),
      provider({ id: 'bluesky', name: 'Bluesky', settings_fields: [] }),
      provider({ id: 'kick', name: 'Kick', settings_fields: [] }),
    ],
  },
}

async function pickChannels(user: ReturnType<typeof userEvent.setup>, ...ids: string[]) {
  await user.click(await screen.findByRole('button', { name: 'Channels' }))

  for (const id of ids) {
    await user.click(await screen.findByTestId(`pick-channel-${id}`))
  }

  await user.keyboard('{Escape}')
}

describe('live preview', () => {
  it('opens from the icon, follows the text, and draws each channel its platform way', async () => {
    mockApi(routes)
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1', 'ch_2', 'ch_3')
    await user.type(screen.getByTestId('post-content'), 'Hello from everyone')

    // Bluesky gets its own text.
    await user.click(screen.getByRole('tab', { name: 'Text for Brand on Bluesky' }))
    await user.clear(screen.getByTestId('override-ch_2'))
    await user.type(screen.getByTestId('override-ch_2'), 'Only on Bluesky #sky')

    await user.click(screen.getByRole('button', { name: 'Live preview' }))
    const sheet = await screen.findByTestId('post-preview-sheet')

    // It opens on the channel being written for: Bluesky's own look, its name, its own text.
    const bsky = await within(sheet).findByRole('article', { name: 'Bluesky post by Brand on Bluesky' })

    expect(bsky).toHaveTextContent('Brand on Bluesky')
    expect(bsky).toHaveTextContent('@brand.bsky.social')
    expect(bsky).toHaveTextContent('Only on Bluesky #sky')
    expect(bsky).not.toHaveTextContent('Hello from everyone')

    // All channels: the neutral card with the shared text.
    await user.click(within(sheet).getByRole('tab', { name: 'All channels' }))
    const all = await within(sheet).findByRole('article', { name: 'Post for all channels' })

    expect(all).toHaveTextContent('Hello from everyone')

    // Mastodon has its own template.
    await user.click(within(sheet).getByRole('tab', { name: 'Preview for Brand on Mastodon' }))
    expect(await within(sheet).findByRole('article', { name: 'Mastodon post by Brand on Mastodon' })).toHaveTextContent(
      'Hello from everyone',
    )

    // A platform without a template of its own gets the neutral card with its name.
    await user.click(within(sheet).getByRole('tab', { name: 'Preview for Brand on Kick' }))
    expect(await within(sheet).findByRole('article', { name: 'Kick post by Brand on Kick' })).toHaveTextContent('Kick')

    // Typing in the composer shows at once, no saving.
    await user.click(within(screen.getByTestId('composer')).getByRole('tab', { name: 'All channels' }))
    await user.type(screen.getByTestId('post-content'), ' and more')
    expect(within(sheet).getByRole('article', { name: 'Kick post by Brand on Kick' })).toHaveTextContent(
      'Hello from everyone and more',
    )

    // Back to the post closes it.
    await user.click(within(sheet).getByRole('button', { name: 'Back to the post' }))
    expect(screen.queryByTestId('post-preview-sheet')).not.toBeInTheDocument()
  })

  it('the text row has no visible label but keeps its name', async () => {
    mockApi(routes)
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1', 'ch_2')
    const row = screen.getByRole('tablist', { name: 'Text for' })

    expect(row).toBeInTheDocument()
    expect(within(row).queryByText('Text for')).not.toBeInTheDocument()
  })
})

describe('preview text', () => {
  it('splits at a limit counted the platform way', () => {
    expect(splitAtLimit('abcdef', 4, (t) => [...t].length)).toEqual(['abcd', 'ef'])
    expect(splitAtLimit('abc', 4, (t) => [...t].length)).toEqual(['abc', ''])
    // X counts an emoji as 2.
    expect(splitAtLimit('ab😀c', 3, xLength)).toEqual(['ab', '😀c'])
  })

  it('folds after lines or characters, at a word', () => {
    expect(foldText('one\ntwo\nthree\nfour', 1000, 3)).toBe('one\ntwo\nthree')
    expect(foldText('short', 100, 3)).toBeNull()
    expect(foldText('hello wonderful world', 18, 3)).toBe('hello wonderful')
  })

  it('marks links, hashtags and mentions', () => {
    expect(segments('Hi @ann see https://staqly.ai #news, me@mail.com')).toEqual([
      { kind: 'text', text: 'Hi ' },
      { kind: 'mention', text: '@ann' },
      { kind: 'text', text: ' see ' },
      { kind: 'link', text: 'https://staqly.ai' },
      { kind: 'text', text: ' ' },
      { kind: 'tag', text: '#news' },
      { kind: 'text', text: ', me@mail.com' },
    ])
  })
})
