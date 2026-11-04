'use client'

import { useEffect, useState } from 'react'

// Google Analytics (GA4) on staqly.ai and docs.staqly.ai, with Google's consent mode: storage is denied until the
// visitor accepts, so without a yes GA gets cookieless pings only. The choice is a first-party cookie on .staqly.ai,
// so it holds on both sites, for a year. PostHog (analytics.tsx) stays cookieless and needs no consent.

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

  return (
    <section
      role="dialog"
      aria-label="Cookies"
      aria-live="polite"
      className="fixed right-4 bottom-4 left-4 z-50 grid gap-3 rounded-xl border border-border bg-card p-4 text-sm shadow-lg sm:right-auto sm:max-w-sm"
      data-testid="cookie-banner"
    >
      <p className="text-muted-foreground">
        May we use Google Analytics cookies to learn how people find and use staqly? Nothing is stored unless you
        accept.{' '}
        <a
          href="https://docs.staqly.ai/legal/privacy#cookies"
          className="text-foreground underline underline-offset-2 hover:text-brand-strong"
        >
          Privacy
        </a>
      </p>
      <div className="flex gap-2">
        <button
          type="button"
          onClick={() => choose('granted')}
          className="min-h-10 flex-1 rounded-lg border border-border bg-foreground px-3 font-medium text-background transition-opacity hover:opacity-90"
        >
          Accept
        </button>
        <button
          type="button"
          onClick={() => choose('denied')}
          className="min-h-10 flex-1 rounded-lg border border-border bg-card px-3 font-medium text-foreground transition-colors hover:bg-muted"
        >
          Decline
        </button>
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
