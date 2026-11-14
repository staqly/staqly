import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import { textOnly, WEBLLM_CDN } from '@/components/assistant/on-device'

describe('the assistant on this device', () => {
  it('loads the WebLLM file the CSP allows', () => {
    const web = readFileSync(join(__dirname, '../../api/internal/web/web.go'), 'utf8')

    expect(web).toContain(`"${WEBLLM_CDN}"`)
  })

  it('sees the conversation as text, without tool calls or results', () => {
    expect(
      textOnly([
        { role: 'user', content: 'Write a post' },
        { role: 'assistant', content: '', tool_calls: [{ id: 'c1', name: 'list_collections', arguments: '{}' }] },
        { role: 'tool', content: '[...]', tool_call_id: 'c1' },
        { role: 'assistant', content: 'Here it is' },
      ]),
    ).toEqual([
      { role: 'user', content: 'Write a post' },
      { role: 'assistant', content: 'Here it is' },
    ])
  })
})
