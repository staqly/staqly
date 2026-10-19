import { chromium, type Browser, type BrowserContext } from 'playwright'
// One suite per mode: every route at every width fails on sideways page overflow, an element that
// widens its parent past the screen, overlapping controls, touch targets under 40 px on touch screens
// content stuck under the phone tab bar and, on desktops, side-by-side controls of different heights. Ellipsised text without a title is only reported.
import { afterAll, beforeAll, describe, expect, inject, test } from 'vitest'

import { layoutReport, type LayoutReport, type Offender } from './checks.js'
import { routes } from './routes.js'

/** Phones, tablets portrait and landscape, laptops, desktops; a landscape phone; touch below 1024 px. */
export const VIEWPORTS = [
  { width: 320, height: 640 },
  { width: 390, height: 844 },
  { width: 844, height: 390, touch: true },
  { width: 768, height: 1024 },
  { width: 1024, height: 768 },
  { width: 1280, height: 800 },
  { width: 1920, height: 1080 },
]

const fmt = (kind: string, list: Offender[]) =>
  list.map((o) => `${kind}: ${o.selector} "${o.text}" ${JSON.stringify(o.rect)} ${o.detail ?? ''}`)

export function problems(r: LayoutReport): string[] {
  return [
    ...(r.overflowX > 0 ? [`page scrolls sideways by ${r.overflowX}px`] : []),
    ...fmt('wider than its parent', r.culprits),
    ...fmt('scrolls sideways', r.sideways),
    ...fmt('overlaps', r.overlaps),
    ...fmt('touch target', r.smallTargets),
    ...fmt('under a fixed bar', r.covered),
    ...fmt('control height', r.mismatched),
  ]
}

export function responsiveSuite(mode: 'local' | 'cloud') {
  const target = inject(mode === 'local' ? 'uiLocal' : 'uiCloud')
  let browser: Browser

  beforeAll(async () => {
    browser = await chromium.launch()
  })

  afterAll(async () => {
    await browser?.close()
  })

  for (const vp of VIEWPORTS) {
    const touch = vp.touch ?? vp.width < 1024

    describe(`${mode} ${vp.width}x${vp.height}${touch ? ' touch' : ''}`, () => {
      let signedIn: BrowserContext
      let signedOut: BrowserContext

      beforeAll(async () => {
        const opts = {
          viewport: { width: vp.width, height: vp.height },
          hasTouch: touch,
          isMobile: touch,
          deviceScaleFactor: touch ? 2 : 1,
          reducedMotion: 'reduce' as const,
          // phones in dark mode, the rest light: both themes are covered at little cost
          colorScheme: vp.width < 768 ? ('dark' as const) : ('light' as const),
        }

        signedIn = await browser.newContext(opts)
        signedOut = await browser.newContext(opts)

        if (target.key) {
          const res = await signedIn.request.post(`${target.dashboard}/api/v1/sessions/api-key`, {
            data: { api_key: target.key },
          })

          if (!res.ok()) {
            throw new Error(`sign in with the API key: ${res.status()} ${await res.text()}`)
          }
        }
      })

      afterAll(async () => {
        await signedIn?.close()
        await signedOut?.close()
      })

      const list = routes(target.data).filter(
        (r) => (!r.cloudOnly || mode === 'cloud') && (!r.public || mode === 'cloud') && (!r.when || r.when(vp.width)),
      )

      for (const r of list) {
        test(`${r.name}`, async () => {
          const page = await (r.public ? signedOut : signedIn).newPage()
          const errors: string[] = []

          page.on('pageerror', (e) => errors.push(e.message))

          try {
            await page.goto(target.dashboard + r.path, { waitUntil: 'load' })
            await page.waitForFunction(
              () => document.querySelector('h1, form, [data-testid]') && !document.querySelector('.animate-pulse'),
              undefined,
              { timeout: 15_000, polling: 100 },
            )
            await page.waitForTimeout(300)
            await r.open?.(page)
            const top = await page.evaluate(layoutReport, { touch })

            // the end of the page, where a fixed bar could cover the last controls
            await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight))
            await page.waitForTimeout(150)
            const bottom = await page.evaluate(layoutReport, { touch })
            const found = [
              ...new Set([...problems(top), ...problems({ ...bottom, culprits: [], overlaps: [], mismatched: [] })]),
            ]

            if (top.truncated.length) {
              console.info(`${r.name} @${vp.width}: ${top.truncated.length} truncated without a title`)
            }

            expect(errors, 'page errors').toEqual([])
            expect(found, `${r.path} at ${vp.width}px`).toEqual([])
          } finally {
            await page.close()
          }
        })
      }
    })
  }
}
