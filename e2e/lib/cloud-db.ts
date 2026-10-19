/**
 * Cloud mode runs on PostgreSQL. Suites that start a cloud server take the server from
 * STAQLY_E2E_DATABASE_URL (postgres://...; CI: a postgres service container) and give every run its own
 * schema through search_path, so runs never see each other's data. Local mode stays on SQLite.
 */
export function cloudDatabaseURL(prefix = 'ui'): string {
  const base = process.env.STAQLY_E2E_DATABASE_URL

  if (!base) {
    throw new Error('cloud mode needs PostgreSQL: set STAQLY_E2E_DATABASE_URL=postgres://... (see e2e/README.md)')
  }

  const url = new URL(base)

  url.searchParams.set('search_path', `${prefix}_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`)

  return url.toString()
}
