'use client'

import { Check } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { ProviderTile } from '@/lib/providers'
import { cn } from '@/lib/utils'

/** What the grid needs of a catalog entry (the API's `GET /providers` has more). */
export interface ProviderCard {
  id: string
  name: string
  status: 'available' | 'soon'
  /** oauth2 | token | none: shown as a hint under the name. */
  auth?: string
}

/**
 * The "Add channel" grid: every platform the catalog lists, in its order, as a tile with the brand mark.
 * Platforms not wired up yet are still shown (greyed, "Soon"), so people see where staqly is going.
 */
/** Platform groups in the Add channel grid, in this order; a platform not listed lands in Other. */
const GROUPS: { title: string; ids: string[] }[] = [
  { title: 'Your own', ids: ['custom'] },
  {
    title: 'Social networks',
    ids: [
      'x',
      'facebook',
      'instagram',
      'instagram_standalone',
      'threads',
      'linkedin',
      'linkedin_page',
      'bluesky',
      'mastodon',
      'pinterest',
      'tumblr',
      'mewe',
    ],
  },
  { title: 'Video and streaming', ids: ['youtube', 'tiktok', 'twitch', 'kick'] },
  {
    title: 'Communities and chat',
    ids: ['reddit', 'lemmy', 'discord', 'slack', 'telegram', 'skool', 'whop', 'farcaster', 'nostr', 'moltbook'],
  },
  { title: 'Blogs and newsletters', ids: ['wordpress', 'medium', 'devto', 'hashnode', 'listmonk'] },
  { title: 'Business and portfolio', ids: ['google_business', 'dribbble'] },
]

function groupProviders<T extends { id: string }>(providers: T[]) {
  const known = new Set(GROUPS.flatMap((g) => g.ids))
  const out = GROUPS.map((g) => ({
    title: g.title,
    items: g.ids.flatMap((id) => providers.filter((p) => p.id === id)),
  }))

  out.push({ title: 'Other', items: providers.filter((p) => !known.has(p.id)) })

  return out.filter((g) => g.items.length)
}

export function ProviderGrid({
  providers,
  onPick,
  selected,
  className,
}: {
  providers: ProviderCard[]
  onPick: (provider: ProviderCard) => void
  /** Pick several (the welcome): the chosen ones are pressed. */
  selected?: ReadonlySet<string>
  className?: string
}) {
  return (
    <div className={cn('grid gap-5', className)}>
      {groupProviders(providers).map((g) => (
        <section
          key={g.title}
          aria-labelledby={`platforms-${g.title.toLowerCase().replaceAll(' ', '-')}`}
          className="grid gap-2"
        >
          <h3 id={`platforms-${g.title.toLowerCase().replaceAll(' ', '-')}`} className="text-sm font-semibold">
            {g.title}
          </h3>
          <ul className="grid grid-cols-3 gap-2 sm:grid-cols-4" aria-label={g.title}>
            {g.items.map((p) => {
              const soon = p.status !== 'available'

              return (
                <li key={p.id}>
                  <button
                    type="button"
                    onClick={() => onPick(p)}
                    disabled={soon}
                    aria-pressed={selected ? selected.has(p.id) : undefined}
                    aria-label={soon ? `${p.name}, coming soon` : p.name}
                    data-testid={`provider-${p.id}`}
                    className={cn(
                      'relative flex h-28 w-full flex-col items-center justify-center gap-1.5 rounded-xl border bg-card px-2 pt-4 pb-2 text-center text-xs font-medium transition-colors outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
                      soon && 'cursor-default text-muted-foreground hover:bg-card',
                      selected?.has(p.id) && 'border-2 border-brand bg-brand-soft hover:bg-brand-soft',
                    )}
                  >
                    {/* The badge sits in the card's corner, clear of the mark (pt-5 leaves it room). */}
                    {soon && (
                      <Badge variant="outline" className="absolute top-1 right-1 h-4 px-1 text-[10px] font-medium">
                        Soon
                      </Badge>
                    )}
                    {selected?.has(p.id) && (
                      <Check
                        aria-hidden
                        className="absolute top-1.5 left-1.5 size-4 rounded-full bg-brand p-0.5 text-[#052817]"
                      />
                    )}
                    <ProviderTile id={p.id} name={p.name} className="size-11" />
                    <span className="line-clamp-3 w-full leading-tight" title={p.name}>
                      {p.name}
                    </span>
                  </button>
                </li>
              )
            })}
          </ul>
        </section>
      ))}
    </div>
  )
}
