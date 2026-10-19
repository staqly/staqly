/** Length in characters (Unicode code points), as the API counts limits; String.length counts UTF-16 units. */
export function charCount(s: string) {
  return Array.from(s).length
}

/** `s` cut to at most `max` characters (code points), never splitting an emoji. */
export function clampChars(s: string, max: number) {
  return s.length <= max ? s : Array.from(s).slice(0, max).join('')
}
