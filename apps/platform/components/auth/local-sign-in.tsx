'use client'

import { KeyRound, Terminal } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'

import { accessTokenErrorMessage, useAccessTokenSignIn } from './access-token-sign-in'
import { AuthLayout } from './auth-layout'

const code = 'rounded bg-muted px-1 py-0.5 font-mono text-[0.85em] text-foreground'

/**
 * The local install's sign-in: every browser signs in once with the access token Staqly printed when it started
 * (or opens the link printed beside it, which does this by itself).
 */
export function LocalSignIn({ initialError, onDone }: { initialError?: string; onDone?: () => void }) {
  const [token, setToken] = useState('')
  const [error, setError] = useState(initialError ?? '')
  const signIn = useAccessTokenSignIn(onDone)

  return (
    <AuthLayout>
      <div className="grid gap-6" data-testid="local-sign-in">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Sign in to Staqly</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            This browser needs the access token of this computer once. Agents and scripts use the same token.
          </p>
        </div>
        <form
          noValidate
          className="grid gap-3"
          onSubmit={(e) => {
            e.preventDefault()

            const v = token.trim()

            if (!v) {
              setError('Paste the access token.')

              return
            }

            if (!signIn.isPending) {
              setError('')
              signIn.mutate(v, { onError: (err) => setError(accessTokenErrorMessage(err)) })
            }
          }}
        >
          <Field data-invalid={!!error}>
            <FieldLabel htmlFor="access-token" required>
              Access token
            </FieldLabel>
            <Input
              id="access-token"
              type="password"
              autoComplete="off"
              spellCheck={false}
              placeholder="stk_local_..."
              className="h-11 font-mono text-base sm:text-sm"
              value={token}
              aria-invalid={!!error}
              aria-required
              onChange={(e) => setToken(e.target.value)}
            />
            <FieldError>{error}</FieldError>
            {!error && <FieldDescription>It starts with stk_local_.</FieldDescription>}
          </Field>
          <Button type="submit" size="lg" className="h-11" disabled={signIn.isPending} data-testid="token-sign-in">
            <KeyRound /> {signIn.isPending ? 'Signing in...' : 'Sign in'}
          </Button>
        </form>
        <div className="grid gap-3 rounded-2xl bg-muted/50 p-4 text-sm">
          <p className="flex items-center gap-2 font-medium">
            <Terminal className="size-4 text-muted-foreground" /> Where to find it
          </p>
          <ul className="grid gap-2 text-muted-foreground">
            <li>
              Staqly prints it when it starts, with a link that signs this browser in by itself. Open that link, or copy
              the token from there.
            </li>
            <li>
              To print it again, run <code className={code}>staqly serve token</code> on this computer. With Docker:{' '}
              <code className={code}>docker exec staqly server token</code>.
            </li>
            <li>
              On a phone, scan the code from <strong className="font-medium text-foreground">Connect a device</strong>{' '}
              in a browser that is already signed in.
            </li>
          </ul>
        </div>
      </div>
    </AuthLayout>
  )
}
