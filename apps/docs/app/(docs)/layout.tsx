import { DocsLayout } from 'fumadocs-ui/layouts/docs'

import { baseOptions } from '@/lib/layout.shared'
import { docsPageTree } from '@/lib/source'

// Guides and the reference index; the per-operation API pages have their own layout (app/(reference)) whose sidebar
// lists the operations, so these pages don't carry them.
export default function Layout({ children }: LayoutProps<'/'>) {
  return (
    <DocsLayout tree={docsPageTree()} {...baseOptions()}>
      {children}
    </DocsLayout>
  )
}
