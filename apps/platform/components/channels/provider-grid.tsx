'use client'

import { Check } from 'lucide-react'
import { useSyncExternalStore } from 'react'

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
  /** The platform calls back over https only: it can't be connected from a dashboard on plain http. */
  needs_https?: boolean
}

const noSubscribe = () => () => {}

/** Whether this page is on plain http (localhost, a LAN address): https-only platforms can't come back to it. */
function useInsecurePage() {
  return useSyncExternalStore(
    noSubscribe,
    () => window.location.protocol !== 'https:',
    () => false,
  )
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
      'instagram_business',
      'threads',
      'linkedin',
      'linkedin_page',
      'bluesky',
      'bluesky_app_password',
      'mastodon',
      'pinterest',
      'tumblr',
      'mewe',
    ],
  },
  { title: 'Video and streaming', ids: ['youtube', 'tiktok', 'twitch', 'kick'] },
  {
    title: 'Communities and chat',
    ids: [
      'reddit',
      'lemmy',
      'discord',
      'discord_webhook',
      'slack',
      'telegram',
      'skool',
      'whop',
      'farcaster',
      'nostr',
      'moltbook',
    ],
  },
  { title: 'Blogs and newsletters', ids: ['wordpress', 'medium', 'devto', 'hashnode', 'listmonk'] },
  { title: 'Business and portfolio', ids: ['google_business', 'dribbble'] },
]

/**
 * A fallback way in, by the platform it stands in for (Discord through a channel webhook, Bluesky with an app
 * password) shows only while this server cannot connect the platform the main way; then it takes the place of the
 * greyed tile.
 */
const FALLBACKS: Record<string, string> = { discord_webhook: 'discord', bluesky_app_password: 'bluesky' }

/** The catalog without the tiles a fallback settles: the fallback when its platform is available, else the platform. */
function withoutSettledFallbacks<T extends { id: string; status: string }>(providers: T[]) {
  const available = new Set(providers.filter((p) => p.status === 'available').map((p) => p.id))

  return providers.filter((p) => {
    const main = FALLBACKS[p.id]

    if (main) {
      return !available.has(main)
    }

    const fallback = Object.keys(FALLBACKS).find((f) => FALLBACKS[f] === p.id)

    return !fallback || available.has(p.id) || !available.has(fallback)
  })
}

function groupProviders<T extends { id: string; status: string; usable: boolean }>(providers: T[]) {
  const known = new Set(GROUPS.flatMap((g) => g.ids))
  const out = GROUPS.map((g) => ({
    title: g.title,
    items: g.ids.flatMap((id) => providers.filter((p) => p.id === id)),
  }))

  out.push({ title: 'Other', items: providers.filter((p) => !known.has(p.id)) })

  // Available platforms lead each group; the sort is stable, so the catalog order holds within each half.
  for (const g of out) {
    g.items.sort((a, b) => +!a.usable - +!b.usable)
  }

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
  const insecure = useInsecurePage()
  const cards = withoutSettledFallbacks(providers).map((p) => {
    const blocked = p.status === 'available' && !!p.needs_https && insecure

    return { ...p, blocked, usable: p.status === 'available' && !blocked }
  })

  return (
    <div className={cn('grid gap-5', className)}>
      {cards.some((p) => p.blocked) && (
        <p className="text-xs text-muted-foreground">
          Platforms marked Not available only connect over a secure (https) address. Open staqly at its https address to
          connect them.
        </p>
      )}
      {groupProviders(cards).map((g) => (
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
              const label = soon ? 'Soon' : p.blocked ? 'Not available' : ''

              return (
                <li key={p.id}>
                  <button
                    type="button"
                    onClick={() => onPick(p)}
                    disabled={!p.usable}
                    aria-pressed={selected ? selected.has(p.id) : undefined}
                    // Contains the visible text in its order (the name, then the Soon badge): speech input can name it.
                    aria-label={label ? `${p.name}, ${label.toLowerCase()}` : p.name}
                    title={p.blocked ? `${p.name} only connects over a secure (https) address` : undefined}
                    data-testid={`provider-${p.id}`}
                    className={cn(
                      'relative flex h-28 w-full flex-col items-center justify-center gap-1.5 rounded-xl border bg-card px-2 pt-4 pb-2 text-center text-xs font-medium transition-colors outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
                      !p.usable && 'cursor-default text-muted-foreground hover:bg-card',
                      selected?.has(p.id) && 'border-2 border-brand bg-brand-soft hover:bg-brand-soft',
                    )}
                  >
                    {selected?.has(p.id) && (
                      <Check
                        aria-hidden
                        className="absolute top-1.5 left-1.5 size-4 rounded-full bg-brand p-0.5 text-[#052817]"
                      />
                    )}
                    <ProviderTile id={p.id} name={p.name} className="size-11" />
                    <span className="line-clamp-3 w-full leading-tight" title={p.name}>
                      {p.name}
                    </span>{' '}
                    {/* The badge sits in the card's corner, clear of the mark (pt-5 leaves it room); after the name
                        in reading order. */}
                    {label && (
                      <Badge variant="outline" className="absolute top-1 right-1 h-4 px-1 text-[10px] font-medium">
                        {label}
                      </Badge>
                    )}
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
