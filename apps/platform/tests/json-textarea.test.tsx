import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { JsonTextarea } from '@/components/common/json-textarea'

const PRETTY = '{\n  "a": 1,\n  "b": [\n    1,\n    2\n  ]\n}'

describe('JsonTextarea', () => {
  it('numbers the lines and offers Format for valid JSON that is not pretty yet', async () => {
    const onChange = vi.fn<(v: string) => void>()
    const user = userEvent.setup()
    const { container, rerender } = render(
      <JsonTextarea aria-label="Metadata" value='{"a":1,"b":[1,2]}' onChange={onChange} />,
    )

    expect(container.querySelectorAll('[aria-hidden] > div')).toHaveLength(1)
    await user.click(screen.getByRole('button', { name: 'Format JSON' }))
    expect(onChange).toHaveBeenCalledWith(PRETTY)

    rerender(<JsonTextarea aria-label="Metadata" value={PRETTY} onChange={onChange} />)
    expect(container.querySelectorAll('[aria-hidden] > div')).toHaveLength(7)
    expect(screen.queryByRole('button', { name: 'Format JSON' })).toBeNull()
    // Blurring never reformats (the field would grow under the button being clicked).
    fireEvent.blur(screen.getByRole('textbox', { name: 'Metadata' }))
    expect(onChange).toHaveBeenCalledTimes(1)
  })

  it('pretty-prints JSON pasted over the whole field, leaves broken JSON and read-only fields alone', () => {
    const onChange = vi.fn<(v: string) => void>()
    const { rerender } = render(<JsonTextarea aria-label="Metadata" value="" onChange={onChange} />)
    const box = screen.getByRole('textbox', { name: 'Metadata' })

    fireEvent.paste(box, { clipboardData: { getData: () => '{"a":1,"b":[1,2]}' } })
    expect(onChange).toHaveBeenCalledWith(PRETTY)
    onChange.mockClear()
    fireEvent.paste(box, { clipboardData: { getData: () => '{oops' } })
    expect(onChange).not.toHaveBeenCalled()

    rerender(<JsonTextarea aria-label="Metadata" value="{oops" onChange={onChange} />)
    expect(screen.queryByRole('button', { name: 'Format JSON' })).toBeNull()
    rerender(<JsonTextarea aria-label="Metadata" value='{"a":1}' onChange={onChange} readOnly />)
    expect(screen.queryByRole('button', { name: 'Format JSON' })).toBeNull()
  })
})
