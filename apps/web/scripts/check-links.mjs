#!/usr/bin/env node
// Link check of the static export (out/), shared with the docs (packages/check-links). Runs after `next build`.
import { join } from 'node:path'

import { checkLinks } from '@staqly/check-links'

await checkLinks({ root: join(import.meta.dirname, '..', 'out') })
