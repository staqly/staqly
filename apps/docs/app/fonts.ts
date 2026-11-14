import localFont from 'next/font/local'

// Geist Sans and Geist Mono (Vercel and basement.studio, SIL OFL 1.1: packages/brand/fonts/OFL.txt, shared with the
// docs and staqly.ai), cut down to Latin plus the arrows and symbols the sites show: ~34 KB each instead of the geist
// package's ~70 KB variable fonts. Same CSS variables as geist/font/*, so font-sans and font-mono keep working; other
// characters fall back to the system font. Made from the geist package's variable .ttf files, per font:
//   uvx --from 'fonttools[woff]' pyftsubset Geist-Variable.ttf --layout-features='*' --flavor=woff2 \
//     --unicodes='U+0000-00FF,U+0131,U+0152-0153,U+02BB-02BC,U+02C6,U+02DA,U+02DC,U+0304,U+0308,U+0329,U+2000-206F,U+20AC,U+2122,U+2190-2199,U+2212,U+2215,U+221E,U+2318,U+23FA,U+2713-2714,U+276F,U+FEFF,U+FFFD' \
//     --output-file=Geist-Variable-latin.woff2
export const GeistSans = localFont({
  src: '../../../packages/brand/fonts/Geist-Variable-latin.woff2',
  variable: '--font-geist-sans',
  weight: '100 900',
})

// Preloaded too: the hero demo and code blocks show mono above the fold, and a font found only through the CSS
// starts late (measured: +300 ms first paint on a throttled phone without the preload).
export const GeistMono = localFont({
  src: '../../../packages/brand/fonts/GeistMono-Variable-latin.woff2',
  variable: '--font-geist-mono',
  weight: '100 900',
  adjustFontFallback: false,
  fallback: [
    'ui-monospace',
    'SFMono-Regular',
    'Roboto Mono',
    'Menlo',
    'Monaco',
    'Liberation Mono',
    'DejaVu Sans Mono',
    'Courier New',
    'monospace',
  ],
})
