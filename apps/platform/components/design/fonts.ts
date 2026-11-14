// The design editor's fonts, self-hosted (Fontsource): the dashboard loads nothing from other sites, and a
// browser downloads a font file only when a text uses it. Regular and bold, all subsets (unicode-range).
import '@fontsource/inter/400.css'
import '@fontsource/inter/700.css'
import '@fontsource/montserrat/400.css'
import '@fontsource/montserrat/700.css'
import '@fontsource/poppins/400.css'
import '@fontsource/poppins/700.css'
import '@fontsource/space-grotesk/400.css'
import '@fontsource/space-grotesk/700.css'
import '@fontsource/oswald/400.css'
import '@fontsource/oswald/700.css'
import '@fontsource/roboto-slab/400.css'
import '@fontsource/roboto-slab/700.css'
import '@fontsource/playfair-display/400.css'
import '@fontsource/playfair-display/700.css'
import '@fontsource/dm-serif-display/400.css'
import '@fontsource/bebas-neue/400.css'
import '@fontsource/anton/400.css'
import '@fontsource/lobster/400.css'
import '@fontsource/pacifico/400.css'
import '@fontsource/caveat/400.css'
import '@fontsource/caveat/700.css'

export const FONTS = [
  'Inter',
  'Montserrat',
  'Poppins',
  'Space Grotesk',
  'Oswald',
  'Roboto Slab',
  'Playfair Display',
  'DM Serif Display',
  'Bebas Neue',
  'Anton',
  'Lobster',
  'Pacifico',
  'Caveat',
]

/** Resolves once the font's file is in (canvas text does not wait for fonts by itself). */
export function loadFont(family: string, bold = false) {
  if (typeof document === 'undefined' || !document.fonts) {
    return Promise.resolve()
  }

  return document.fonts.load(`${bold ? 700 : 400} 32px "${family}"`).then(
    () => undefined,
    () => undefined,
  )
}
