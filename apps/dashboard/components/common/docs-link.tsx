export const DOCS_URL = 'https://docs.staqly.ai'

/** A link into docs.staqly.ai, underlined until hovered (the style of every inline link). */
export function DocsLink({ path, children }: { path: string; children: React.ReactNode }) {
  return (
    <a
      href={`${DOCS_URL}${path}`}
      target="_blank"
      rel="noopener noreferrer"
      className="text-foreground underline underline-offset-2 hover:no-underline"
    >
      {children}
    </a>
  )
}
