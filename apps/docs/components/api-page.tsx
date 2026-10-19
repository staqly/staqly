'use client'

import { createOpenAPIPage } from 'fumadocs-openapi/ui'
import { useEffect, type ComponentProps } from 'react'

// The playground sends requests from the reader's browser to their own cloud
// instance with their API key: cloud mode answers CORS for docs.staqly.ai
// (CORS_ALLOWED_ORIGINS). Local mode never accepts cross-origin calls.
const Page = createOpenAPIPage({
  playground: { enabled: true },
})

// fumadocs-openapi renders its selects (auth scheme, body type) without an accessible name; name them after
// their current value until the library does.
function labelComboboxes() {
  for (const el of document.querySelectorAll<HTMLElement>(
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
  useEffect(() => {
    labelComboboxes()
    const observer = new MutationObserver(labelComboboxes)

    observer.observe(document.body, { subtree: true, childList: true, characterData: true })

    return () => observer.disconnect()
  }, [])

  return <Page {...props} />
}
