export const appName = 'staqly'
export const docsRoute = '/docs'
export const docsContentRoute = '/llms.mdx/docs'

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

export function getPageMarkdownUrl(page: { slugs: string[] }) {
  const segments = [...page.slugs, 'content.md']

  return { segments, url: `${docsContentRoute}/${segments.join('/')}` }
}
