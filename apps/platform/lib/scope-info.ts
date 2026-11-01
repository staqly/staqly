import type { Scope } from '@/lib/api'

export const SCOPE_INFO: Record<Scope, string> = {
  'collections:read': 'List and read collections',
  'collections:write': 'Create, rename and archive collections',
  'collections:delete': 'Permanently delete collections (their posts move to Default)',
  'posts:read': 'List, search and read posts',
  'posts:write': 'Create and edit posts',
  'posts:delete': 'Delete posts',
  'posts:publish': 'Schedule, publish, cancel and report results for posts',
  'channels:read': 'List channels',
  'channels:manage': 'Connect and remove channels (they hold sign-ins)',
  'schedules:manage': 'Create and change collection schedules',
  'media:read': 'Download media files',
  'media:write': 'Upload and delete media files',
  'webhooks:manage': 'Manage webhooks and deliveries',
  'alerts:manage': 'Manage alerts (they hold chat tokens)',
  'integrations:manage': 'Manage your integration keys and connected agents (sign-in required)',
  'events:read': 'Read the activity log',
  admin: 'Manage API keys, sessions and workspace',
}

/** The heading a scope is listed under: what it touches ("posts:publish" goes under Posts). */
export function scopeGroup(scope: string) {
  const area = scope.split(':')[0] ?? scope

  return area.charAt(0).toUpperCase() + area.slice(1)
}
