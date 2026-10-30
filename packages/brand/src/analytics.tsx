'use client'

import { useEffect } from 'react'

// The project's public token (it only sends events). Cookieless: nothing is stored in the browser and the project
// counts visitors from a daily hash, so the sites need no consent banner.
const POSTHOG_TOKEN = 'phc_pMwZQ0lfoTklsPLfB1W3RD4GUDOBVN2FDVMcgKmZHSO'

// Page views, clicks and web vitals for staqly.ai and docs.staqly.ai. Off everywhere else (local dev, previews, forks),
// and loaded after hydration so it stays out of the first bundle.
export function Analytics() {
  useEffect(() => {
    const { hostname } = window.location

    if (hostname !== 'staqly.ai' && !hostname.endsWith('.staqly.ai')) {
      return
    }

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
  }, [])

  return null
}
