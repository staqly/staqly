/** One spelling per tag, for posts and channels: no leading #, lowercase, spaces become dashes. */
export function normalizeTag(v: string): string {
  return v.trim().replace(/^#/, '').toLowerCase().replace(/\s+/g, '-')
}
