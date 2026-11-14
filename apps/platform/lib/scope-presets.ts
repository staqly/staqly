import { ALL_SCOPES, type Scope } from '@/lib/api'

/** Ready-made scope sets for a new API key (Integrations > API keys and the welcome wizard's agent step). */
export const SCOPE_PRESETS: { id: string; label: string; description: string; scopes: Scope[] }[] = [
  {
    id: 'content',
    label: 'Content agent',
    description: 'Write posts, add media, schedule and publish them',
    scopes: ['collections:read', 'posts:read', 'posts:write', 'posts:publish', 'media:read', 'media:write'],
  },
  {
    id: 'publisher',
    label: 'Publisher automation',
    description: 'Picks up posts for a custom channel and reports the result',
    scopes: ['posts:read', 'posts:publish'],
  },
  {
    id: 'readonly',
    label: 'Read-only agent',
    description: 'Look things up, change nothing',
    scopes: ['collections:read', 'posts:read', 'channels:read', 'events:read'],
  },
  {
    id: 'full',
    label: 'Full access',
    description: 'Every scope, including admin',
    scopes: [...ALL_SCOPES],
  },
]
