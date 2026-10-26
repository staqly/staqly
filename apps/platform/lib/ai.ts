import type { AIJob, AIOperationId, AIStatus } from './api'

// The AI tools (design editor, media library): what each operation is called on screen and who made a result.
// The server decides which operations exist (GET /v1/ai, lib/ai-status.ts); the dashboard only names them.

export const AI_OPS: Record<AIOperationId, { label: string; hint: string; run: string }> = {
  generate: { label: 'New picture', hint: 'Describe a picture to add to the page.', run: 'Make the picture' },
  edit: { label: 'Change it', hint: 'Describe the change, e.g. "make it a winter scene".', run: 'Change the photo' },
  inpaint: {
    label: 'Redraw an area',
    hint: 'Paint over an area, then describe what goes there.',
    run: 'Redraw the area',
  },
  erase: { label: 'Remove an object', hint: 'Paint over what should go; AI fills in behind it.', run: 'Remove it' },
  expand: {
    label: 'Expand',
    hint: 'AI paints new space around the photo so it fits the page. Optionally describe what goes there.',
    run: 'Expand the photo',
  },
  remove_background: {
    label: 'Cut out with AI',
    hint: 'Sends the photo to the AI service. Remove background in the photo toolbar does the same on this device, free.',
    run: 'Cut out with AI',
  },
  replace_background: {
    label: 'New background',
    hint: 'Keeps the subject and paints a new background, e.g. "on a marble table".',
    run: 'Replace the background',
  },
  upscale: {
    label: 'Upscale',
    hint: 'More pixels for print or large screens; the picture stays the same.',
    run: 'Upscale',
  },
  enhance: {
    label: 'Enhance',
    hint: 'Sharper, cleaner detail at twice the size. Optionally describe the look.',
    run: 'Enhance',
  },
  text_to_video: { label: 'New video', hint: 'Describe a short video clip.', run: 'Make the video' },
  image_to_video: { label: 'Animate', hint: 'Describe how the picture should move.', run: 'Make the video' },
}

const PROVIDERS: Record<string, string> = { fal: 'fal.ai', higgsfield: 'Higgsfield' }

export function providerName(id: string) {
  return PROVIDERS[id] ?? id
}

/** Who made it, in plain words ("Made by Higgsfield; fal.ai was not available."). */
export function madeBy(job: AIJob) {
  if (!job.provider) {
    return ''
  }

  const by = `Made by ${providerName(job.provider)}`

  return job.fallback_from ? `${by}; ${providerName(job.fallback_from)} was not available.` : `${by}.`
}

/** The month's AI allowance, when half or more of it is used ("" otherwise). */
export function allowanceNote(st: AIStatus | undefined) {
  const limit = st?.usage.limit_usd

  if (!st || limit === null || limit === undefined || limit <= 0) {
    return ''
  }

  const share = Math.min(100, Math.round((st.usage.used_usd / limit) * 100))

  return share >= 50 ? `This workspace has used ${share}% of this month's AI allowance.` : ''
}

/** Whether this month's AI allowance is used up, and when it starts again (the 1st of next month, UTC). */
export function allowanceUsedUp(st: AIStatus | undefined) {
  const limit = st?.usage.limit_usd

  if (!st || limit === null || limit === undefined || st.usage.used_usd < limit) {
    return null
  }

  const [y, m] = st.usage.period.split('-').map(Number)
  const renews = new Date(Date.UTC(y!, m!, 1))

  return { renews: renews.toLocaleDateString(undefined, { day: 'numeric', month: 'long' }) }
}

/** A key that makes a repeated send of the same request answer the same job (no secure context needed). */
export function requestKey() {
  const b = new Uint8Array(16)

  crypto.getRandomValues(b)

  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
}
