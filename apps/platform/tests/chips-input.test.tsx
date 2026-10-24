import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'

import { ChipsInput } from '@/components/common/chips-input'

function Field() {
  const [tags, setTags] = useState(['launch', 'news'])

  return <ChipsInput value={tags} onChange={setTags} label="Tags" />
}

describe('chips input', () => {
  it('keeps keyboard focus in the field after removing a chip or adding one with its button', async () => {
    const user = userEvent.setup()

    render(<Field />)
    await user.click(screen.getByRole('button', { name: 'Remove launch' }))
    expect(screen.queryByText('launch')).not.toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Tags' })).toHaveFocus()

    await user.keyboard('sale')
    await user.click(screen.getByRole('button', { name: 'Add sale' }))
    expect(screen.getAllByTestId('chip').map((c) => c.textContent)).toEqual(['news', 'sale'])
    expect(screen.getByRole('textbox', { name: 'Tags' })).toHaveFocus()
  })
})
