import { describe, expect, it } from 'vitest'

import { suggestionsFor } from '@/components/assistant/assistant-panel'

describe('assistant suggestions', () => {
  it('fit the page the assistant is opened on, else the general ones', () => {
    const [key, analytics] = suggestionsFor('/analytics')

    expect(key).toBe('analytics')
    expect(analytics.items).toHaveLength(4)
    expect(analytics.items[0]!.text).toMatch(/30 days/)
    expect(suggestionsFor('/posts/pst_1')[0]).toBe('posts')
    expect(suggestionsFor('/settings/members')[0]).toBe('')
    expect(suggestionsFor('/settings/members')[1].items[0]!.text).toMatch(/LinkedIn/)
  })
})
