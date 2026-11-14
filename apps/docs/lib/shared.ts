import { env } from './env.mjs'

export const docsRoute = '/'

/** Where "Edit on GitHub" links point. */
export const gitConfig = {
  user: 'staqly',
  repo: 'staqly',
  branch: 'main',
  contentDir: 'apps/docs/content/docs',
}

export function editUrl(path: string) {
  return `https://github.com/${gitConfig.user}/${gitConfig.repo}/blob/${gitConfig.branch}/${gitConfig.contentDir}/${path}`
}

export const repoUrl = `https://github.com/${gitConfig.user}/${gitConfig.repo}`

/**
 * Markdown of a page for LLMs. `segments` feed the /llms.mdx route that
 * renders it; `url` is the public copy, `/<page>.md` (index → `/index.md`),
 * written next to the HTML by scripts/markdown-files.mjs after the export.
 */
export function getPageMarkdownUrl(page: { slugs: string[] }) {
  const segments = [...page.slugs, 'content.md']

  return { segments, url: `${env.basePath}/${page.slugs.join('/') || 'index'}.md` }
}
