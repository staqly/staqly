// Cloud-mode helpers: workspaces from the operator CLI, admin sessions from
// recovery keys, signups and raw requests with an API key.
import { type HttpInit, type HttpResponse, http, Session } from './http.js'
import { API, server, SETUP_TOKEN } from './stack.js'
import { sleep } from './wait.js'

export interface WorkspaceRow {
  id: string
  plan: string
  name: string
}

/** `server workspaces`: id, plan, plan end, created, name (tab separated). */
export async function workspaces(): Promise<WorkspaceRow[]> {
  return (await server('workspaces'))
    .split('\n')
    .filter(Boolean)
    .map((line) => {
      const cols = line.split('\t')

      return { id: cols[0]!, plan: cols[1]!, name: cols.at(-1)! }
    })
}

export interface Admin {
  workspaceId: string
  /** Signed-in browser session with its CSRF token. */
  session: Session
  /** Admin API key issued by the recovery. */
  key: string
}

/**
 * Signs in to a workspace the way an operator does after losing access: a
 * recovery key from `server recovery-key <ws>`, redeemed for a session and an
 * admin key.
 */
export async function adminFor(workspaceId: string): Promise<Admin> {
  const recoveryKey = await server('recovery-key', workspaceId)
  const session = new Session(API)
  const res = await session.request('/v1/sessions/recover', { method: 'POST', json: { recovery_key: recoveryKey } })

  if (res.status !== 201) {
    throw new Error(`recovery for ${workspaceId} failed: ${res.status} ${res.text()}`)
  }

  const body = res.json<{ csrf_token: string; api_key: { key: string } }>()

  session.csrf = body.csrf_token

  return { workspaceId, session, key: body.api_key.key }
}

export interface Signup {
  workspaceId: string
  key: string
  recoveryKey: string
}

export async function signup(name: string): Promise<Signup> {
  const res = await http(`${API}/v1/workspaces`, { method: 'POST', json: { name, setup_token: SETUP_TOKEN } })

  if (res.status !== 201) {
    throw new Error(`signup failed: ${res.status} ${res.text()}`)
  }

  const body = res.json<{ workspace: { id: string }; api_key: { key: string }; recovery_key: string }>()

  return { workspaceId: body.workspace.id, key: body.api_key.key, recoveryKey: body.recovery_key }
}

/**
 * A raw API request with a bearer key (null: no credentials). Like the SDK,
 * it waits out 429 answers (Retry-After), so long tests stay under the
 * per-key rate limit without hiding any other status.
 */
export async function withKey(key: string | null, path: string, init: HttpInit = {}): Promise<HttpResponse> {
  const headers: Record<string, string> = { ...init.headers }

  if (key) {
    headers.Authorization = `Bearer ${key}`
  }

  for (let attempt = 0; ; attempt++) {
    const res = await http(`${API}${path}`, { ...init, headers })

    if (res.status !== 429 || attempt >= 10) {
      return res
    }

    await sleep(Number(res.headers['retry-after'] ?? '1') * 1000)
  }
}

/** Creates an API key with the given scopes using an admin key. */
export async function createKey(adminKey: string, scopes: string[], extra: Record<string, unknown> = {}) {
  const res = await withKey(adminKey, '/v1/api-keys', {
    method: 'POST',
    json: { name: `e2e ${scopes.join(' ')}`, scopes, ...extra },
  })

  if (res.status !== 201) {
    throw new Error(`creating a key with ${scopes.join(',')} failed: ${res.status} ${res.text()}`)
  }

  return res.json<{ id: string; key: string; scopes: string[]; prefix: string }>()
}
