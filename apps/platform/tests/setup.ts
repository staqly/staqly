import '@testing-library/jest-dom/vitest'
import { cleanup, configure } from '@testing-library/react'
import { afterEach, vi } from 'vitest'

import { nav } from './navigation'

// findBy*/waitFor give up after 1 s by default. CI runs these tests next to `next build` on 4 cores, where the first
// open of a lazily loaded dialog or drawer (compiled on first import) took 7.5 s. They resolve as soon as the element
// shows, so a long ceiling costs a passing test nothing; testTimeout (vitest.config.ts) stays above it.
configure({ asyncUtilTimeout: 15_000 })

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
