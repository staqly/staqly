// The free tools live in @staqly/brand: the site footer, shared with the docs, lists them too.
export { TOOLS, type Tool } from '@staqly/brand/tools'

// Character limits per platform, as Staqly checks a post's text before it goes out
// (apps/api/internal/providers/catalog.go): one per character, the way the server counts.
export type PlatformLimit = { id: string; name: string; max: number; note?: string }

export const LIMITS: PlatformLimit[] = [
  { id: 'instagram', name: 'Instagram', max: 2200, note: 'Caption; a post needs a picture or video.' },
  { id: 'tiktok', name: 'TikTok', max: 2200, note: 'Caption; a post needs a video.' },
  { id: 'youtube', name: 'YouTube', max: 5000, note: 'Description; a video and a title are needed too.' },
  { id: 'threads', name: 'Threads', max: 500 },
  { id: 'x', name: 'X', max: 280, note: 'Standard accounts; links are removed or shown as plain text, as you pick.' },
  { id: 'linkedin', name: 'LinkedIn', max: 3000 },
  { id: 'facebook', name: 'Facebook Page', max: 63206 },
  {
    id: 'pinterest',
    name: 'Pinterest',
    max: 500,
    note: 'Pin description; a picture, a title and a link are needed too.',
  },
  { id: 'bluesky', name: 'Bluesky', max: 300 },
  { id: 'mastodon', name: 'Mastodon', max: 500, note: 'The default of most servers; some allow more.' },
  { id: 'telegram', name: 'Telegram', max: 4096 },
  { id: 'discord', name: 'Discord', max: 2000 },
  { id: 'google_business', name: 'Google Business', max: 1500 },
]
