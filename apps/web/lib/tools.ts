// The free tools under /tools: one entry per page (index cards, sitemap, llms.txt, the home teaser).

/** `short` is the name in the footer and menus. */
export type Tool = {
  slug: string
  title: string
  short: string
  summary: string
  icon: 'clock' | 'type' | 'link' | 'check' | 'ratio'
}

export const TOOLS: Tool[] = [
  {
    slug: 'best-time-to-post',
    title: 'Best time to post',
    short: 'Best time to post',
    summary:
      'A weekly heatmap of strong posting hours for Instagram, TikTok, YouTube, X, LinkedIn and more, in your audience’s time zone and yours.',
    icon: 'clock',
  },
  {
    slug: 'character-counter',
    title: 'Character counter for every platform',
    short: 'Character counter',
    summary:
      'Paste a caption and see at once where it fits: Instagram, TikTok, YouTube, X, Threads, LinkedIn, Bluesky and more, links handled the way X wants them.',
    icon: 'type',
  },
  {
    slug: 'aspect-ratio-checker',
    title: 'Aspect ratio checker',
    short: 'Aspect ratio checker',
    summary:
      'Drop a picture or video, or type its size, and see how it fits Instagram, TikTok, YouTube, X, LinkedIn, Facebook, Threads and Pinterest, with a live crop preview.',
    icon: 'ratio',
  },
  {
    slug: 'schedule-validator',
    title: 'Schedule validator',
    short: 'Schedule validator',
    summary:
      'Check a posting plan before it runs: posts per day for Instagram, TikTok, YouTube and more, posts too close together, quiet days, hours your audience sleeps and clock changes.',
    icon: 'check',
  },
  {
    slug: 'link-to-plain-text',
    title: 'Links to plain text',
    short: 'Links to plain text',
    summary:
      'Turn links into readable text that no platform treats as a link (example[.]com), take them out, or turn plain text back into links.',
    icon: 'link',
  },
]

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
