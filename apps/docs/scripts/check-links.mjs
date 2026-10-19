#!/usr/bin/env node
// Link check of the static export (out/), shared with staqly.ai (packages/check-links): pages and #anchors.
// Runs after `next build`. Built with DOCS_BASE_PATH (e.g. /staqly), every root-relative link must carry it.
import { join } from 'node:path'

import { checkLinks } from '@staqly/check-links'

import { env } from '../lib/env.mjs'

await checkLinks({ root: join(import.meta.dirname, '..', 'out'), basePath: env.basePath })
