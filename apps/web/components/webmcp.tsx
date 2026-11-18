'use client'

import { useEffect } from 'react'

type ModelContext = {
  registerTool: (tool: object, options?: { signal?: AbortSignal }) => Promise<void> | void
}

// WebMCP: the free tools and the plans as tools an AI agent in the browser can call (lib/webmcp.ts). The spec puts
// modelContext on document, Chrome's first implementation on navigator. A browser without it loads nothing.
export function WebMcp() {
  useEffect(() => {
    const context = ((document as { modelContext?: ModelContext }).modelContext ??
      (navigator as { modelContext?: ModelContext }).modelContext) as ModelContext | undefined

    if (!context?.registerTool) {
      return
    }

    const registration = new AbortController()

    void import('@/lib/webmcp').then(({ WEBMCP_TOOLS }) => {
      for (const tool of WEBMCP_TOOLS) {
        if (!registration.signal.aborted) {
          // A tool the browser refuses (a sync throw or a rejection) must not stop the others.
          Promise.resolve()
            .then(() => context.registerTool(tool, { signal: registration.signal }))
            .catch((error: unknown) => console.warn('WebMCP: couldn’t register', tool.name, error))
        }
      }
    })

    return () => registration.abort()
  }, [])

  return null
}
