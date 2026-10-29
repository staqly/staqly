import { Workflow, type LucideIcon } from 'lucide-react'
import type { CSSProperties } from 'react'
import {
  siBluesky,
  siDevdotto,
  siDiscord,
  siDribbble,
  siFacebook,
  siGoogle,
  siHashnode,
  siInstagram,
  siKick,
  siListmonk,
  siMastodon,
  siMedium,
  siPinterest,
  siReddit,
  siTelegram,
  siThreads,
  siTiktok,
  siTumblr,
  siTwitch,
  siWordpress,
  siX,
  siYoutube,
  type SimpleIcon,
} from 'simple-icons'

import { classes } from './classes'

/**
 * How each provider looks (dashboard and landing page): its brand icon (simple-icons, CC0) and colour. The list of
 * providers itself comes from the API (`GET /providers`); this only decorates it. A provider without a brand
 * icon here gets a lettered badge in its colour.
 */
interface Look {
  icon?: SimpleIcon | { title: string; path: string }
  /** A mark in several colours (Slack): one path per colour on a 24 x 24 box. */
  parts?: { d: string; color: string }[]
  /** Brand colour as #rrggbb; icons that are black get a neutral treatment in dark mode. */
  color: string
  /** The badge letters when there is no icon. */
  letters?: string
  /** A line icon instead of letters (stays readable at 16 px, where letters like </> don't). */
  glyph?: LucideIcon
  /** The mark on the round tile when the icon is itself a filled circle (Facebook's): just its letter, centred. */
  tile?: string
}

// Facebook's "f" alone, centred in a 24 x 24 box: its logo is a circle with the f cut out and resting on the
// bottom edge, which on a round tile reads as a ring around an off-centre letter.
const FACEBOOK_F =
  'M13.5 20.8v-8h2.7l.4-3.2h-3.1V7.6c0-.9.3-1.6 1.6-1.6h1.7V3.2c-.3 0-1.3-.1-2.5-.1-2.5 0-4.2 1.5-4.2 4.3v2.2H7.4v3.2h2.7v8z'

// ponytail: simple-icons dropped LinkedIn and Slack at the companies' request; their official marks are kept
// here as paths (the same 24 x 24 box), the only brand marks not taken from simple-icons.
const LINKEDIN = {
  title: 'LinkedIn',
  path: 'M20.447 20.452h-3.554v-5.569c0-1.328-.027-3.037-1.852-3.037-1.853 0-2.136 1.445-2.136 2.939v5.667H9.351V9h3.414v1.561h.046c.477-.9 1.637-1.85 3.37-1.85 3.601 0 4.267 2.37 4.267 5.455v6.286zM5.337 7.433c-1.144 0-2.063-.926-2.063-2.065 0-1.138.92-2.063 2.063-2.063 1.14 0 2.064.925 2.064 2.063 0 1.139-.925 2.065-2.064 2.065zm1.782 13.019H3.555V9h3.564v11.452zM22.225 0H1.771C.792 0 0 .774 0 1.729v20.542C0 23.227.792 24 1.771 24h20.451C23.2 24 24 23.227 24 22.271V1.729C24 .774 23.2 0 22.222 0h.003z',
}
const SLACK = [
  {
    color: '#E01E5A',
    d: 'M5.042 15.165a2.528 2.528 0 0 1-2.52 2.523A2.528 2.528 0 0 1 0 15.165a2.527 2.527 0 0 1 2.522-2.52h2.52v2.52zM6.313 15.165a2.527 2.527 0 0 1 2.521-2.52 2.527 2.527 0 0 1 2.521 2.52v6.313A2.528 2.528 0 0 1 8.834 24a2.528 2.528 0 0 1-2.521-2.522v-6.313z',
  },
  {
    color: '#36C5F0',
    d: 'M8.834 5.042a2.528 2.528 0 0 1-2.521-2.52A2.528 2.528 0 0 1 8.834 0a2.528 2.528 0 0 1 2.521 2.522v2.52H8.834zM8.834 6.313a2.528 2.528 0 0 1 2.521 2.521 2.528 2.528 0 0 1-2.521 2.521H2.522A2.528 2.528 0 0 1 0 8.834a2.528 2.528 0 0 1 2.522-2.521h6.312z',
  },
  {
    color: '#2EB67D',
    d: 'M18.956 8.834a2.528 2.528 0 0 1 2.522-2.521A2.528 2.528 0 0 1 24 8.834a2.528 2.528 0 0 1-2.522 2.521h-2.522V8.834zM17.688 8.834a2.528 2.528 0 0 1-2.523 2.521 2.527 2.527 0 0 1-2.52-2.521V2.522A2.527 2.527 0 0 1 15.165 0a2.528 2.528 0 0 1 2.523 2.522v6.312z',
  },
  {
    color: '#ECB22E',
    d: 'M15.165 18.956a2.528 2.528 0 0 1 2.523 2.522A2.528 2.528 0 0 1 15.165 24a2.527 2.527 0 0 1-2.52-2.522v-2.522h2.52zM15.165 17.688a2.527 2.527 0 0 1-2.52-2.523 2.526 2.526 0 0 1 2.52-2.52h6.313A2.527 2.527 0 0 1 24 15.165a2.528 2.528 0 0 1-2.522 2.523h-6.313z',
  },
]

const LOOKS: Record<string, Look> = {
  custom: { color: '#22c55e', glyph: Workflow },
  x: { icon: siX, color: '#000000' },
  linkedin: { icon: LINKEDIN, color: '#0A66C2' },
  linkedin_page: { icon: LINKEDIN, color: '#0A66C2' },
  reddit: { icon: siReddit, color: '#FF4500' },
  instagram: { icon: siInstagram, color: '#FF0069' },
  instagram_business: { icon: siInstagram, color: '#FF0069' },
  facebook: { icon: siFacebook, color: '#0866FF', tile: FACEBOOK_F },
  threads: { icon: siThreads, color: '#000000' },
  youtube: { icon: siYoutube, color: '#FF0000' },
  google_business: { icon: siGoogle, color: '#4285F4' },
  tiktok: { icon: siTiktok, color: '#000000' },
  pinterest: { icon: siPinterest, color: '#BD081C' },
  dribbble: { icon: siDribbble, color: '#EA4C89' },
  discord: { icon: siDiscord, color: '#5865F2' },
  discord_webhook: { icon: siDiscord, color: '#5865F2' },
  slack: { parts: SLACK, color: '#4A154B' },
  kick: { icon: siKick, color: '#53FC19' },
  twitch: { icon: siTwitch, color: '#9146FF' },
  mastodon: { icon: siMastodon, color: '#6364FF' },
  mastodon_token: { icon: siMastodon, color: '#6364FF' },
  bluesky: { icon: siBluesky, color: '#1185FE' },
  bluesky_app_password: { icon: siBluesky, color: '#1185FE' },
  telegram: { icon: siTelegram, color: '#26A5E4' },
  medium: { icon: siMedium, color: '#000000' },
  devto: { icon: siDevdotto, color: '#0A0A0A' },
  hashnode: { icon: siHashnode, color: '#2962FF' },
  wordpress: { icon: siWordpress, color: '#21759B' },
  listmonk: { icon: siListmonk, color: '#0055D4' },
  moltbook: { color: '#E11D48', letters: 'M' },
  whop: { color: '#FF6243', letters: 'W' },
  skool: { color: '#E8B31F', letters: 'sk' },
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

/** Relative luminance (0 black, 1 white) of a #rrggbb colour. */
function luminance(color: string) {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(color.slice(i, i + 2), 16) / 255

    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  })

  return 0.2126 * r! + 0.7152 * g! + 0.0722 * b!
}

/**
 * The brand colour per theme, as CSS variables for THEMED: a very light one (Kick's green) is deepened on light
 * backgrounds, a very dark one (Tumblr's slate) lifted on dark ones; the rest stay as the brand has them.
 */
function themed(color: string): CSSProperties {
  const l = luminance(color)

  return {
    '--mark': l > 0.5 ? `color-mix(in oklch, ${color} 65%, black)` : color,
    '--mark-dark': l < 0.08 ? `color-mix(in oklch, ${color} 45%, white)` : color,
  } as CSSProperties
}

const THEMED = 'text-(color:--mark) [.dark_&]:text-(color:--mark-dark)'

/**
 * The provider's brand mark, sized by the `size-*` class you pass (default 16 px). Black marks render in
 * the current text colour so they stay visible on dark backgrounds; others take their colour per theme (themed).
 */
export function ProviderIcon({ id, name, className }: { id: string; name?: string; className?: string }) {
  const look = providerLook(id)

  if (look.glyph) {
    const Glyph = look.glyph

    return (
      <Glyph
        role="img"
        aria-label={name ?? id}
        className={classes(`shrink-0 ${THEMED}`, ['size-4'], className)}
        style={themed(look.color)}
        strokeWidth={2.25}
      />
    )
  }

  if (look.parts) {
    return (
      <svg
        role="img"
        aria-label={name ?? id}
        viewBox="0 0 24 24"
        className={classes('shrink-0', ['size-4'], className)}
      >
        {look.parts.map((p) => (
          <path key={p.color} d={p.d} fill={p.color} />
        ))}
      </svg>
    )
  }

  if (!look.icon) {
    return (
      <span
        role="img"
        aria-label={name ?? id}
        className={classes(
          'inline-flex shrink-0 items-center justify-center rounded-full leading-none font-bold text-white [container-type:size]',
          ['size-4'],
          className,
        )}
        style={{ background: look.color }}
      >
        <span className="text-[55cqh]">{look.letters ?? (name ?? id).slice(0, 1).toUpperCase()}</span>
      </span>
    )
  }

  return (
    <svg
      role="img"
      aria-label={name ?? look.icon.title}
      viewBox="0 0 24 24"
      className={classes(
        isBlack(look.color) ? 'shrink-0 text-foreground' : `shrink-0 ${THEMED}`,
        ['size-4'],
        className,
      )}
      style={isBlack(look.color) ? undefined : themed(look.color)}
      fill="currentColor"
    >
      <path d={look.icon.path} />
    </svg>
  )
}

/**
 * The provider as an app icon (round unless `className` gives another radius): the "Add channel" grid, channel
 * rows, the landing page and the stacked avatars on calendar chips (when a channel has no profile picture).
 */
export function ProviderTile({ id, name, className }: { id: string; name?: string; className?: string }) {
  const look = providerLook(id)
  const label = name ?? look.icon?.title ?? id
  // Like an app icon: the brand colour fills the tile and the mark is white (black on a very light colour).
  // A multi-colour mark (Slack) keeps its colours on white.
  const background = look.parts ? '#ffffff' : id.startsWith('instagram') ? INSTAGRAM : look.color
  const ink = luminance(look.color) > 0.5 ? '#000000' : '#ffffff'
  let mark

  if (look.parts) {
    mark = <ProviderIcon id={id} className="size-[60%]" />
  } else if (look.glyph) {
    const Glyph = look.glyph

    mark = <Glyph className="size-[60%]" strokeWidth={2.25} />
  } else if (look.icon) {
    mark = (
      <svg viewBox="0 0 24 24" className="size-[56%]" fill="currentColor">
        <path d={look.tile ?? look.icon.path} />
      </svg>
    )
  } else {
    mark = (
      <span className="text-[50cqh] leading-none font-bold">{look.letters ?? label.slice(0, 1).toUpperCase()}</span>
    )
  }

  return (
    <span
      role="img"
      aria-label={label}
      className={classes(
        // A black tile (X, Threads, TikTok) would vanish on a dark page: a thin light ring outlines it there.
        `inline-flex shrink-0 items-center justify-center [container-type:size] ${isBlack(look.color) ? '[.dark_&]:ring-1 [.dark_&]:ring-white/20' : ''}`,
        ['size-9', 'rounded-full'],
        className,
      )}
      style={{ background, color: ink }}
    >
      {mark}
    </span>
  )
}

const INSTAGRAM = 'radial-gradient(circle at 30% 107%, #fdf497 0%, #fdf497 5%, #fd5949 45%, #d6249f 60%, #285aeb 90%)'
