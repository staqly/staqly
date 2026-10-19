import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared'

import { Logo } from '@/components/logo'
import { env } from '@/lib/env.mjs'

import { repoUrl } from './shared'

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: <Logo />,
      url: '/',
    },
    githubUrl: repoUrl,
    themeSwitch: { enabled: false }, // dark only (components/provider.tsx)
    links: [
      { text: 'API reference', url: '/api/reference', active: 'nested-url' },
      { text: 'staqly.ai', url: env.webUrl, external: true },
    ],
  }
}
