import { DocsLayout } from 'fumadocs-ui/layouts/docs'

import { baseOptions } from '@/lib/layout.shared'
import { source } from '@/lib/source'

// The API reference's tag and operation pages: the whole sidebar, with a folder of operations per tag.
export default function Layout({ children }: LayoutProps<'/'>) {
  return (
    <DocsLayout tree={source.getPageTree()} {...baseOptions()}>
      {children}
    </DocsLayout>
  )
}
