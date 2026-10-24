'use client'

import { useEffect, useRef, useState, useSyncExternalStore } from 'react'

import { TerminalCard } from '@/components/terminal'
import { APP, MCP_URL } from '@/lib/site'

type Preview = { name: string; title: string; code: string }

// How each tool talks to Staqly: real MCP tool names, REST fields, SDK methods and CLI flags.
const PREVIEWS: Preview[] = [
  {
    name: 'Claude Code',
    title: 'Claude Code',
    code: `# once: claude mcp add --transport http staqly ${MCP_URL} --header "Authorization: Bearer $STAQLY_API_KEY"
$ claude "Write three reel captions into Reels, one per evening this week"
⏺ staqly · create_post → Reels (X, Instagram)
⏺ staqly · schedule_post → Wed 18:00, Thu 18:00, Fri 18:00
→ 3 posts on the calendar`,
  },
  {
    name: 'Codex',
    title: 'Codex',
    code: `# once: codex mcp add staqly --url ${MCP_URL} --bearer-token-env-var STAQLY_API_KEY
$ codex "What is scheduled this week? Move Friday's post to Saturday 10:00"
⏺ staqly · list_posts → 4 scheduled
⏺ staqly · schedule_post → Sat 10:00
→ Friday's post now goes out on Saturday at 10:00`,
  },
  {
    name: 'OpenCode',
    title: 'OpenCode',
    code: `# opencode.json: "mcp": { "staqly": { "type": "remote", "url": "${MCP_URL}",
#   "headers": { "Authorization": "Bearer {env:STAQLY_API_KEY}" } } }
$ opencode run "Shorten the X version of today's post and publish it now"
⏺ staqly · preview_post → X: 312 / 280
⏺ staqly · update_post → X version shortened
⏺ staqly · publish_post → publishing`,
  },
  {
    name: 'Cursor',
    title: 'Cursor chat',
    code: `# Settings → MCP: Streamable HTTP, ${MCP_URL}, Authorization: Bearer stk_...
"Fill Product updates with five posts about v1.4, weekdays at 09:00"
⏺ staqly · create_schedule → Weekdays at 09:00
⏺ staqly · create_post → Product updates (5 drafts)
→ the schedule publishes the next draft every weekday morning`,
  },
  {
    name: 'n8n',
    title: 'n8n · HTTP Request node',
    code: `# Method POST · Header Auth: Authorization: Bearer {{ $env.STAQLY_API_KEY }}
URL   ${APP}/v1/posts
Body  {"collection": "Instagram Avatar", "content": "{{ $json.caption }}",
       "media": ["{{ $json.media_id }}"], "targets": [{"channel_id": "ch_..."}],
       "publish_now": true}
→ 201 {"id": "post_...", "status": "publishing"}`,
  },
  {
    name: 'REST API',
    title: 'Terminal',
    code: `$ curl -X POST ${APP}/v1/posts \\
    -H "Authorization: Bearer $STAQLY_API_KEY" -H 'Content-Type: application/json' \\
    -d '{"collection":"Reels","content":"New video tonight","targets":[{"channel_id":"ch_..."}],"publish_at":"2026-11-03T18:00:00Z","status":"scheduled"}'
→ 201 {"id":"post_8f2...","status":"scheduled"}`,
  },
  {
    name: 'TypeScript SDK',
    title: 'post.ts',
    code: `$ npm install @staqly/sdk
import { Staqly } from '@staqly/sdk'

const staqly = new Staqly({ baseUrl: '${APP}', apiKey: process.env.STAQLY_API_KEY })
await staqly.posts.create({
  collection: 'Reels', content: 'New video tonight',
  targets: [{ channel_id: 'ch_...' }], publish_at: '2026-11-03T18:00:00Z', status: 'scheduled',
})`,
  },
  {
    name: 'Python SDK',
    title: 'post.py',
    code: `$ pip install staqly
import os
from staqly import Staqly

staqly = Staqly(base_url="${APP}", api_key=os.environ["STAQLY_API_KEY"])
staqly.posts.create("Reels", content="New video tonight",
    targets=[{"channel_id": "ch_..."}], publish_at="2026-11-03T18:00:00Z", status="scheduled")`,
  },
  {
    name: 'CLI',
    title: 'Terminal',
    code: `# cloud: export STAQLY_API_URL=${APP} STAQLY_API_KEY=stk_...
$ staqly posts create Reels --content "New video tonight" \\
    --channel X --publish-at 2026-11-03T18:00
$ staqly posts list --status scheduled`,
  },
]

const ROTATE_MS = 7000 // how long each tool shows; the progress bar under its tab runs this long

const REDUCED = '(prefers-reduced-motion: reduce)'
const subscribeReduced = (onChange: () => void) => {
  const mq = window.matchMedia(REDUCED)

  mq.addEventListener('change', onChange)

  return () => mq.removeEventListener('change', onChange)
}

/**
 * The tools as tabs over a terminal that shows how each one talks to Staqly. It moves to the next tool every few
 * seconds while on screen, and stops for good once someone picks a tab (or hovers or focuses it, for the while).
 * With reduced motion it never moves on its own.
 */
export function ToolPreview() {
  const root = useRef<HTMLDivElement>(null)
  const tabs = useRef<(HTMLButtonElement | null)[]>([])
  const [active, setActive] = useState(0)
  const [onScreen, setOnScreen] = useState(false)
  const [held, setHeld] = useState(false) // hover or focus inside
  const [picked, setPicked] = useState(false)
  const reduced = useSyncExternalStore(
    subscribeReduced,
    () => window.matchMedia(REDUCED).matches,
    () => true,
  )

  useEffect(() => {
    const el = root.current

    if (!el) {
      return
    }

    const io = new IntersectionObserver(([e]) => setOnScreen(e.isIntersecting), { threshold: 0.3 })

    io.observe(el)

    return () => io.disconnect()
  }, [])

  const pick = (i: number, focus = false) => {
    setPicked(true)
    setActive(i)

    if (focus) {
      tabs.current[i]?.focus()
    }
  }

  const p = PREVIEWS[active]!

  return (
    <div
      ref={root}
      className="mx-auto mt-6 max-w-5xl text-left"
      onMouseEnter={() => setHeld(true)}
      onMouseLeave={() => setHeld(false)}
      onFocus={() => setHeld(true)}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget)) {
          setHeld(false)
        }
      }}
    >
      <div role="tablist" aria-label="Tools that work with Staqly" className="flex flex-wrap justify-center gap-2">
        {PREVIEWS.map((x, i) => (
          <button
            key={x.name}
            ref={(el) => {
              tabs.current[i] = el
            }}
            type="button"
            role="tab"
            id={`tool-tab-${i}`}
            aria-selected={i === active}
            aria-controls="tool-panel"
            tabIndex={i === active ? 0 : -1}
            onClick={() => pick(i)}
            onKeyDown={(e) => {
              const last = PREVIEWS.length - 1
              const to =
                e.key === 'ArrowRight'
                  ? (i + 1) % PREVIEWS.length
                  : e.key === 'ArrowLeft'
                    ? (i + last) % PREVIEWS.length
                    : e.key === 'Home'
                      ? 0
                      : e.key === 'End'
                        ? last
                        : -1

              if (to >= 0) {
                e.preventDefault()
                pick(to, true)
              }
            }}
            className={`relative overflow-hidden rounded-full border px-3.5 py-1.5 text-sm font-medium transition-colors outline-none focus-visible:ring-2 focus-visible:ring-[color-mix(in_oklch,var(--brand)_45%,transparent)] pointer-coarse:min-h-10 ${
              i === active
                ? 'border-[color-mix(in_oklch,var(--brand)_45%,transparent)] bg-brand-soft text-brand-strong'
                : 'border-border bg-card text-foreground hover:bg-accent'
            }`}
          >
            {x.name}
            {/* How long until the next tool: a thin bar under the active tab while it rotates. */}
            {i === active && !reduced && !picked && (
              <span
                key={active}
                aria-hidden
                className="sq-tab-progress absolute inset-x-0 bottom-0 h-0.5 origin-left bg-brand"
                // The bar's end is the clock: it pauses with hover, focus and scrolling away.
                onAnimationEnd={() => setActive((a) => (a + 1) % PREVIEWS.length)}
                style={{
                  animationDuration: `${ROTATE_MS}ms`,
                  animationPlayState: held || !onScreen ? 'paused' : 'running',
                }}
              />
            )}
          </button>
        ))}
      </div>
      {/* As tall as the longest snippet, so switching tools never moves the page below. */}
      <div
        id="tool-panel"
        role="tabpanel"
        aria-labelledby={`tool-tab-${active}`}
        className="mx-auto mt-5 grid min-h-[20rem] max-w-3xl sm:min-h-[23rem]"
      >
        <TerminalCard key={p.name} title={p.title} code={p.code} />
      </div>
    </div>
  )
}
