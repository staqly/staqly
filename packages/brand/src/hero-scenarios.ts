// The hero demo's scenarios, shared by the server part (src/hero-demo.tsx renders their provider tiles) and the
// client loop (src/hero-demo-client.tsx).

// `id` picks the platform's mark (src/providers.tsx); `via` names the automation that posts there.
export type Target = { id: string; channel: string; via?: string; chars: string; link: string }

export type Scenario = {
  agent: string
  prompt: string
  collection: string
  schedule: string
  slot: string
  content: string
  targets: Target[]
  image?: string // a file under the app's public/ (both apps serve /demo/*.svg)
}

// Every scenario goes to three channels, so the card keeps one height as it loops.
// Use cases of the landing page (apps/web/lib/use-cases.ts), as an agent would send them. staqly publishes to the
// platforms in its catalog itself (apps/api/internal/providers); `via` marks a channel on your own automation
// (`custom`), and says which one ("via script").
export const scenarios: Scenario[] = [
  {
    agent: 'claude',
    prompt: 'Write captions for this week’s reels into Reels',
    collection: 'Reels',
    schedule: 'Every day at 18:00',
    slot: 'Today 18:00',
    content: '3 things I wish I knew before my first marathon 🏃 Save this for race week. #running #marathontraining',
    targets: [
      { id: 'instagram', channel: 'Instagram', chars: '96 / 2,200', link: 'instagram.com/reel/C9...' },
      { id: 'tiktok', channel: 'TikTok', chars: '96 / 2,200', link: 'tiktok.com/@.../video/73...' },
      { id: 'x', channel: 'X', chars: '96 / 280', link: 'x.com/.../status/18...' },
    ],
    image: '/demo/race-day.svg',
  },
  {
    agent: 'n8n',
    prompt: 'Generate today’s Instagram post',
    collection: 'Instagram Avatar',
    schedule: 'Every day at 18:30',
    slot: 'Today 18:30',
    content: 'Chasing the last light of the day ☀️ #goldenhour #citylife',
    targets: [
      { id: 'instagram', channel: 'Instagram', chars: '58 / 2,200', link: 'instagram.com/p/C9x...' },
      { id: 'threads', channel: 'Threads', chars: '58 / 500', link: 'threads.net/@.../post/C9...' },
      { id: 'facebook', channel: 'Facebook', chars: '58 / 63,206', link: 'facebook.com/.../posts/pf...' },
    ],
    image: '/demo/golden-hour.svg',
  },
  {
    agent: 'codex',
    prompt: 'Announce v1.4, keep the X version short',
    collection: 'Product updates',
    schedule: 'Once',
    slot: 'Thu 10:00',
    content: 'v1.4 is out: schedules per collection, a calendar you can drag, and alerts when a post fails.',
    targets: [
      { id: 'x', channel: 'X', chars: '41 / 280', link: 'x.com/staqly/...' },
      { id: 'linkedin', channel: 'LinkedIn', chars: '96 / 3,000', link: 'linkedin.com/feed/update/...' },
      { id: 'custom', channel: 'Newsletter', via: 'script', chars: '96 chars', link: 'example.com/news/v1-4' },
    ],
  },
  {
    agent: 'cron',
    prompt: 'Post today’s special from the menu sheet',
    collection: 'Daily specials',
    schedule: 'Weekdays at 07:30',
    slot: 'Tomorrow 07:30',
    content: 'Fresh out of the oven: cardamom buns, today only 🥐 Come early, they go fast!',
    targets: [
      { id: 'facebook', channel: 'Facebook', chars: '78 / 63,206', link: 'facebook.com/.../posts/pf...' },
      {
        id: 'google_business',
        channel: 'Google Business',
        chars: '78 / 1,500',
        link: 'g.page/.../posts/...',
      },
      { id: 'instagram', channel: 'Instagram', chars: '78 / 2,200', link: 'instagram.com/p/C9b...' },
    ],
    image: '/demo/bakery.svg',
  },
]
