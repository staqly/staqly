import { remarkMdxMermaid } from 'fumadocs-core/mdx-plugins'
import { defineConfig } from 'fumadocs-mdx/config'

// Global MDX options. Collections themselves are declared with the macro API
// in lib/collections.ts.
export default defineConfig({
  mdxOptions: {
    // ```mermaid fences become <Mermaid chart="…" /> (rendered client-side).
    remarkPlugins: (plugins) => [...plugins, remarkMdxMermaid],
    rehypeCodeOptions: {
      themes: { light: 'github-light', dark: 'github-dark-dimmed' },
    },
  },
})
