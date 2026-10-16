import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { TableView } from '@/components/collections/item-views'
import { ItemPreview, ItemThumb } from '@/components/items/item-preview'

import { item } from './fixtures'
import { renderWithProviders } from './utils'

describe('item rendering by type', () => {
  it('renders uploaded images through the media proxy', () => {
    renderWithProviders(<ItemPreview item={item()} />)
    const img = within(screen.getByTestId('preview-image')).getByRole('img')

    expect(img).toHaveAttribute('src', '/api/v1/media/med_1')
    expect(img).toHaveAttribute('alt', 'Avatar v1')
  })

  it('shows a scaled-down image in inbox panes', () => {
    renderWithProviders(<ItemPreview item={item()} scaled />)
    const img = within(screen.getByTestId('preview-image')).getByRole('img')

    expect(img).toHaveAttribute('src', '/api/v1/media/med_1?w=1280')
    expect(img).toHaveAttribute('srcset', '/api/v1/media/med_1?w=640 640w, /api/v1/media/med_1?w=1280 1280w')
  })

  it('loads external image URLs only after a click', async () => {
    const user = userEvent.setup()

    renderWithProviders(<ItemPreview item={item({ media: null, url: 'https://cdn.example.com/a.png' })} />)
    const box = screen.getByTestId('preview-image')

    expect(within(box).queryByRole('img')).not.toBeInTheDocument()
    await user.click(within(box).getByRole('button', { name: /load image/i }))
    expect(within(box).getByRole('img')).toHaveAttribute('src', 'https://cdn.example.com/a.png')
  })

  it('renders videos as a play tile that opens a player', async () => {
    renderWithProviders(
      <ItemPreview
        item={item({
          type: 'video',
          media: { id: 'med_v', mime_type: 'video/mp4', size_bytes: 1, original_name: 'clip.mp4', url: '' },
        })}
      />,
    )
    // A play tile; the video itself only loads in the dialog after a click.
    expect(screen.getByTestId('preview-video').querySelector('video')).toBeNull()
    await userEvent.setup().click(screen.getByRole('button', { name: 'Play clip.mp4' }))
    const video = (await screen.findByRole('dialog')).querySelector('video')!

    expect(video).toHaveAttribute('src', '/api/v1/media/med_v')
    expect(video).toHaveAttribute('controls')
  })

  it('renders text preserving whitespace', () => {
    renderWithProviders(<ItemPreview item={item({ type: 'text', media: null, content: 'line one\n\n  line two' })} />)
    const el = screen.getByTestId('preview-text')

    expect(el.textContent).toBe('line one\n\n  line two')
    expect(el.className).toContain('whitespace-pre-wrap')
  })

  it('pretty-prints JSON, with a full-screen view', async () => {
    renderWithProviders(<ItemPreview item={item({ type: 'json', media: null, content: '{"a":1,"b":[true]}' })} />)
    expect(screen.getByTestId('preview-json').textContent).toBe('{\n  "a": 1,\n  "b": [\n    true\n  ]\n}')
    await userEvent.setup().click(screen.getByRole('button', { name: 'Open JSON content full screen' }))
    expect(await screen.findByRole('dialog', { name: 'JSON content' })).toHaveTextContent('"a": 1')
  })

  it('renders links as a card with an external link', () => {
    renderWithProviders(
      <ItemPreview
        item={item({
          type: 'link',
          media: null,
          title: 'Great read',
          url: 'https://example.com/post',
          content: 'Summary',
        })}
      />,
    )
    const card = screen.getByTestId('preview-link')

    expect(within(card).getByText('example.com')).toBeInTheDocument()
    expect(within(card).getByText('Great read')).toBeInTheDocument()
    expect(within(card).getByRole('link', { name: /open/i })).toHaveAttribute('href', 'https://example.com/post')
    expect(within(card).getByText('Summary')).toBeInTheDocument()
  })

  it('offers a download for other files', () => {
    renderWithProviders(
      <ItemPreview
        item={item({
          type: 'file',
          media: { id: 'med_f', mime_type: 'application/pdf', size_bytes: 4096, original_name: 'brief.pdf', url: '' },
        })}
      />,
    )
    expect(screen.getByText('brief.pdf')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /download/i })).toHaveAttribute('href', '/api/v1/media/med_f?download=1')
  })
})

describe('collection table', () => {
  const items = [
    item(),
    item({ id: 'itm_2', title: 'Caption', type: 'text', media: null, content: 'Hello there', status: 'approved' }),
  ]

  it('shows a row per item with its status and opens it on click', async () => {
    const opened: string[] = []
    const user = userEvent.setup()

    renderWithProviders(<TableView items={items} onOpen={(i) => opened.push(i.id)} />)
    expect(screen.getByText('Avatar v1')).toBeInTheDocument()
    expect(screen.getByText('Pending')).toBeInTheDocument()
    expect(screen.getByText('Approved')).toBeInTheDocument()
    await user.click(screen.getByText('Caption'))
    expect(opened).toEqual(['itm_2'])
  })

  it('uses small thumbnails in list rows and keeps unresizable media as is', () => {
    const { container } = renderWithProviders(
      <>
        <ItemThumb item={item()} />
        <ItemThumb
          item={item({
            id: 'itm_a',
            media: { id: 'med_a', mime_type: 'image/avif', size_bytes: 1, original_name: 'a.avif', url: '' },
          })}
        />
        <ItemThumb item={item({ id: 'itm_e', media: null, url: 'https://cdn.example.com/a.png' })} />
      </>,
    )
    const srcs = [...container.querySelectorAll('img')].map((i) => i.getAttribute('src'))

    expect(srcs).toEqual(['/api/v1/media/med_1?w=160', '/api/v1/media/med_a'])
  })
})
