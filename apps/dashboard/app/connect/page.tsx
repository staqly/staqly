import type { Metadata } from 'next'

import { ConnectDevice } from '@/components/devices/connect-device'

export const metadata: Metadata = { title: 'Connect this device', referrer: 'no-referrer' }

export default function ConnectPage() {
  return <ConnectDevice />
}
