import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared'

import { Logo } from '@/components/logo'

import { repoUrl } from './shared'

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: <Logo />,
      url: '/',
    },
    githubUrl: repoUrl,
    links: [
      { text: 'Docs', url: '/docs', active: 'nested-url' },
      { text: 'API reference', url: '/docs/api/reference', active: 'nested-url' },
      { text: 'staqly.ai', url: 'https://staqly.ai', external: true },
    ],
  }
}
