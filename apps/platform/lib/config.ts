'use client'

import { useSetupStatus } from './queries'

export const DEFAULT_PUBLIC_API_URL = 'http://localhost:3333'

/**
 * Public base URL of the API as seen by agents/tools on the host machine: the API's own PUBLIC_API_URL,
 * reported by GET /v1/setup. Read at runtime, so pre-built dashboard images need no build-time setting.
 */
export function usePublicApiUrl() {
  const setup = useSetupStatus()

  return (setup.data?.public_api_url || DEFAULT_PUBLIC_API_URL).replace(/\/+$/, '')
}
