import { expect, ready, test } from '../../lib/fixtures.js'

// Two small PNGs and a two-second tone (16-bit WAV), uploaded in the library.
const PNG_A = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAQAAAADCAIAAAA7ljmRAAAAEElEQVR4nGM4YVMBRww4OQBfThHR5zkcVQAAAABJRU5ErkJggg==',
  'base64',
)
const PNG_B = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAEElEQVR4nGM4YVMBRAwQCgAq3gXxFDo9qwAAAABJRU5ErkJggg==',
  'base64',
)

function tone(seconds: number, rate = 8000) {
  const n = seconds * rate
  const b = Buffer.alloc(44 + n * 2)

  b.write('RIFF', 0)
  b.writeUInt32LE(36 + n * 2, 4)
  b.write('WAVEfmt ', 8)
  b.writeUInt32LE(16, 16)
  b.writeUInt16LE(1, 20)
  b.writeUInt16LE(1, 22)
  b.writeUInt32LE(rate, 24)
  b.writeUInt32LE(rate * 2, 28)
  b.writeUInt16LE(2, 32)
  b.writeUInt16LE(16, 34)
  b.write('data', 36)
  b.writeUInt32LE(n * 2, 40)

  for (let i = 0; i < n; i++) {
    b.writeInt16LE(Math.round(Math.sin((i / rate) * 440 * 2 * Math.PI) * 8000), 44 + i * 2)
  }

  return b
}

test.describe('the video and sound editor', () => {
  test('pictures and a sound make a video with a caption, saved as a file and opened again; the sound is cut in two', async ({
    page,
  }) => {
    test.setTimeout(180_000)
    const stamp = Date.now().toString(36)
    const names = [`clip-a-${stamp}.png`, `clip-b-${stamp}.png`, `tone-${stamp}.wav`]

    await page.goto('/media')
    await ready(page, 'All files')
    await page.getByTestId('media-upload-input').setInputFiles([
      { name: names[0]!, mimeType: 'image/png', buffer: PNG_A },
      { name: names[1]!, mimeType: 'image/png', buffer: PNG_B },
      { name: names[2]!, mimeType: 'audio/wav', buffer: tone(2) },
    ])

    for (const name of names) {
      await expect(page.getByRole('button', { name, exact: true })).toBeVisible()
    }

    for (const name of names) {
      await page.getByRole('button', { name: `Select ${name}` }).click()
    }

    // Pictures and a sound picked: the selection makes a video, the sound as its music.
    await page.getByTestId('media-make-video').click()
    await expect(page).toHaveURL(/\/media\/video\?ids=/)

    const timeline = page.getByRole('listbox', { name: 'Timeline' })

    await expect(timeline.getByRole('option', { name: new RegExp(`clip-[ab]-${stamp}`) })).toHaveCount(2)
    await expect(timeline.getByRole('option', { name: new RegExp(`tone-${stamp}`) })).toHaveCount(1)
    await expect(page.getByTestId('timeline-time')).toContainText('/ 0:06.0')

    // It plays: the clock moves.
    await page.getByTestId('timeline-play').click()
    await expect(page.getByTestId('timeline-time')).not.toHaveText(/^0:00\.0 /, { timeout: 5_000 })
    await page.getByTestId('timeline-play').click()

    // A caption at the playhead.
    await page.getByRole('navigation', { name: 'Video tools' }).getByRole('button', { name: 'Text' }).click()
    await page.getByRole('button', { name: 'Subtitle' }).click()
    await expect(timeline.getByRole('option', { name: /Caption: Your words here/ })).toBeVisible()

    // Saved: made in the browser, a video file in Media, the timeline its project.
    await page.getByTestId('timeline-save').click()
    await page.getByTestId('design-save-confirm').click()
    await expect(page.getByText(/Saved to Media as .*\.mp4/)).toBeVisible({ timeout: 90_000 })
    await expect(page).toHaveURL(/\?project=/)

    const list = await (await page.request.get('/api/v1/media?limit=100&kind=video')).json()
    const made = (list.data as { original_name: string; mime_type: string }[]).find((m) =>
      m.original_name.startsWith(`clip-`),
    )

    expect(made?.mime_type).toBe('video/mp4')

    await page.reload()
    await expect(timeline.getByRole('option', { name: /Caption: Your words here/ })).toBeVisible({ timeout: 30_000 })

    // The sound alone in the sound editor: cut in two at one second.
    const sounds = await (await page.request.get(`/api/v1/media?limit=100&kind=audio&q=tone-${stamp}`)).json()

    await page.goto(`/media/audio?ids=${sounds.data[0].id}`)

    const tracks = page.getByRole('listbox', { name: 'Timeline' })

    await tracks.getByRole('option', { name: new RegExp(`tone-${stamp}`) }).click()
    await page.keyboard.press('Shift+ArrowRight')
    await page.keyboard.press('s')
    await expect(tracks.getByRole('option', { name: new RegExp(`tone-${stamp}`) })).toHaveCount(2)
    await page.getByTestId('timeline-save').click()
    await page.getByTestId('design-save-confirm').click()
    await expect(page.getByText(/Saved to Media as .*\.(m4a|wav)/)).toBeVisible({ timeout: 60_000 })
  })
})
