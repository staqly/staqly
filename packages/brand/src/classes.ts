/**
 * The classes of a brand component: `base`, then each default that `className` doesn't override, then `className`.
 * A default gives way to any class of its kind, by prefix (`size-9` to `size-5`, `rounded-full` to `rounded-md`),
 * the only overrides callers pass. Replaces tailwind-merge here, which is ~11 KB of client JS on the landing page
 * and the docs.
 */
export function classes(base: string, defaults: string[], className?: string): string {
  const own = className?.split(/\s+/).filter(Boolean) ?? []
  const kept = defaults.filter((d) => {
    const kind = d.slice(0, d.indexOf('-') + 1) || d

    return !own.some((c) => c.startsWith(kind))
  })

  return [base, ...kept, ...own].filter(Boolean).join(' ')
}
