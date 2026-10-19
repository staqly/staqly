// The hero demo's scenarios, shared by the server part (src/hero-demo.tsx renders their provider tiles) and the
// client loop (src/hero-demo-client.tsx).

export type Target = { id: string; channel: string; chars: string; link: string }

export type Scenario = {
  agent: string
  prompt: string
  collection: string
  schedule: string
  slot: string
  content: string
  targets: Target[]
  image?: string // a file under the app's public/ (the app serves /demo/golden-hour.svg)
}

// Use cases of the landing page (apps/web/lib/use-cases.ts), as an agent would send them. Only channels that are
// available (apps/api/internal/providers: X and your own automation, `custom`); a platform that is "soon" appears
// only behind your own automation, named the way a person would name that channel.
export const scenarios: Scenario[] = [
  {
    agent: 'claude',
    prompt: 'Write captions for this week’s reels into Reels',
    collection: 'Reels',
    schedule: 'Every day at 18:00',
    slot: 'Today 18:00',
    content: '3 things I wish I knew before my first marathon 🏃 Save this for race week. #running #marathontraining',
    targets: [
      {
        id: 'custom',
        channel: 'Instagram via n8n',
        chars: '96 chars',
        link: 'instagram.com/reel/C9...',
      },
      { id: 'x', channel: 'X', chars: '96 / 280', link: 'x.com/.../status/18...' },
    ],
  },
  {
    agent: 'n8n',
    prompt: 'Generate today’s Instagram post',
    collection: 'Instagram Avatar',
    schedule: 'Every day at 18:30',
    slot: 'Today 18:30',
    content: 'Chasing the last light of the day ☀️ #goldenhour #citylife',
    targets: [
      {
        id: 'custom',
        channel: 'Instagram via n8n',
        chars: '58 chars',
        link: 'instagram.com/p/C9x...',
      },
      { id: 'x', channel: 'X', chars: '58 / 280', link: 'x.com/.../status/18...' },
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
      { id: 'custom', channel: 'Newsletter script', chars: '96 chars', link: 'example.com/news/v1-4' },
      { id: 'x', channel: 'X', chars: '41 / 280', link: 'x.com/staqly/...' },
    ],
  },
]
