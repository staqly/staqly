import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { PlatformPreview } from '@/components/posts/preview/templates'
import { charCount } from '@/lib/posts'

import { channel } from './fixtures'
import { renderWithProviders } from './utils'

describe('Threads preview', () => {
  it('shows the topic beside the name and numbers the parts of a post with replies', () => {
    renderWithProviders(
      <PlatformPreview
        channel={channel({ provider: 'threads', name: 'Mia', handle: '@miapastmidnight' })}
        providerName="Threads"
        text="Test post"
        media={[]}
        comments={[{ key: 'c1', text: 'test comment', media: [] }]}
        maxChars={500}
        count={charCount}
        settings={{ tags: 'Tag1, other' }}
      />,
    )
    expect(screen.getByText('Tag1')).toBeInTheDocument()
    expect(screen.queryByText('other')).toBeNull()
    expect(screen.getByText('1/2')).toBeInTheDocument()
    expect(screen.getByText('2/2')).toBeInTheDocument()
  })
})
