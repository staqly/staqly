// Colour maths for the custom swatch: hex <-> HSV, no dependency. Hex is always "#rrggbb" lowercase.

export interface HSV {
  /** 0 to 360 */
  h: number
  /** 0 to 100 */
  s: number
  /** 0 to 100 */
  v: number
}

const HEX_RE = /^#?([0-9a-f]{6}|[0-9a-f]{3})$/i

/** "#rrggbb" for a 3 or 6 digit hex (with or without "#"), or null when it is not one. */
export function normalizeHex(input: string): string | null {
  const m = HEX_RE.exec(input.trim())

  if (!m) {
    return null
  }

  const h = m[1].toLowerCase()

  return `#${h.length === 3 ? [...h].map((c) => c + c).join('') : h}`
}

export const isHex = (input: string) => normalizeHex(input) !== null

const clamp = (n: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, n))

export function hsvToHex({ h, s, v }: HSV): string {
  const hh = (((h % 360) + 360) % 360) / 60
  const sat = clamp(s, 0, 100) / 100
  const val = clamp(v, 0, 100) / 100
  const c = val * sat
  const x = c * (1 - Math.abs((hh % 2) - 1))
  const m = val - c
  const [r, g, b] =
    hh < 1 ? [c, x, 0] : hh < 2 ? [x, c, 0] : hh < 3 ? [0, c, x] : hh < 4 ? [0, x, c] : hh < 5 ? [x, 0, c] : [c, 0, x]
  const to = (n: number) =>
    Math.round((n + m) * 255)
      .toString(16)
      .padStart(2, '0')

  return `#${to(r)}${to(g)}${to(b)}`
}

export function hexToHsv(hex: string): HSV | null {
  const n = normalizeHex(hex)

  if (!n) {
    return null
  }

  const r = parseInt(n.slice(1, 3), 16) / 255
  const g = parseInt(n.slice(3, 5), 16) / 255
  const b = parseInt(n.slice(5, 7), 16) / 255
  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  const d = max - min
  let h = 0

  if (d > 0) {
    h = max === r ? ((g - b) / d) % 6 : max === g ? (b - r) / d + 2 : (r - g) / d + 4
    h = Math.round((((h * 60) % 360) + 360) % 360)
  }

  return { h, s: Math.round(max === 0 ? 0 : (d / max) * 100), v: Math.round(max * 100) }
}

/** Black or white, whichever reads better on the colour (for the check mark on a swatch). */
export function contrastText(hex: string): 'black' | 'white' {
  const n = normalizeHex(hex)

  if (!n) {
    return 'white'
  }

  const r = parseInt(n.slice(1, 3), 16)
  const g = parseInt(n.slice(3, 5), 16)
  const b = parseInt(n.slice(5, 7), 16)

  return (r * 299 + g * 587 + b * 114) / 1000 > 160 ? 'black' : 'white'
}
