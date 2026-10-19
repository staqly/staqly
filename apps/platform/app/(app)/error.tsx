'use client'

import { ErrorState } from '@/components/common/error-state'

// A page that fails to render keeps the sidebar and the navigation (app/error.tsx would replace the whole shell).
// React logs the error itself (onCaughtError), so nothing is logged here.
export default function AppError({ error, retry }: { error: Error & { digest?: string }; retry: () => void }) {
  return (
    <div className="p-4 sm:p-6">
      <ErrorState
        error={error}
        title="This page hit an unexpected error"
        description={
          error.digest
            ? `Your data is safe. Try again, or open another page. Error ID: ${error.digest}`
            : 'Your data is safe. Try again, or open another page.'
        }
        onRetry={() => retry()}
      />
    </div>
  )
}
