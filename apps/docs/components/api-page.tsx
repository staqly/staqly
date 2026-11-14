'use client'

import { createOpenAPIPage } from 'fumadocs-openapi/ui'
import { useEffect, useRef, type ComponentProps } from 'react'

// The playground sends requests from the reader's browser to their own cloud
// instance with their API key: cloud mode answers CORS for docs.staqly.ai
// (CORS_ALLOWED_ORIGINS). Local mode never accepts cross-origin calls.
const Page = createOpenAPIPage({
  playground: { enabled: true },
})

// fumadocs-openapi renders its selects (auth scheme, body type) without an accessible name; name them after
// their current value until the library does.
function labelComboboxes(root: HTMLElement) {
  for (const el of root.querySelectorAll<HTMLElement>(
    '[role=combobox]:not([aria-labelledby]):is(:not([aria-label]), [data-auto-label])',
  )) {
    const label = el.textContent?.trim() || 'Select'

    if (el.getAttribute('aria-label') !== label) {
      el.setAttribute('aria-label', label)
      el.setAttribute('data-auto-label', '')
    }
  }
}

export function OpenAPIPage(props: ComponentProps<typeof Page>) {
  const root = useRef<HTMLDivElement>(null)

  // Watch only the reference itself (not the whole document), and label at most once a frame however many
  // mutations arrive: a page has hundreds of elements and the playground changes many per keystroke. characterData
  // stays: React updates a select's shown value in place, as a text change.
  useEffect(() => {
    const el = root.current

    if (!el) {
      return
    }

    let frame = 0

    labelComboboxes(el)

    const observer = new MutationObserver(() => {
      if (!frame) {
        frame = requestAnimationFrame(() => {
          frame = 0
          labelComboboxes(el)
        })
      }
    })

    observer.observe(el, { subtree: true, childList: true, characterData: true })

    return () => {
      observer.disconnect()
      cancelAnimationFrame(frame)
    }
  }, [])

  return (
    <div ref={root} className="contents">
      <Page {...props} />
    </div>
  )
}
