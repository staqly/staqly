/**
 * The welcome wizard (/welcome) keeps two things in this tab while it runs, because connecting an OAuth platform
 * leaves the page: what the person said so far (their name and goal) and where the platform's callback should
 * send them back. Nothing here is a source of truth: the server stores whether the wizard is done.
 */
export type WelcomeGoal = 'creator' | 'team' | 'agents'

export interface WelcomeDraft {
  name?: string
  goal?: WelcomeGoal
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

  return path && path.startsWith('/') && !path.startsWith('//') ? path : null
}

export const clearOAuthReturn = () => write(RETURN, null)
