import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach, vi } from 'vitest'

import { nav } from './navigation'

vi.mock('next/navigation', async () => (await import('./navigation')).navigationModule)

afterEach(() => {
  cleanup()
  nav.reset()
  vi.unstubAllGlobals()
})

// jsdom gaps used by Radix UI / our components (skipped in `@vitest-environment node` files).
if (typeof window !== 'undefined') {
  installDomShims()
}

function installDomShims() {
  if (!window.matchMedia) {
    window.matchMedia = (query: string) =>
      ({
        matches: false,
        media: query,
        onchange: null,
        addEventListener: () => {},
        removeEventListener: () => {},
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false,
      }) as MediaQueryList
  }

  class RO {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  globalThis.ResizeObserver ??= RO as unknown as typeof ResizeObserver
  Element.prototype.scrollIntoView ??= function () {}

  Element.prototype.hasPointerCapture ??= () => false
  Element.prototype.releasePointerCapture ??= () => {}

  Element.prototype.setPointerCapture ??= () => {}

  window.HTMLElement.prototype.scrollIntoView ??= function () {}

  URL.createObjectURL ??= () => 'blob:mock'
  URL.revokeObjectURL ??= () => {}
}
