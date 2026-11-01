import type { Role } from '@/lib/api'

/** How a role reads in the dashboard. */
export const ROLE_LABELS: Record<Role, string> = {
  owner: 'Owner',
  admin: 'Admin',
  operator: 'Operator',
  editor: 'Editor',
  viewer: 'Viewer',
}
