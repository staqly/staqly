export { cn } from 'cn'

/**
 * `pathname` with the current search params plus `patch` (a value sets the param, undefined removes it), for
 * `router.replace` after a filter or tab change. Drops the `?` when nothing is left.
 */
export function hrefWithParams(
  pathname: string,
  params: { toString(): string },
  patch: Record<string, string | undefined>,
): string {
  const sp = new URLSearchParams(params.toString())

  for (const [key, value] of Object.entries(patch)) {
    if (value) {
      sp.set(key, value)
    } else {
      sp.delete(key)
    }
  }

  const s = sp.toString()

  return s ? `${pathname}?${s}` : pathname
}
