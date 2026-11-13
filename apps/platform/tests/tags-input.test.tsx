import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'

import { MultiSelect } from '@/components/common/multi-select'
import { splitTags, tagInput } from '@/lib/tags'

const none: string[] = []

function Tags({ start = none, known = none }: { start?: string[]; known?: string[] }) {
  const [tags, setTags] = useState<string[]>(start)

  return (
    <>
      <MultiSelect
        label="Tags"
        value={tags}
        onChange={setTags}
        options={known.map((t) => ({ value: t, label: `#${t}` }))}
        creatable={tagInput}
      />
      <output data-testid="tags">{tags.join('|')}</output>
    </>
  )
}

describe('tags field', () => {
  it('removes a tag added by typing and one from the workspace by clicking it again', async () => {
    const user = userEvent.setup()

    render(<Tags start={['my-own', 'deadlift']} known={['deadlift']} />)
    await user.click(screen.getByRole('button', { name: /Tags/ }))
    await user.click(await screen.findByRole('option', { name: 'my-own' }))
    await user.click(screen.getByRole('option', { name: '#deadlift' }))
    expect(screen.getByTestId('tags')).toHaveTextContent(/^$/)
  })

  it('removes a picked tag with its remove button, and the last one with Backspace in the empty box', async () => {
    const user = userEvent.setup()

    render(<Tags start={['gym', 'news', 'launch']} />)
    await user.click(screen.getByRole('button', { name: /Tags/ }))
    await user.click(await screen.findByRole('button', { name: 'Remove news' }))
    expect(screen.getByTestId('tags')).toHaveTextContent('gym|launch')
    await user.click(screen.getByPlaceholderText('Search or add...'))
    await user.keyboard('{Backspace}')
    expect(screen.getByTestId('tags')).toHaveTextContent(/^gym$/)
  })

  it('adds each hashtag of a pasted list as its own tag', async () => {
    const user = userEvent.setup()

    render(<Tags start={['deadlift']} />)
    await user.click(screen.getByRole('button', { name: /Tags/ }))
    await user.click(screen.getByPlaceholderText('Search or add...'))
    await user.paste('#deadlift #StrengthTraining #gymlife, personal record')
    await user.click(screen.getByRole('option', { name: /Add/ }))
    expect(screen.getByTestId('tags')).toHaveTextContent('deadlift|strengthtraining|gymlife|personal-record')
  })

  it('refuses a tag longer than the API takes before the post is saved', async () => {
    const user = userEvent.setup()

    render(<Tags />)
    await user.click(screen.getByRole('button', { name: /Tags/ }))
    await user.click(screen.getByPlaceholderText('Search or add...'))
    await user.paste('a'.repeat(41))
    expect(screen.getByRole('option', { name: /Too long: at most 40 characters/ })).toHaveAttribute(
      'aria-disabled',
      'true',
    )
    await user.keyboard('{Tab}')
    expect(screen.getByTestId('tags')).toHaveTextContent(/^$/)
  })
})

describe('splitTags', () => {
  it('splits on commas and before a #, keeping a name with spaces whole', () => {
    expect(splitTags('#a #b, c d,#e')).toEqual(['#a', '#b', ' c d', '#e'])
  })
})
