/** One spelling per tag, for posts and channels: no leading #, lowercase, spaces become dashes. */
export function normalizeTag(v: string): string {
  return v.trim().replace(/^#/, '').toLowerCase().replace(/\s+/g, '-')
}

/**
 * The text a channel receives with the post's tags as hashtags at the end, as the API sends it
 * (providers.AppendHashtags): "client-a" becomes #clienta, a tag the text already has is left out.
 */
export function appendHashtags(text: string, tags: readonly string[]): string {
  let have = text.toLowerCase()
  const add: string[] = []

  for (const t of tags) {
    const h = '#' + t.replace(/^#/, '').replace(/[^\p{L}\p{N}_]+/gu, '')
    const low = h.toLowerCase()

    if (h === '#' || `${have} `.includes(`${low} `) || have.includes(`${low}\n`)) {
      continue
    }

    have += ` ${low}`
    add.push(h)
  }

  if (!add.length) {
    return text
  }

  return text.trim() ? `${text.replace(/[ \t\n]+$/, '')}\n\n${add.join(' ')}` : add.join(' ')
}

/**
 * Whether a channel gets the post's tags as hashtags in its text: not your own automation (it gets them apart) nor a
 * platform with tags of its own (YouTube, blogs), which gets them there (as the API does: providers.NativeTags).
 */
export function hashtagsFor(
  channel: { provider: string },
  provider?: { settings_fields?: { key: string }[] | null },
): boolean {
  return channel.provider !== 'custom' && !provider?.settings_fields?.some((f) => f.key === 'tags')
}
