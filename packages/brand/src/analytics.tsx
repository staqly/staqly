'use client'

import { useEffect } from 'react'

// The project's public token (it only sends events). Cookieless: nothing is stored in the browser and the project
// counts visitors from a daily hash, so PostHog needs no consent banner.
const POSTHOG_TOKEN = 'phc_pMwZQ0lfoTklsPLfB1W3RD4GUDOBVN2FDVMcgKmZHSO'

// Google Analytics (GA4): the measurement id baked in at build time (NEXT_PUBLIC_GA_ID; empty = off, so another
// installation sends nothing to Google). Page views of client-side navigation come from its enhanced measurement
// (history changes).
const GA_ID = process.env.NEXT_PUBLIC_GA_ID

// The first sign of a person on the page. Until then no analytics code is fetched, so it never competes with the
// page's own loading (Lighthouse scores, which see no person, stay at 100).
const INTERACTIONS = ['pointerdown', 'pointermove', 'keydown', 'scroll', 'touchstart', 'wheel'] as const

function loadGoogleAnalytics(id: string) {
  const w = window as unknown as { dataLayer: unknown[]; gtag: (...args: unknown[]) => void }

  w.dataLayer = w.dataLayer || []
  // gtag.js reads the arguments object, not an array: the standard snippet.
  w.gtag = function gtag() {
    // eslint-disable-next-line prefer-rest-params
    w.dataLayer.push(arguments)
  }

  w.gtag('js', new Date())
  w.gtag('config', id)

  const script = document.createElement('script')

  script.async = true
  script.src = `https://www.googletagmanager.com/gtag/js?id=${encodeURIComponent(id)}`
  document.head.append(script)
}

function loadPostHog() {
  // The extensions ship in the bundle: fetched from the relay at runtime they are blocked by ad blockers by file
  // name (dead-clicks-autocapture.js) and leave errors in the console.
  void Promise.all([
    import('posthog-js/no-external'),
    import('posthog-js/dist/dead-clicks-autocapture'),
    import('posthog-js/dist/web-vitals'),
  ]).then(([{ default: posthog }]) => {
    posthog.init(POSTHOG_TOKEN, {
      // PostHog's managed reverse proxy (EU), so ad blockers that drop *.posthog.com let the events through.
      api_host: 'https://relay.staqly.ai',
      ui_host: 'https://eu.posthog.com',
      defaults: '2026-08-30',
      cookieless_mode: 'always',
      disable_external_dependency_loading: true,
      disable_session_recording: true,
      disable_surveys: true,
    })
  })
}

// Every analytics tool of staqly.ai and docs.staqly.ai: PostHog (page views, clicks, web vitals) and Google Analytics.
// Off everywhere else (local dev, previews, forks), and loaded on the first interaction so it stays out of page load.
export function Analytics() {
  useEffect(() => {
    const { hostname } = window.location

    if (hostname !== 'staqly.ai' && !hostname.endsWith('.staqly.ai')) {
      return
    }

    const start = () => {
      for (const type of INTERACTIONS) {
        window.removeEventListener(type, start)
      }

      if (GA_ID) {
        loadGoogleAnalytics(GA_ID)
      }

      loadPostHog()
    }

    for (const type of INTERACTIONS) {
      window.addEventListener(type, start, { once: true, passive: true })
    }

    return () => {
      for (const type of INTERACTIONS) {
        window.removeEventListener(type, start)
      }
    }
  }, [])

  return null
}
