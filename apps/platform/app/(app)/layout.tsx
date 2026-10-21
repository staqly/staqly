import { NoPlanGate, WelcomeGate } from '@/components/onboarding/welcome-redirect'
import { AppShell } from '@/components/shell/app-shell'
import { AuthGate } from '@/components/shell/auth-gate'

export default function AppLayout({ children }: { children: React.ReactNode }) {
  return (
    <AuthGate>
      <NoPlanGate>
        <WelcomeGate>
          <AppShell>{children}</AppShell>
        </WelcomeGate>
      </NoPlanGate>
    </AuthGate>
  )
}
