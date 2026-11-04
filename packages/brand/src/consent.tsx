'use client'

import { ChevronDown, Cookie, X } from 'lucide-react'
import { useEffect, useState } from 'react'

// The cookie question of staqly.ai and docs.staqly.ai, and the tools that wait for its answer. One answer, a
// first-party cookie on .staqly.ai, holds on both sites for a year. Google Analytics (GA4) loads in Google's consent
// mode: storage is denied until the visitor accepts, so without a yes it gets cookieless pings only. PostHog
// (analytics.tsx) stays cookieless and needs no consent.

/** What the banner lists under Details: add or drop a tool here (and in the privacy policy) when the sites change. */
export const CONSENT_SERVICES = [
  { name: 'Google Analytics', purpose: 'Counts visits and how you found us, with cookies kept up to two years.' },
]

const GA_ID = 'G-JQGZS84PME'
const CHOICE = 'staqly_consent'
const OPEN = 'staqly:cookie-settings'
const YEAR = 365 * 24 * 60 * 60

type Choice = 'granted' | 'denied'

declare global {
  interface Window {
    dataLayer?: unknown[]
    gtag?: (...args: unknown[]) => void
  }
}

function onOurSites(): boolean {
  const { hostname } = window.location

  return hostname === 'staqly.ai' || hostname === 'www.staqly.ai' || hostname === 'docs.staqly.ai'
}

function readChoice(): Choice | null {
  const v = document.cookie.match(/(?:^|; )staqly_consent=(granted|denied)/)?.[1]

  return (v as Choice | undefined) ?? null
}

function saveChoice(c: Choice) {
  document.cookie = `${CHOICE}=${c}; domain=.staqly.ai; path=/; max-age=${YEAR}; SameSite=Lax; Secure`
}

// A visitor who takes their yes back: GA's own cookies go too.
function dropGACookies() {
  for (const part of document.cookie.split('; ')) {
    const name = part.slice(0, part.indexOf('='))

    if (name === '_ga' || name.startsWith('_ga_')) {
      document.cookie = `${name}=; domain=.staqly.ai; path=/; max-age=0`
    }
  }
}

function consent(c: Choice) {
  window.gtag?.('consent', 'update', { analytics_storage: c })
}

/** Loads Google Analytics with consent mode and asks for consent once; Cookie settings asks again. */
export function GoogleTag() {
  const [asking, setAsking] = useState(false)

  useEffect(() => {
    if (!onOurSites()) {
      return
    }

    const choice = readChoice()

    const dataLayer = (window.dataLayer ??= [])

    window.gtag = function gtag() {
      // gtag.js reads the arguments object itself, not an array.
      // eslint-disable-next-line prefer-rest-params
      dataLayer.push(arguments)
    }

    // Ads storage stays denied: staqly runs no ads through Google.
    window.gtag('consent', 'default', {
      analytics_storage: choice ?? 'denied',
      ad_storage: 'denied',
      ad_user_data: 'denied',
      ad_personalization: 'denied',
    })
    window.gtag('js', new Date())
    window.gtag('config', GA_ID)

    const s = document.createElement('script')

    s.async = true
    s.src = `https://www.googletagmanager.com/gtag/js?id=${GA_ID}`
    document.head.appendChild(s)

    const open = () => setAsking(true)

    window.addEventListener(OPEN, open)
    // Asked after paint, so the banner never holds up the page.
    const t = choice ? undefined : window.setTimeout(open, 0)

    return () => {
      window.removeEventListener(OPEN, open)
      window.clearTimeout(t)
    }
  }, [])

  if (!asking) {
    return null
  }

  const choose = (c: Choice) => {
    saveChoice(c)
    consent(c)

    if (c === 'denied') {
      dropGACookies()
    }

    setAsking(false)
  }

  return <ConsentBanner onChoose={choose} />
}

/** The banner itself: a short question, Details with each tool, and two answers of the same weight. */
export function ConsentBanner({ onChoose }: { onChoose: (c: Choice) => void }) {
  const [details, setDetails] = useState(false)

  return (
    <section
      role="dialog"
      aria-labelledby="consent-title"
      aria-describedby="consent-text"
      className="fixed inset-x-3 bottom-3 z-50 overflow-hidden rounded-2xl border border-border bg-card/85 text-sm shadow-[0_24px_64px_-24px_rgb(0_0_0/0.65)] backdrop-blur-xl motion-safe:animate-[sq-consent-in_420ms_cubic-bezier(0.2,0.8,0.2,1)] sm:inset-x-auto sm:bottom-5 sm:left-5 sm:w-[24rem]"
      data-testid="cookie-banner"
    >
      <div aria-hidden className="h-px bg-(image:--brand-gradient)" />
      <div className="grid gap-4 p-5">
        <div className="flex items-start gap-3">
          <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-brand-soft text-brand">
            <Cookie className="size-4.5" aria-hidden />
          </span>
          <div className="grid min-w-0 flex-1 gap-1">
            <h2 id="consent-title" className="font-semibold text-foreground">
              A few cookies, your call
            </h2>
            <p id="consent-text" className="leading-relaxed text-muted-foreground">
              We&apos;d like to measure how people find and use staqly. Nothing is stored in your browser until you say
              yes, and you can change your mind any time under Cookie settings.
            </p>
          </div>
          <button
            type="button"
            onClick={() => onChoose('denied')}
            aria-label="Close and decline"
            className="-mt-1 -mr-1 grid size-8 shrink-0 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            <X className="size-4" aria-hidden />
          </button>
        </div>
        <div className="grid gap-2">
          <button
            type="button"
            onClick={() => setDetails((d) => !d)}
            aria-expanded={details}
            aria-controls="consent-details"
            className="inline-flex w-fit items-center gap-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
          >
            Details
            <ChevronDown className={`size-3.5 transition-transform ${details ? 'rotate-180' : ''}`} aria-hidden />
          </button>
          {details && (
            <ul
              id="consent-details"
              className="grid gap-2 rounded-xl border border-border bg-background/60 p-3 text-xs"
            >
              {CONSENT_SERVICES.map((svc) => (
                <li key={svc.name} className="grid gap-0.5">
                  <span className="font-medium text-foreground">{svc.name}</span>
                  <span className="text-muted-foreground">{svc.purpose}</span>
                </li>
              ))}
              <li>
                <a
                  href="https://docs.staqly.ai/legal/privacy#cookies"
                  className="text-foreground underline underline-offset-2 hover:text-brand-strong"
                >
                  Privacy policy
                </a>
              </li>
            </ul>
          )}
        </div>
        <div className="grid grid-cols-2 gap-2">
          <button
            type="button"
            onClick={() => onChoose('denied')}
            className="h-10 rounded-xl border border-border bg-card font-semibold text-foreground transition-colors hover:bg-accent"
          >
            No thanks
          </button>
          <button
            type="button"
            onClick={() => onChoose('granted')}
            className="sq-rainbow h-10 rounded-xl font-semibold shadow-[0_8px_24px_-8px_color-mix(in_oklch,var(--brand)_70%,transparent)] transition hover:brightness-110"
          >
            Accept
          </button>
        </div>
      </div>
    </section>
  )
}

/** Reopens the cookie question (the footer's Cookie settings). */
export function CookieSettings({ className }: { className?: string }) {
  return (
    <button type="button" className={className} onClick={() => window.dispatchEvent(new Event(OPEN))}>
      Cookie settings
    </button>
  )
}
