import { describe, expect, it } from 'vitest'

import { docToText, textToHtml } from '@/lib/post-text'

const p = (...content: object[]) => ({ type: 'paragraph', content })
const t = (text: string, ...marks: string[]) => ({ type: 'text', text, marks: marks.map((type) => ({ type })) })

describe('post text', () => {
  it('turns text into one paragraph per line, escaped, blank lines kept', () => {
    expect(textToHtml('Hi <b>\n\nBye & go')).toBe('<p>Hi &lt;b&gt;</p><p></p><p>Bye &amp; go</p>')
  })

  it('turns the document back into lines, Unicode bold and italic, list markers and link URLs', () => {
    const doc = {
      type: 'doc',
      content: [
        p(t('Sale '), t('Mon 9', 'bold'), t(' now', 'italic')),
        { type: 'paragraph' },
        {
          type: 'bulletList',
          content: [
            { type: 'listItem', content: [p(t('one'))] },
            { type: 'listItem', content: [p(t('two'))] },
          ],
        },
        { type: 'orderedList', attrs: { start: 3 }, content: [{ type: 'listItem', content: [p(t('three'))] }] },
        p(
          { type: 'text', text: 'shop', marks: [{ type: 'link', attrs: { href: 'https://x.test' } }] },
          { type: 'hardBreak' },
          t('end'),
        ),
      ],
    }

    expect(docToText(doc)).toBe('Sale 𝗠𝗼𝗻 𝟵 𝘯𝘰𝘸\n\n• one\n• two\n3. three\nshop (https://x.test)\nend')
  })
})
