import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { PlatformPreview } from '@/components/posts/preview/templates'
import { charCount } from '@/lib/posts'

import { channel } from './fixtures'
import { renderWithProviders } from './utils'

describe('Pinterest preview', () => {
  it('is a pin: Save, the title, the link site, the description; a picture is asked for', () => {
    renderWithProviders(
      <PlatformPreview
        channel={channel({ provider: 'pinterest', name: 'Urbexplore Polska' })}
        providerName="Pinterest"
        text="test 1"
        media={[]}
        comments={[]}
        maxChars={500}
        count={charCount}
        settings={{ title: 'Abandoned mill', link: 'https://www.example.com/mill' }}
      />,
    )
    expect(screen.getByLabelText('Pinterest pin by Urbexplore Polska')).toBeInTheDocument()
    expect(screen.getByText('Save')).toBeInTheDocument()
    expect(screen.getByText('Abandoned mill')).toBeInTheDocument()
    expect(screen.getByText('example.com')).toBeInTheDocument()
    expect(screen.getByText('test 1')).toBeInTheDocument()
    expect(screen.getByText('Pinterest pins need a picture.')).toBeInTheDocument()
  })
})
