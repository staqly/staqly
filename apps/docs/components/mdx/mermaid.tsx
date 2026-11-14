'use client'

import { Suspense, use, useEffect, useId, useRef, useState } from 'react'

/**
 * Client-side Mermaid renderer (works with static export: nothing is rendered
 * at build time). Themed with the staqly graphite + green palette; the docs are dark only.
 */
export function Mermaid({ chart }: { chart: string }) {
  // Mermaid is ~0.5 MB of JS: fetch and run it only once a diagram comes near the screen, not on every page load.
  // Starts false on the server and in the first client render, so hydration matches.
  const [near, setNear] = useState(false)
  const placeholderRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = placeholderRef.current

    if (near || !el) {
      return
    }

    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          setNear(true)
          io.disconnect()
        }
      },
      { rootMargin: '600px 0px' },
    )

    io.observe(el)

    return () => io.disconnect()
  }, [near])

  const placeholder = <div ref={placeholderRef} className="sq-mermaid sq-mermaid-placeholder" aria-hidden />

  if (!near) {
    return placeholder
  }

  // The placeholder holds the space while mermaid loads, instead of suspending the page around it.
  return (
    <Suspense fallback={placeholder}>
      <MermaidContent chart={chart} />
    </Suspense>
  )
}

const cache = new Map<string, Promise<unknown>>()

function cachePromise<T>(key: string, create: () => Promise<T>): Promise<T> {
  const hit = cache.get(key)

  if (hit) {
    return hit as Promise<T>
  }

  const p = create()

  cache.set(key, p)

  return p
}

const palette = {
  background: 'transparent',
  primaryColor: '#15261d',
  primaryBorderColor: '#4fe07d',
  primaryTextColor: '#eceef2',
  secondaryColor: '#25272d',
  tertiaryColor: '#1f2126',
  lineColor: '#7d818c',
  textColor: '#eceef2',
  edgeLabelBackground: '#1d1f24',
  clusterBkg: '#1d1f24',
  clusterBorder: '#34363d',
}

const NO_SVG = { svg: '' }

/**
 * Loaded and configured once, on the first diagram. Null when the chunk didn't load (offline, a deploy replaced it):
 * the diagrams show their source until the page is loaded again. The fallback is inside the cached promise, so every
 * render reads the same promise.
 */
function loadMermaid() {
  return cachePromise('mermaid', () =>
    import('mermaid').then(
      ({ default: mermaid }) => {
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: 'strict',
          fontFamily: 'var(--font-geist-sans), ui-sans-serif, system-ui, sans-serif',
          theme: 'base',
          themeVariables: { ...palette, fontSize: '14px' },
          flowchart: { curve: 'basis', padding: 12, htmlLabels: true },
        })

        return mermaid
      },
      (err: unknown) => {
        console.error('mermaid: could not load', err)

        return null
      },
    ),
  )
}

function MermaidContent({ chart }: { chart: string }) {
  const id = useId().replace(/[^a-zA-Z0-9]/g, '')
  const mermaid = use(loadMermaid())

  const text = chart.replaceAll('\\n', '\n')
  // A broken diagram, or one mermaid fails to draw, shows its source instead of taking the whole page down.
  const { svg } = mermaid
    ? use(
        cachePromise(`chart:${chart}`, async () => {
          if (!(await mermaid.parse(text, { suppressErrors: true }))) {
            console.error('mermaid: invalid diagram', text)

            return NO_SVG
          }

          return mermaid.render(`m${id}`, text).catch((err: unknown) => {
            console.error('mermaid: could not draw the diagram', err)

            return NO_SVG
          })
        }),
      )
    : NO_SVG

  if (!svg) {
    return (
      <pre className="sq-mermaid overflow-x-auto text-xs" data-invalid>
        <code>{text}</code>
      </pre>
    )
  }

  return <div className="sq-mermaid" dangerouslySetInnerHTML={{ __html: svg }} />
}
