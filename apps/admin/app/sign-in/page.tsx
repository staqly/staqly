'use client'

import { Logo } from '@staqly/brand/logo'
import { useEffect, useState, type FormEvent } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { adminApi, AdminApiError } from '@/lib/api'

export default function SignInPage() {
  const [email, setEmail] = useState('')
  const [code, setCode] = useState('')
  const [sent, setSent] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [methods, setMethods] = useState<{ email_code: boolean; access_token: boolean } | null>(null)
  const [token, setToken] = useState('')

  useEffect(() => {
    // plain fetch: adminApi.get redirects to /sign-in on 401
    fetch('/api/v1/admin/auth/methods')
      .then((r) => r.json())
      .then(setMethods)
      .catch(() => setMethods({ email_code: true, access_token: false }))
  }, [])

  async function submitToken(e: FormEvent) {
    e.preventDefault()
    setError('')
    setBusy(true)

    try {
      await adminApi.anon('/v1/admin/auth/token', { token: token.trim() })
      window.location.assign('/')
    } catch (err) {
      setError(err instanceof AdminApiError ? err.message : 'Something went wrong. Try again.')
      setBusy(false)
    }
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setBusy(true)

    try {
      if (sent) {
        await adminApi.anon('/v1/admin/auth/verify', { email, code: code.trim() })
        window.location.assign('/')

        return
      }

      await adminApi.anon('/v1/admin/auth/code', { email })
      setSent(true)
    } catch (err) {
      setError(err instanceof AdminApiError ? err.message : 'Something went wrong. Try again.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="grid min-h-svh place-items-center p-4">
      <Card className="w-full max-w-sm gap-6 p-6">
        <div className="flex items-center gap-2 text-foreground">
          <Logo wordmarkClassName="h-4" />
          <Badge>Admin</Badge>
        </div>
        {methods && !methods.email_code && !methods.access_token && (
          <p className="text-sm text-muted-foreground">
            No sign-in method is configured: set up mail (MAIL_SMTP_HOST) or run in local mode.
          </p>
        )}
        {methods?.email_code && (
          <form onSubmit={submit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="email">Email</Label>
              <Input
                id="email"
                type="email"
                autoComplete="email"
                required
                disabled={sent}
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </div>
            {sent && (
              <div className="space-y-2">
                <Label htmlFor="code">6-digit code</Label>
                <Input
                  id="code"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  pattern="[0-9]{6}"
                  maxLength={6}
                  required
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">If this address is an admin, a code is on its way.</p>
              </div>
            )}
            {error && (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}
            <Button type="submit" className="w-full" disabled={busy}>
              {sent ? 'Sign in' : 'Send code'}
            </Button>
          </form>
        )}
        {methods?.access_token && (
          <form onSubmit={submitToken} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="token">Access token</Label>
              <Input
                id="token"
                type="password"
                autoComplete="current-password"
                required
                value={token}
                onChange={(e) => setToken(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                The install&apos;s access token, printed at start or by `server token`.
              </p>
            </div>
            {!methods.email_code && error && (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}
            <Button type="submit" className="w-full" disabled={busy}>
              Sign in
            </Button>
          </form>
        )}
      </Card>
    </main>
  )
}
