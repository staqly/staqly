/**
 * The welcome wizard (/welcome) keeps two things in this tab while it runs, because Stripe Checkout and an OAuth
 * platform leave the page: what the person chose so far (the draft) and where the platform's callback should send
 * them back. Nothing here is a source of truth: the server stores whether the wizard is done.
 */
import { safeNextPath } from './safe'

export type WelcomeGoal = 'creator' | 'team' | 'agents'

/**
 * Everything the wizard collects before the workspace has a plan; nothing is written until the end (after
 * Stripe Checkout when there is no plan yet), then `applied` keeps a reload or an OAuth return from writing twice.
 */
export interface WelcomeDraft {
  name?: string
  goal?: WelcomeGoal
  workspaceName?: string
  /** Named in the New workspace dialog: the wizard skips its Name your workspace step. */
  named?: boolean
  /** Platforms to connect once the workspace is ready (provider ids). */
  providers?: string[]
  agent?: 'claude' | 'codex'
  /** The agent's key was made in the agent step (the workspace could write then): the end doesn't make another. */
  keyMade?: boolean
  invites?: { emails: string[]; role: 'admin' | 'operator' | 'editor' | 'viewer' }
  /** The workspace this draft belongs to: a draft from another workspace is dropped. */
  workspace?: string
  // The agent key itself stays in memory only (lib/session-key.ts), never in storage.
  applied?: { keyCreated: boolean; invited: number; failed: string[] }
}

const DRAFT = 'staqly:welcome'
const RETURN = 'staqly:oauth-return'

function read<T>(key: string): T | null {
  try {
    const raw = sessionStorage.getItem(key)

    return raw ? (JSON.parse(raw) as T) : null
  } catch {
    return null
  }
}

function write(key: string, value: unknown) {
  try {
    if (value === null) {
      sessionStorage.removeItem(key)
    } else {
      sessionStorage.setItem(key, JSON.stringify(value))
    }
  } catch {
    // Blocked storage: the wizard still works, it just forgets the draft on an OAuth round trip.
  }
}

export const readWelcomeDraft = () => read<WelcomeDraft>(DRAFT) ?? {}
export const writeWelcomeDraft = (d: WelcomeDraft | null) => write(DRAFT, d)

/** Where an OAuth callback goes instead of Channels (set right before leaving for the platform). */
export const setOAuthReturn = (path: string) => write(RETURN, path)

/** The path the OAuth callback should return to (a same-site path only). */
export function peekOAuthReturn(): string | null {
  const path = read<string>(RETURN)

  return path ? safeNextPath(path, 'http://localhost', '') || null : null
}

export const clearOAuthReturn = () => write(RETURN, null)
