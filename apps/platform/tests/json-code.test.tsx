import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { JsonCode } from '@/components/common/json-code'

describe('JsonCode', () => {
  it('colours keys, strings, numbers and literals and keeps the exact text', () => {
    const json = JSON.stringify({ name: 'a "quoted" value', n: -1.5e3, ok: true, none: null }, null, 2)
    const { container } = render(<JsonCode json={json} />)
    const spans = [...container.querySelectorAll('span')].map((s) => [s.textContent, s.className.split(' ')[0]])

    expect(container.textContent).toBe(json)
    expect(spans).toEqual([
      ['"name"', 'text-sky-700'],
      ['"a \\"quoted\\" value"', 'text-emerald-700'],
      ['"n"', 'text-sky-700'],
      ['-1500', 'text-amber-700'],
      ['"ok"', 'text-sky-700'],
      ['true', 'text-violet-700'],
      ['"none"', 'text-sky-700'],
      ['null', 'text-violet-700'],
    ])
  })
})
