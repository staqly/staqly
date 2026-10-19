import type { JSONContent } from '@tiptap/react'

/**
 * Posts are plain text: platforms take no HTML. The editor works on a Tiptap document, so these two turn
 * text into the editor's HTML and the document back into text. Bold and italic become Unicode letters
 * (what social schedulers do, since most platforms have no markup), lists become "•" or "1." lines and a
 * link keeps its URL.
 */

const STYLES = {
  bold: { upper: 0x1d5d4, lower: 0x1d5ee, digit: 0x1d7ec },
  italic: { upper: 0x1d608, lower: 0x1d622, digit: 0 },
  boldItalic: { upper: 0x1d63c, lower: 0x1d656, digit: 0x1d7ec },
} as const

function styled(text: string, style: keyof typeof STYLES) {
  const s = STYLES[style]

  return [...text]
    .map((ch) => {
      const c = ch.codePointAt(0)!

      if (c >= 65 && c <= 90) {
        return String.fromCodePoint(s.upper + c - 65)
      }

      if (c >= 97 && c <= 122) {
        return String.fromCodePoint(s.lower + c - 97)
      }

      if (c >= 48 && c <= 57 && s.digit) {
        return String.fromCodePoint(s.digit + c - 48)
      }

      return ch
    })
    .join('')
}

function escapeHtml(s: string) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

/** Plain text to the editor's HTML: one paragraph per line, so blank lines survive the round trip. */
export function textToHtml(text: string) {
  return text
    .split('\n')
    .map((line) => `<p>${escapeHtml(line)}</p>`)
    .join('')
}

function inline(nodes: JSONContent[] = []): string {
  return nodes
    .map((n) => {
      if (n.type === 'hardBreak') {
        return '\n'
      }

      let text = n.text ?? ''
      const marks = new Set((n.marks ?? []).map((m) => m.type))
      const href = n.marks?.find((m) => m.type === 'link')?.attrs?.href as string | undefined

      if (marks.has('bold') && marks.has('italic')) {
        text = styled(text, 'boldItalic')
      } else if (marks.has('bold')) {
        text = styled(text, 'bold')
      } else if (marks.has('italic')) {
        text = styled(text, 'italic')
      }

      return href && href !== n.text ? `${text} (${href})` : text
    })
    .join('')
}

function block(node: JSONContent): string[] {
  switch (node.type) {
    case 'paragraph':
      return [inline(node.content)]
    case 'bulletList':
    case 'orderedList': {
      const start = (node.attrs?.start as number | undefined) ?? 1

      return (node.content ?? []).flatMap((item, i) => {
        const marker = node.type === 'bulletList' ? '• ' : `${start + i}. `
        const lines = (item.content ?? []).flatMap(block)

        return lines.map((l, j) => (j === 0 ? marker + l : `   ${l}`))
      })
    }

    default:
      return node.content ? node.content.flatMap(block) : [inline([node])]
  }
}

/** The editor's document as the plain text a post stores. */
export function docToText(doc: JSONContent) {
  return (doc.content ?? []).flatMap(block).join('\n')
}
