import type { Channel, Media, Provider } from './api'

type Limits = Provider['limits']

/** A file's kind as a platform's media_kinds name it (a GIF is a picture unless the platform lists GIFs apart). */
function kindOf(mime: string, kinds: readonly string[]) {
  if (mime === 'image/gif' && kinds.includes('gif')) {
    return 'gif'
  }

  const kind = mime.split('/')[0]

  return kind === 'image' || kind === 'video' ? kind : 'file'
}

/** Whether a platform takes a file of this type (no media_kinds: any). */
export function takesFile(limits: Limits | undefined, mime: string) {
  const kinds = limits?.media_kinds ?? []

  return kinds.length === 0 || kinds.includes(kindOf(mime, kinds) as (typeof kinds)[number])
}

/** The files a platform takes, as the server keeps them (providers.FitMedia): of its kinds, in order, at most max_media. */
export function fitMedia<T extends { mime_type: string }>(limits: Limits | undefined, files: T[]): T[] {
  if (!limits) {
    return files
  }

  const out: T[] = []

  for (const f of files) {
    if (out.length >= limits.max_media) {
      break
    }

    if (takesFile(limits, f.mime_type)) {
      out.push(f)
    }
  }

  return out
}

/** The file picker's accept list for a platform; undefined when it takes any file. */
export function acceptFor(limits: Limits | undefined) {
  const kinds = limits?.media_kinds ?? []

  if (kinds.length === 0 || kinds.includes('file')) {
    return undefined
  }

  return kinds.map((k) => (k === 'video' ? 'video/*' : k === 'gif' ? 'image/gif' : 'image/*')).join(',')
}

const KIND_WORDS: Record<string, string> = { image: 'pictures', gif: 'GIFs', video: 'videos', file: 'files' }

/** "pictures and videos": what a platform's files may be. */
function kindsWords(limits: Limits) {
  const words = [...new Set((limits.media_kinds ?? []).map((k) => KIND_WORDS[k] ?? k))]

  return words.length > 1 ? `${words.slice(0, -1).join(', ')} and ${words.at(-1)}` : (words[0] ?? 'files')
}

/** "Takes pictures only, 1 file at most": what a platform takes, said when a file doesn't fit. */
export function takesWords(limits: Limits) {
  if (limits.max_media === 0) {
    return 'Takes no files'
  }

  const count = `${limits.max_media} file${limits.max_media === 1 ? '' : 's'} at most`

  return (limits.media_kinds ?? []).length && !(limits.media_kinds ?? []).includes('file')
    ? `Takes ${kindsWords(limits)} only, ${count}`
    : `Takes ${count}`
}

interface Part {
  content: string
  media: { mime_type: string }[]
}

/**
 * What a channel leaves out of the post, said before it goes out: files it doesn't take (the server sends the ones
 * that fit), comments it can't post, files in comments that take text only. Empty: it goes out as written. Your
 * own automation gets everything.
 */
export function channelFit(channel: Channel, provider: Provider | undefined, media: Media[], thread: Part[]): string[] {
  const limits = provider?.limits

  if (!limits || channel.provider === 'custom') {
    return []
  }

  const notes: string[] = []
  const kept = new Set(fitMedia(limits, media))
  const left = media.filter((m) => !kept.has(m))

  if (left.length) {
    notes.push(`${takesWords(limits)}: leaves out ${left.map((m) => m.original_name).join(', ')}`)
  }

  // Dribbble takes 400x300 or 800x600 only; the server fits any other picture (not a GIF) into 800x600 when sending.
  if (channel.provider === 'dribbble') {
    for (const m of kept) {
      const { width: w = 0, height: h = 0 } = m

      if (m.mime_type !== 'image/gif' && w > 0 && !(w === 400 && h === 300) && !(w === 800 && h === 600)) {
        const size = w <= 400 && h <= 300 ? '400x300' : '800x600'

        notes.push(`Takes 400x300 or 800x600 pictures: ${m.original_name} (${w}x${h}) goes out fitted into ${size}`)
      }
    }
  }

  const comments = thread.filter((p) => p.content.trim() || p.media.length)

  if (comments.length && !channel.comments) {
    notes.push(
      comments.length === 1
        ? "Posts no comments: the comment doesn't go out here"
        : `Posts no comments: the ${comments.length} comments don't go out here`,
    )
  } else if (comments.some((p) => p.media.length) && limits.comments_text_only) {
    notes.push('Comments take text only: their pictures and videos are left out')
  }

  return notes
}

const formatsOf = (p: Provider) => p.limits.image?.formats ?? []

/**
 * What a picture of this type turns into so every given platform takes it: JPEG when they all take JPEG, else PNG,
 * with the platforms that refuse it as it is. Undefined when they all take it, no type suits them all, or it is a GIF
 * (converting would stop its animation).
 */
export function pictureTarget(providers: Provider[], mime: string) {
  if (!mime.startsWith('image/') || mime === 'image/gif') {
    return undefined
  }

  const refusing = providers.filter((p) => formatsOf(p).length > 0 && !formatsOf(p).includes(mime))

  if (refusing.length === 0) {
    return undefined
  }

  const to = (['image/jpeg', 'image/png'] as const).find((t) =>
    providers.every((p) => formatsOf(p).length === 0 || formatsOf(p).includes(t)),
  )

  return to && { to, by: [...new Set(refusing.map((p) => p.name))] }
}
