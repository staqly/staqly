// WebMCP tools of staqly.ai (https://webmachinelearning.github.io/webmcp/): the free tools' logic and the plans, for
// an AI agent in the visitor's browser. All read-only and computed in the page; nothing is sent anywhere.
// Registered by components/webmcp.tsx; checked with plain Node by scripts/check-tools.mjs (hence the .ts imports).

import { bars, closest, crop, PLACEMENTS, ratioLabel } from './aspect-ratio.ts'
import { DAY_NAMES, PLATFORMS, scheduleSuggestion, scoreGrid } from './best-time.ts'
import { applyLinks, type LinksMode } from './links.ts'
import { CURRENCY_NOTE, PLANS, TRIAL_DAYS, TRIAL_PLAN } from './plans.ts'
import { applyStyle, STYLES, type Style } from './text-format.ts'
import { LIMITS } from './tools.ts'
import { checkTitle } from './youtube-title.ts'

export type WebMcpTool = {
  name: string
  title: string
  description: string
  inputSchema: { type: 'object'; properties: Record<string, unknown>; required?: string[] }
  annotations: { readOnlyHint: true }
  execute: (input: Record<string, unknown>) => Promise<unknown>
}

const count = (s: string) => [...s].length
const text = { type: 'string', minLength: 1 } as const

// The input is checked against inputSchema by the browser before execute runs.
const tool = <I>(t: Omit<WebMcpTool, 'annotations' | 'execute'> & { run: (input: I) => unknown }): WebMcpTool => ({
  name: t.name,
  title: t.title,
  description: t.description,
  inputSchema: t.inputSchema,
  annotations: { readOnlyHint: true },
  execute: async (input) => t.run((input ?? {}) as I),
})

export const WEBMCP_TOOLS: WebMcpTool[] = [
  tool({
    name: 'count_characters',
    title: 'Character counter',
    description:
      'Counts the characters of a social media post the way staqly does and checks it against the limit of every platform (Instagram, TikTok, YouTube, Threads, X, LinkedIn, Facebook, Pinterest, Bluesky, Mastodon, Telegram, Discord, Google Business).',
    inputSchema: {
      type: 'object',
      properties: { text: { ...text, description: 'The post text.' } },
      required: ['text'],
    },
    run: ({ text: t }: { text: string }) => {
      // staqly drops links from a post to X by default, so X counts the text without them (character-counter.tsx).
      const forX = applyLinks('remove', t).text

      return LIMITS.map((l) => {
        const n = count(l.id === 'x' ? forX : t)

        return { platform: l.name, characters: n, max: l.max, fits: n <= l.max, note: l.note }
      })
    },
  }),
  tool({
    name: 'links_to_plain_text',
    title: 'Links to plain text',
    description:
      'Turns the links in a post into plain text that platforms do not make clickable (example[.]com) or removes them, and lists the links found.',
    inputSchema: {
      type: 'object',
      properties: {
        text: { ...text, description: 'The post text.' },
        mode: { type: 'string', enum: ['defang', 'remove'], default: 'defang', description: 'Defang or remove.' },
      },
      required: ['text'],
    },
    run: ({ text: t, mode = 'defang' }: { text: string; mode?: LinksMode }) => applyLinks(mode, t),
  }),
  tool({
    name: 'format_text',
    title: 'Text formatter',
    description:
      'Styles text with Unicode look-alike letters (bold, italic, monospace...) or underline and strikethrough marks, for platforms without formatting such as LinkedIn, X, Instagram and Threads.',
    inputSchema: {
      type: 'object',
      properties: {
        text: { ...text, description: 'The text to style.' },
        style: { type: 'string', enum: STYLES.map((s) => s.id), description: 'The style to apply.' },
      },
      required: ['text', 'style'],
    },
    run: ({ text: t, style }: { text: string; style: Style }) => ({ text: applyStyle(t, style) }),
  }),
  tool({
    name: 'check_youtube_title',
    title: 'YouTube title checker',
    description:
      'Checks a YouTube video title: the 100-character limit, where search results cut it, characters YouTube refuses and other common problems.',
    inputSchema: {
      type: 'object',
      properties: { title: { type: 'string', description: 'The video title.' } },
      required: ['title'],
    },
    run: ({ title }: { title: string }) => ({ characters: count(title), checks: checkTitle(title) }),
  }),
  tool({
    name: 'best_time_to_post',
    title: 'Best time to post',
    description:
      'Strong posting hours per weekday for a platform, in the audience’s own time zone, with a suggested posting schedule. Rules of thumb from engagement studies, not the account’s own data.',
    inputSchema: {
      type: 'object',
      properties: {
        platform: { type: 'string', enum: PLATFORMS.map((p) => p.id), description: 'The platform.' },
      },
      required: ['platform'],
    },
    run: ({ platform }: { platform: string }) => {
      const p = PLATFORMS.find((x) => x.id === platform)

      if (!p) {
        throw new Error(`Unknown platform ${platform}; one of ${PLATFORMS.map((x) => x.id).join(', ')}.`)
      }

      const grid = scoreGrid(p)
      const label = ['', 'worth a try', 'good', 'strong']

      return {
        platform: p.name,
        why: p.why,
        hours: DAY_NAMES.map((day, d) => ({
          day,
          windows: grid[d]!.flatMap((s, h) => (s ? [`${String(h).padStart(2, '0')}:00 ${label[s]}`] : [])),
        })),
        suggestion: scheduleSuggestion(grid),
        tool: '/tools/best-time-to-post/',
      }
    },
  }),
  tool({
    name: 'check_aspect_ratio',
    title: 'Aspect ratio checker',
    description:
      'Checks a picture or video size against the shapes of every platform placement (Instagram feed and Stories, TikTok, YouTube and Shorts, X, LinkedIn, Facebook, Threads, Pinterest): how much a platform would crop or letterbox, and the closest common shape.',
    inputSchema: {
      type: 'object',
      properties: {
        width: { type: 'number', exclusiveMinimum: 0, description: 'Width in pixels.' },
        height: { type: 'number', exclusiveMinimum: 0, description: 'Height in pixels.' },
      },
      required: ['width', 'height'],
    },
    run: ({ width, height }: { width: number; height: number }) => {
      if (!(width > 0 && height > 0)) {
        throw new Error('Width and height must be positive numbers.')
      }

      const ratio = width / height
      const near = closest(ratio, width)

      return {
        ratio: ratioLabel(width, height),
        closest: { shape: `${near.shape.w}:${near.shape.h}`, width: near.width, height: near.height },
        placements: PLACEMENTS.map((p) => {
          const c = crop(ratio, p.w / p.h)
          const b = bars(ratio, p.w / p.h)

          return {
            placement: p.name,
            shape: `${p.w}:${p.h}`,
            fits: c.from === 'none',
            croppedShare: Math.round(c.share * 100) / 100,
            croppedFrom: c.from,
            letterboxShare: Math.round(b.share * 100) / 100,
          }
        }),
      }
    },
  }),
  tool({
    name: 'get_plans',
    title: 'staqly plans',
    description:
      'The staqly plans with their price per user and month (billed monthly or yearly), what each includes, and the free trial.',
    inputSchema: { type: 'object', properties: {} },
    run: () => ({
      currency: `The same number ${CURRENCY_NOTE}, tax included.`,
      trial: `${TRIAL_DAYS} days free on ${PLANS.find((p) => p.id === TRIAL_PLAN)?.name}.`,
      plans: PLANS.map((p) => ({
        id: p.id,
        name: p.name,
        monthly: p.month,
        yearly: p.year,
        tagline: p.tagline,
        features: p.features,
      })),
      pricing: '/pricing/',
      selfHosted: 'staqly is open source (AGPL-3.0): self-hosting it is free, https://docs.staqly.ai/quickstart.',
    }),
  }),
]
