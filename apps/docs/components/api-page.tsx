'use client'

import { createOpenAPIPage } from 'fumadocs-openapi/ui'

// The playground sends requests from the reader's browser to their own cloud
// instance with their API key: cloud mode answers CORS for docs.staqly.ai
// (CORS_ALLOWED_ORIGINS). Local mode never accepts cross-origin calls.
export const OpenAPIPage = createOpenAPIPage({
  playground: { enabled: true },
})
