import { defineConfig } from "fumadocs-mdx/config";
import { remarkMdxMermaid } from "fumadocs-core/mdx-plugins";

// Global MDX options. Collections themselves are declared with the macro API
// in lib/collections.ts.
export default defineConfig({
  mdxOptions: {
    // ```mermaid fences become <Mermaid chart="…" /> (rendered client-side).
    remarkPlugins: (plugins) => [...plugins, remarkMdxMermaid],
    rehypeCodeOptions: {
      themes: { light: "github-light", dark: "github-dark-dimmed" },
    },
  },
});
