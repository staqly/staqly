import { GuestSettingsGate } from '@/components/settings/guest-settings-gate'

export default function SettingsLayout({ children }: { children: React.ReactNode }) {
  return <GuestSettingsGate>{children}</GuestSettingsGate>
}
