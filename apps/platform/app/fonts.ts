import localFont from 'next/font/local'

export { GeistSans } from 'geist/font/sans'

// geist/font/mono, but not preloaded. Every page preloaded both 70 KB variable fonts at the highest
// priority, ahead of the scripts (on a slow phone connection the two took ~2 s of the bandwidth before the
// first script finished), though mono only shows in IDs, code and shortcut hints: it loads when one is on
// screen, and the fallback stack is monospace too. The root layout and global-error share this one
// definition: two definitions of the same file are two font URLs, and the browser downloaded both.
export const GeistMono = localFont({
  src: '../node_modules/geist/dist/fonts/geist-mono/GeistMono-Variable.woff2',
  variable: '--font-geist-mono',
  weight: '100 900',
  preload: false,
  adjustFontFallback: false,
  fallback: ['ui-monospace', 'SFMono-Regular', 'Roboto Mono', 'Menlo', 'Monaco', 'Liberation Mono', 'monospace'],
})
