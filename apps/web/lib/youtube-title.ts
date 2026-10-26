// What a YouTube title gets checked for before upload: YouTube's own rules (100 characters, no angle brackets) and
// the habits that cost clicks. No imports, so scripts/check-tools.mjs can run it with plain Node.

export type Level = 'ok' | 'warn' | 'error'
export type TitleCheck = { level: Level; title: string; detail: string }

/** YouTube refuses a longer title. */
export const MAX_TITLE = 100

/** About where search results on a computer cut a title; phones and suggestions cut sooner. */
export const SEARCH_CUT = 70

/** About what every placement shows, the phone's home feed included: the words that have to carry the click. */
export const SAFE_CUT = 50

/** Characters as YouTube counts them: an emoji is one. */
export const titleLength = (s: string) => [...s].length

/** The first `n` characters, cut back to a word break when one is near, with "..." when anything was left out. */
export function cutTitle(title: string, n: number) {
  const chars = [...title]

  if (chars.length <= n) {
    return title
  }

  const head = chars.slice(0, n).join('')
  const space = head.search(/\s\S*$/)

  return `${(space > n * 0.6 ? head.slice(0, space) : head).trimEnd()}...`
}

export function checkTitle(title: string): TitleCheck[] {
  const checks: TitleCheck[] = []
  const n = titleLength(title)
  const t = title.trim()

  if (!t) {
    return [{ level: 'error', title: 'A title is needed', detail: 'YouTube won’t publish a video without one.' }]
  }

  if (n > MAX_TITLE) {
    checks.push({
      level: 'error',
      title: `${n - MAX_TITLE} over the limit`,
      detail: `YouTube takes at most ${MAX_TITLE} characters.`,
    })
  } else if (n > SEARCH_CUT) {
    checks.push({
      level: 'warn',
      title: 'Search will cut it short',
      detail: `Results on a computer show about the first ${SEARCH_CUT} characters; put the words that matter first.`,
    })
  } else {
    checks.push({ level: 'ok', title: 'A good length', detail: `${n} of ${MAX_TITLE} characters.` })
  }

  if (/[<>]/.test(title)) {
    checks.push({ level: 'error', title: 'Angle brackets', detail: 'YouTube doesn’t accept < or > in a title.' })
  }

  if (title !== t || /\s{2,}/.test(title)) {
    checks.push({
      level: 'warn',
      title: 'Extra spaces',
      detail: 'Spaces at either end or two in a row; YouTube may drop them and they shift your count.',
    })
  }

  const letters = t.match(/\p{L}/gu) ?? []
  const upper = t.match(/\p{Lu}/gu) ?? []

  if (letters.length >= 12 && upper.length / letters.length > 0.6) {
    checks.push({
      level: 'warn',
      title: 'Mostly capitals',
      detail: 'A title in capitals reads as shouting and is harder to scan; keep capitals for a word or two.',
    })
  }

  if (/([!?])\1{2,}/.test(t)) {
    checks.push({ level: 'warn', title: 'Repeated punctuation', detail: 'One ! or ? says as much as three.' })
  }

  const emoji = t.match(/\p{Extended_Pictographic}/gu)?.length ?? 0

  if (emoji > 2) {
    checks.push({
      level: 'warn',
      title: `${emoji} emoji`,
      detail: 'One or two can help a title stand out; more crowd out the words people search for.',
    })
  }

  const tags = t.match(/(^|\s)#[\p{L}\p{N}_]+/gu)?.length ?? 0

  if (tags > 0) {
    checks.push({
      level: 'warn',
      title: tags === 1 ? 'A hashtag in the title' : `${tags} hashtags in the title`,
      detail: 'They take room from the words people read; the description is the usual place for them.',
    })
  }

  return checks
}
