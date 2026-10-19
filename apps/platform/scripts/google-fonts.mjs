// Refreshes the Google Fonts directory the media editor's font search reads
// (lib/opengpex/core/fonts/google-fonts.json), so the browser needs no API key.
// Run by hand: node scripts/google-fonts.mjs
import { writeFileSync } from 'node:fs'

const CATEGORY = {
  'Sans Serif': 'sans-serif',
  Serif: 'serif',
  Display: 'display',
  Handwriting: 'handwriting',
  Monospace: 'monospace',
}

const res = await fetch('https://fonts.google.com/metadata/fonts')

if (!res.ok) {
  throw new Error(`HTTP ${res.status}`)
}

const text = await res.text()
const { familyMetadataList } = JSON.parse(text.slice(text.indexOf('{')))

// [family, category, variants] by popularity; variants in the Developer API spelling ("regular", "700italic").
const fonts = familyMetadataList
  .toSorted((a, b) => a.popularity - b.popularity)
  .map((f) => [
    f.family,
    CATEGORY[f.category] ?? 'sans-serif',
    Object.keys(f.fonts).map((v) => {
      const italic = v.endsWith('i')
      const weight = italic ? v.slice(0, -1) : v

      if (weight === '400') {
        return italic ? 'italic' : 'regular'
      }

      return italic ? `${weight}italic` : weight
    }),
  ])

writeFileSync(new URL('../lib/opengpex/core/fonts/google-fonts.json', import.meta.url), JSON.stringify(fonts) + '\n')
console.log(`${fonts.length} families`)
