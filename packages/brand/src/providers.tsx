import { cn } from 'cn'
import { Workflow, type LucideIcon } from 'lucide-react'
import {
  siBluesky,
  siDevdotto,
  siDiscord,
  siDribbble,
  siFacebook,
  siFarcaster,
  siGoogle,
  siHashnode,
  siInstagram,
  siKick,
  siLemmy,
  siListmonk,
  siMastodon,
  siMedium,
  siMewe,
  siPinterest,
  siReddit,
  siTelegram,
  siThreads,
  siTiktok,
  siTumblr,
  siTwitch,
  siVk,
  siWordpress,
  siX,
  siYoutube,
  type SimpleIcon,
} from 'simple-icons'

/**
 * How each provider looks (dashboard and landing page): its brand icon (simple-icons, CC0) and colour. The list of
 * providers itself comes from the API (`GET /providers`); this only decorates it. A provider without a brand
 * icon here gets a lettered badge in its colour.
 */
interface Look {
  icon?: SimpleIcon
  /** Brand colour as #rrggbb; icons that are black get a neutral treatment in dark mode. */
  color: string
  /** The badge letters when there is no icon. */
  letters?: string
  /** A line icon instead of letters (stays readable at 16 px, where letters like </> don't). */
  glyph?: LucideIcon
}

const LOOKS: Record<string, Look> = {
  custom: { color: '#22c55e', glyph: Workflow },
  x: { icon: siX, color: '#000000' },
  linkedin: { color: '#0A66C2', letters: 'in' },
  linkedin_page: { color: '#0A66C2', letters: 'in' },
  reddit: { icon: siReddit, color: '#FF4500' },
  instagram: { icon: siInstagram, color: '#FF0069' },
  instagram_standalone: { icon: siInstagram, color: '#FF0069' },
  facebook: { icon: siFacebook, color: '#0866FF' },
  threads: { icon: siThreads, color: '#000000' },
  youtube: { icon: siYoutube, color: '#FF0000' },
  google_business: { icon: siGoogle, color: '#4285F4' },
  tiktok: { icon: siTiktok, color: '#000000' },
  pinterest: { icon: siPinterest, color: '#BD081C' },
  dribbble: { icon: siDribbble, color: '#EA4C89' },
  discord: { icon: siDiscord, color: '#5865F2' },
  slack: { color: '#4A154B', letters: '#' },
  kick: { icon: siKick, color: '#53FC19' },
  twitch: { icon: siTwitch, color: '#9146FF' },
  mastodon: { icon: siMastodon, color: '#6364FF' },
  bluesky: { icon: siBluesky, color: '#1185FE' },
  lemmy: { icon: siLemmy, color: '#000000' },
  farcaster: { icon: siFarcaster, color: '#855DCD' },
  telegram: { icon: siTelegram, color: '#26A5E4' },
  nostr: { color: '#8E30EB', letters: 'N' },
  vk: { icon: siVk, color: '#0077FF' },
  medium: { icon: siMedium, color: '#000000' },
  devto: { icon: siDevdotto, color: '#0A0A0A' },
  hashnode: { icon: siHashnode, color: '#2962FF' },
  wordpress: { icon: siWordpress, color: '#21759B' },
  listmonk: { icon: siListmonk, color: '#0055D4' },
  moltbook: { color: '#E11D48', letters: 'M' },
  whop: { color: '#FF6243', letters: 'W' },
  skool: { color: '#E8B31F', letters: 'sk' },
  mewe: { icon: siMewe, color: '#17377F' },
  tumblr: { icon: siTumblr, color: '#36465D' },
}

const FALLBACK: Look = { color: '#64748b' }

export function providerLook(id: string): Look {
  return LOOKS[id] ?? FALLBACK
}

/** Black brand marks (X, Threads, TikTok, Medium...) need to flip in dark mode; everything else keeps its colour. */
function isBlack(color: string) {
  return /^#0[0-9a-f]0[0-9a-f]0[0-9a-f]$/i.test(color) || color === '#000000'
}

/**
 * The provider's brand mark, sized by the `size-*` class you pass (default 16 px). Black marks render in
 * the current text colour so they stay visible on dark backgrounds.
 */
export function ProviderIcon({ id, name, className }: { id: string; name?: string; className?: string }) {
  const look = providerLook(id)

  if (look.glyph) {
    const Glyph = look.glyph

    return (
      <Glyph
        role="img"
        aria-label={name ?? id}
        className={cn('size-4 shrink-0', className)}
        style={{ color: look.color }}
        strokeWidth={2.25}
      />
    )
  }

  if (!look.icon) {
    return (
      <span
        role="img"
        aria-label={name ?? id}
        className={cn(
          'inline-flex size-4 shrink-0 items-center justify-center rounded-full text-[0.55em] leading-none font-bold text-white',
          className,
        )}
        style={{ background: look.color }}
      >
        {look.letters ?? (name ?? id).slice(0, 1).toUpperCase()}
      </span>
    )
  }

  return (
    <svg
      role="img"
      aria-label={name ?? look.icon.title}
      viewBox="0 0 24 24"
      className={cn('size-4 shrink-0', className)}
      style={isBlack(look.color) ? undefined : { color: look.color }}
      fill="currentColor"
    >
      <path d={look.icon.path} />
    </svg>
  )
}

/**
 * A round tile with the mark on a tinted background: the "Add channel" grid, channel rows and the stacked
 * avatars on calendar chips (when a channel has no profile picture).
 */
export function ProviderTile({ id, name, className }: { id: string; name?: string; className?: string }) {
  const look = providerLook(id)

  return (
    <span
      className={cn('inline-flex size-9 shrink-0 items-center justify-center rounded-full', className)}
      style={{ background: `color-mix(in oklch, ${look.color} 18%, var(--muted))` }}
    >
      <ProviderIcon id={id} name={name} className="size-[55%]" />
    </span>
  )
}
