/** One spelling per tag, for posts and channels: no leading #, lowercase, spaces become dashes. */
export function normalizeTag(v: string): string {
  return v.trim().replace(/^#/, '').toLowerCase().replace(/\s+/g, '-')
}

/**
 * Several tags typed or pasted at once: split on commas and before every "#" ("#gym #lift, news" is three), so
 * "client a" stays one tag ("client-a").
 */
export function splitTags(v: string): string[] {
  return v.split(/,|\s+(?=#)/)
}

/** What the API takes for a post's or a file's tag (maxTagLen in apps/api/internal/app). */
export const maxTagLength = 40

/** The tag fields' typing rules (MultiSelect `creatable`). */
export const tagInput = { normalize: normalizeTag, split: splitTags, maxLength: maxTagLength }

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
 * The post's tags a channel gets as hashtags in its text: none for your own automation (it gets them apart) nor a
 * platform with tags of its own (YouTube, blogs), which gets them there; on one that takes fewer than the post has
 * (Threads: one topic), the rest (as the API does: providers.TextTags).
 */
export function hashtagsFor(
  channel: { provider: string },
  provider: { settings_fields?: { key: string; max_items?: number; hashtag_rest?: boolean }[] | null } | undefined,
  tags: readonly string[],
): readonly string[] {
  const field = provider?.settings_fields?.find((f) => f.key === 'tags')

  if (channel.provider === 'custom') {
    return []
  }

  if (!field) {
    return tags
  }

  return field.hashtag_rest && field.max_items ? tags.slice(field.max_items) : []
}
