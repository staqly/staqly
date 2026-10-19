import type { Metadata } from 'next'

import { EditorPage } from '@/components/media-editor/editor-page'
import { AuthGate } from '@/components/shell/auth-gate'

export const metadata: Metadata = { title: 'Edit media' }

export default function Page() {
  return (
    <AuthGate>
      <EditorPage />
    </AuthGate>
  )
}
