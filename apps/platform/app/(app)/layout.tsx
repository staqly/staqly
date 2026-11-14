import { NoPlanGate, WelcomeGate } from '@/components/onboarding/welcome-redirect'
import { AppShell } from '@/components/shell/app-shell'
import { AuthGate } from '@/components/shell/auth-gate'
import { WorkspaceZone } from '@/components/shell/workspace-zone'

export default function AppLayout({ children }: { children: React.ReactNode }) {
  return (
    <AuthGate>
      <WorkspaceZone>
        <NoPlanGate>
          <WelcomeGate>
            <AppShell>{children}</AppShell>
          </WelcomeGate>
        </NoPlanGate>
      </WorkspaceZone>
    </AuthGate>
  )
}
