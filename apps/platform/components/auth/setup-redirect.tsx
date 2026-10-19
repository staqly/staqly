'use client'

import { useRouter } from 'next/navigation'
import { useEffect } from 'react'

/** /setup is the address of the old workspace sign-up: it moved to /signup (query kept, e.g. a picked plan). */
export function SetupRedirect() {
  const router = useRouter()

  useEffect(() => {
    router.replace(`/signup${window.location.search}`)
  }, [router])

  return null
}
