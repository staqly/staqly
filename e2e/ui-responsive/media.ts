// Media for the seed, made by the browser itself so the suite needs no fixtures or ffmpeg:
// a wide and a tall PNG, a short WebM (Playwright's video recording) and a PDF.
import { mkdtempSync, readdirSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import type { Browser } from 'playwright'

import { fixture, type SeedMedia } from './seed.js'

const art = (label: string) =>
  `<body style="margin:0;height:100vh;display:grid;place-items:center;font:700 64px system-ui;color:white;background:linear-gradient(135deg,#7c3aed,#db2777 50%,#f59e0b)">${label}</body>`

export async function makeMedia(browser: Browser): Promise<SeedMedia> {
  const dir = mkdtempSync(join(tmpdir(), 'staqly-ui-media-'))
  const images = [fixture('avatar0.png'), fixture('avatar1.png')]

  for (const [name, width, height] of [
    ['wide', 1600, 600],
    ['tall', 500, 1500],
  ] as const) {
    const page = await browser.newPage({ viewport: { width, height } })

    await page.setContent(art(name))
    const path = join(dir, `${name}.png`)

    await page.screenshot({ path })
    await page.close()
    images.push(path)
  }

  const videoDir = join(dir, 'video')
  const ctx = await browser.newContext({ viewport: { width: 640, height: 360 }, recordVideo: { dir: videoDir } })
  const page = await ctx.newPage()

  await page.setContent(art('video'))
  await page.waitForTimeout(800)
  await ctx.close()
  const video = readdirSync(videoDir).find((f) => f.endsWith('.webm'))
  let pdf: string | undefined

  if (browser.browserType().name() === 'chromium') {
    const p = await browser.newPage()

    await p.setContent(art('report'))
    pdf = join(dir, 'report.pdf')
    await p.pdf({ path: pdf })
    await p.close()
  }

  return { images, video: video ? join(videoDir, video) : undefined, pdf }
}
