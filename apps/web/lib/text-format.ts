// Bold, italic and other styles for platforms without formatting (LinkedIn, X, Instagram, Threads): letters and
// digits are swapped for look-alike Unicode symbols, underline and strikethrough are combining marks after each
// character. No imports, so scripts/check-tools.mjs can run it with plain Node.

export type LetterStyle =
  | 'bold'
  | 'italic'
  | 'boldItalic'
  | 'serifBold'
  | 'serifItalic'
  | 'serifBoldItalic'
  | 'script'
  | 'mono'
export type MarkStyle = 'underline' | 'strike'
export type Style = LetterStyle | MarkStyle

// First code point of A, a and 0 in each style (Unicode's Mathematical Alphanumeric Symbols); no digit start means
// the style has no digits of its own and keeps plain ones.
const LETTERS: Record<LetterStyle, { upper: number; lower: number; digit?: number }> = {
  bold: { upper: 0x1d5d4, lower: 0x1d5ee, digit: 0x1d7ec },
  italic: { upper: 0x1d608, lower: 0x1d622 },
  boldItalic: { upper: 0x1d63c, lower: 0x1d656 },
  serifBold: { upper: 0x1d400, lower: 0x1d41a, digit: 0x1d7ce },
  serifItalic: { upper: 0x1d434, lower: 0x1d44e },
  serifBoldItalic: { upper: 0x1d468, lower: 0x1d482 },
  script: { upper: 0x1d4d0, lower: 0x1d4ea },
  mono: { upper: 0x1d670, lower: 0x1d68a, digit: 0x1d7f6 },
}

// Letters Unicode had encoded before the block: their place in it is empty and the older symbol stands in.
const HOLES: Partial<Record<LetterStyle, Record<string, number>>> = {
  serifItalic: { h: 0x210e },
}

const MARKS: Record<MarkStyle, string> = { underline: '̲', strike: '̶' }

export const STYLES: { id: Style; label: string; sample: string }[] = [
  { id: 'bold', label: 'Bold', sample: 'Bold' },
  { id: 'italic', label: 'Italic', sample: 'Italic' },
  { id: 'boldItalic', label: 'Bold italic', sample: 'Bold italic' },
  { id: 'underline', label: 'Underline', sample: 'Underline' },
  { id: 'strike', label: 'Strikethrough', sample: 'Strike' },
  { id: 'serifBold', label: 'Serif bold', sample: 'Serif' },
  { id: 'serifItalic', label: 'Serif italic', sample: 'Serif' },
  { id: 'serifBoldItalic', label: 'Serif bold italic', sample: 'Serif' },
  { id: 'script', label: 'Script', sample: 'Script' },
  { id: 'mono', label: 'Monospace', sample: 'Mono' },
]

const isMark = (s: Style): s is MarkStyle => s in MARKS

function forward(style: LetterStyle, ch: string): string {
  const set = LETTERS[style]
  const hole = HOLES[style]?.[ch]

  if (hole) {
    return String.fromCodePoint(hole)
  }

  const c = ch.codePointAt(0)!

  if (c >= 65 && c <= 90) {
    return String.fromCodePoint(set.upper + c - 65)
  }

  if (c >= 97 && c <= 122) {
    return String.fromCodePoint(set.lower + c - 97)
  }

  if (set.digit && c >= 48 && c <= 57) {
    return String.fromCodePoint(set.digit + c - 48)
  }

  return ch
}

let back: Map<string, { plain: string; style: LetterStyle }> | undefined

/** Every styled letter and digit, mapped to its plain character and its style. */
function reverse() {
  if (!back) {
    back = new Map()

    const plain = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'

    for (const style of Object.keys(LETTERS) as LetterStyle[]) {
      for (const ch of plain) {
        const styled = forward(style, ch)

        if (styled !== ch) {
          back.set(styled, { plain: ch, style })
        }
      }
    }
  }

  return back
}

/** Plain text again: styled letters and digits back to ASCII, underline and strikethrough marks dropped. */
export function toPlain(text: string) {
  const map = reverse()

  return [...text]
    .filter((ch) => ch !== MARKS.underline && ch !== MARKS.strike)
    .map((ch) => map.get(ch)?.plain ?? ch)
    .join('')
}

/** Whether every letter and digit (letter styles) or every visible character (marks) in `text` already has `style`. */
export function hasStyle(text: string, style: Style) {
  const chars = [...text]

  if (isMark(style)) {
    const base = chars.filter((ch) => ch !== MARKS.underline && ch !== MARKS.strike && ch.trim())

    if (!base.length) {
      return false
    }

    // Every visible character is followed, maybe after another mark, by this one.
    return chars.every((ch, i) => {
      if (ch === MARKS.underline || ch === MARKS.strike || !ch.trim()) {
        return true
      }

      for (let j = i + 1; j < chars.length && (chars[j] === MARKS.underline || chars[j] === MARKS.strike); j++) {
        if (chars[j] === MARKS[style]) {
          return true
        }
      }

      return false
    })
  }

  const map = reverse()
  const styled = chars.filter((ch) => map.has(ch) || /[A-Za-z0-9]/.test(ch))

  return (
    styled.length > 0 && styled.every((ch) => map.get(ch)?.style === style || (!LETTERS[style].digit && /\d/.test(ch)))
  )
}

/** `text` in `style`. A letter style replaces any other letter style; marks add to it. */
export function applyStyle(text: string, style: Style) {
  if (isMark(style)) {
    const mark = MARKS[style]

    return [...text]
      .filter((ch) => ch !== mark)
      .map((ch) => (ch.trim() && ch !== MARKS.underline && ch !== MARKS.strike ? ch + mark : ch))
      .join('')
  }

  const map = reverse()

  return [...text].map((ch) => forward(style, map.get(ch)?.plain ?? ch)).join('')
}

/** `style` taken off `text`, other styles kept. */
export function removeStyle(text: string, style: Style) {
  if (isMark(style)) {
    return text.replaceAll(MARKS[style], '')
  }

  const map = reverse()

  return [...text].map((ch) => (map.get(ch)?.style === style ? map.get(ch)!.plain : ch)).join('')
}

/**
 * Turn `style` on or off for text[start, end) (UTF-16 offsets, as a textarea's selection gives them); with nothing
 * selected, for the whole text. Returns the new text and the selection around the changed part.
 */
export function toggleRange(text: string, start: number, end: number, style: Style) {
  const [from, to] = start === end ? [0, text.length] : [Math.min(start, end), Math.max(start, end)]
  const part = text.slice(from, to)
  const changed = hasStyle(part, style) ? removeStyle(part, style) : applyStyle(part, style)

  return { text: text.slice(0, from) + changed + text.slice(to), start: from, end: from + changed.length }
}

const BULLET = /^(?:[•▪◦‣–-]|\d+[.)])\s+/

/** Lines of `text` as a list: bullets or numbers in front of every line with text, or taken off when all have one. */
export function toggleList(text: string, kind: 'bullet' | 'number') {
  const lines = text.split('\n')
  const filled = lines.filter((l) => l.trim())
  const isList = filled.length > 0 && filled.every((l) => (kind === 'bullet' ? /^•\s/ : /^\d+[.)]\s/).test(l))

  if (isList) {
    return lines.map((l) => l.replace(BULLET, '')).join('\n')
  }

  let n = 0

  return lines
    .map((l) => {
      if (!l.trim()) {
        return l
      }

      n++

      return `${kind === 'bullet' ? '•' : `${n}.`} ${l.replace(BULLET, '')}`
    })
    .join('\n')
}

/** Characters, counted the way people see them: a styled letter or an emoji is one. Marks don't count. */
export function visibleLength(text: string) {
  return [...text].filter((ch) => ch !== MARKS.underline && ch !== MARKS.strike).length
}

/**
 * The opening a feed shows before it folds the post behind "...see more": at most `maxChars` characters and
 * `maxLines` lines, cut at a word break. LinkedIn folds after about three lines on a computer.
 */
export function foldPreview(text: string, maxChars: number, maxLines: number) {
  const lines = text.split('\n')
  let shown = lines.slice(0, maxLines).join('\n')
  let cut = lines.length > maxLines
  const chars = [...shown]

  if (chars.length > maxChars) {
    shown = chars.slice(0, maxChars).join('')

    const space = shown.search(/\s\S*$/)

    if (space > maxChars / 2) {
      shown = shown.slice(0, space)
    }

    cut = true
  }

  return { shown: cut ? shown.trimEnd() : shown, cut }
}
