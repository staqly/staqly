import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

import { ORT_CDN_BASE } from '@/lib/opengpex/plugins/base/drawers/AIToolsDrawer/_shared/inference/ort-session'

// The media editor's CSP (apps/api/internal/web/web.go) allows jsDelivr per package version: bumping ONNX Runtime
// or transformers.js here without the CSP there blocks the on-device AI tools.
const webGo = readFileSync(resolve(__dirname, '../../api/internal/web/web.go'), 'utf8')
const tfm = readFileSync(
  resolve(__dirname, '../lib/opengpex/plugins/base/drawers/AIToolsDrawer/_shared/inference/tfm-session.ts'),
  'utf8',
)

describe('editor CSP', () => {
  it('allows the ONNX Runtime version the editor loads', () => {
    const pkg = ORT_CDN_BASE.replace(/dist\/$/, '')

    expect(webGo).toContain(`"${pkg} "`)
  })

  it('allows the transformers.js version the editor loads', () => {
    const url = /const TRANSFORMERS_CDN = '([^']+)'/.exec(tfm)?.[1]

    expect(url).toMatch(/^https:\/\/cdn\.jsdelivr\.net\/npm\/@huggingface\/transformers@\d/)
    expect(webGo).toContain(`"${url} "`)
  })
})
