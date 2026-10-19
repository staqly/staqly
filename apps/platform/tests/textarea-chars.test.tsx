import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'

import { Textarea } from '@/components/ui/textarea'
import { charCount, clampChars } from '@/lib/chars'

function Limited({ max }: { max: number }) {
  const [value, setValue] = useState('')

  return <Textarea aria-label="note" maxLength={max} value={value} onChange={(e) => setValue(e.target.value)} />
}

describe('character limits', () => {
  it('counts code points like the API: an emoji or a Polish letter is one character', () => {
    expect(charCount('zażółć 🚀')).toBe(8)
    expect(clampChars('ab🚀🚀', 3)).toBe('ab🚀')
  })

  it('a textarea with maxLength allows that many characters, emoji included, and counts them', () => {
    render(<Limited max={5} />)
    const box = screen.getByLabelText('note')

    fireEvent.change(box, { target: { value: '🚀🚀🚀🚀🚀' } })
    expect(box).toHaveValue('🚀🚀🚀🚀🚀')
    expect(screen.getByText('5/5')).toBeInTheDocument()
    fireEvent.change(box, { target: { value: '🚀🚀🚀🚀🚀ż' } })
    expect(box).toHaveValue('🚀🚀🚀🚀🚀')
  })
})
