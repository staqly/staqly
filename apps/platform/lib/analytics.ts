import type { CaptureResult, PostHog } from 'posthog-js'
import type ReactGA from 'react-ga4'

/** What GET /v1/setup says when the server sets POSTHOG_KEY (staqly cloud). Absent: analytics are off. */
export type AnalyticsConfig = { key: string; host: string; environment: string; release: string }

let client: PostHog | undefined
let started = false

// Sign-in links, invitations, previews and OAuth callbacks carry tokens and codes in the query or the fragment.
const URL_PROPERTIES = ['$current_url', '$referrer', '$initial_current_url', '$initial_referrer'] as const

/** The URL without its query and fragment. */
export function withoutQuery(url: unknown): unknown {
  if (typeof url !== 'string') {
    return url
  }

  return url.split(/[?#]/)[0]
}

/** before_send: no query strings or fragments leave the browser, in an event's properties or a person's. */
export function scrub(event: CaptureResult | null): CaptureResult | null {
  if (!event) {
    return null
  }

  for (const props of [event.properties, event.$set, event.$set_once]) {
    if (!props) {
      continue
    }

    for (const key of URL_PROPERTIES) {
      if (key in props) {
        props[key] = withoutQuery(props[key])
      }
    }
  }

  return event
}

/**
 * Page views, web vitals and errors of the dashboard, cookieless (nothing stored in the browser, no person: a
 * visitor is a daily hash) and with no clicks (autocapture would send the text of what was clicked: post text,
 * names). Loaded on demand, once, and only when the server asks for it; the bundle loads no scripts from
 * elsewhere, so the CSP keeps script-src 'self'.
 */
export function startAnalytics(cfg: AnalyticsConfig) {
  if (started) {
    return
  }

  started = true
  void Promise.all([
    import('posthog-js/no-external'),
    import('posthog-js/dist/exception-autocapture'),
    import('posthog-js/dist/web-vitals'),
  ]).then(([{ default: posthog }]) => {
    posthog.init(cfg.key, {
      api_host: cfg.host,
      defaults: '2026-08-30',
      cookieless_mode: 'always',
      person_profiles: 'never',
      disable_external_dependency_loading: true,
      advanced_disable_flags: true,
      autocapture: false,
      capture_pageleave: false,
      capture_dead_clicks: false,
      enable_heatmaps: false,
      disable_session_recording: true,
      disable_surveys: true,
      capture_exceptions: true,
      capture_performance: { web_vitals: true, network_timing: false },
      before_send: scrub,
      loaded: (ph) => ph.register({ app: 'dashboard', environment: cfg.environment, release: cfg.release }),
    })
    client = posthog
  })
}

/** Reports an error an error boundary caught (window errors and rejections are captured by themselves). */
export function captureError(error: unknown, boundary: string) {
  client?.captureException(error, { boundary })
}

let ga: typeof ReactGA | undefined

/**
 * Google Analytics (GOOGLE_ANALYTICS_ID, staqly cloud): page views only, sent by trackPage with the path alone. The
 * location GA would read itself (query strings with codes and searches, fragments with tokens) never reaches it:
 * page_location and page_title are set from the path, and GA's own first page view is off.
 */
export function startGoogleAnalytics(measurementId: string) {
  if (ga) {
    return
  }

  void import('react-ga4').then(({ default: g }) => {
    if (!g.isInitialized) {
      g.initialize(measurementId, { gtagOptions: { send_page_view: false, page_location: safeLocation() } })
    }

    ga = g
    trackPage(window.location.pathname)
  })
}

function safeLocation(path = window.location.pathname): string {
  return window.location.origin + path
}

/** A page view of a path (no query or fragment: the router's pathname). */
export function trackPage(path: string) {
  if (!ga) {
    return
  }

  const location = safeLocation(path)

  ga.set({ page_location: location, page_title: 'staqly' })
  ga.send({ hitType: 'pageview', page: path, location, title: 'staqly' })
}
