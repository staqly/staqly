import { cn } from '@/lib/utils'

import { CopyButton } from './copy-button'

export function CodeBlock({
  code,
  title,
  className,
  language,
}: {
  code: string
  title?: string
  className?: string
  language?: string
}) {
  const header = !!(title || language)
  const copy = <CopyButton value={code} label={title ? `Copy ${title}` : 'Copy code'} size="icon-xs" />

  return (
    <div className={cn('group/code relative overflow-hidden rounded-lg border bg-muted/40', className)}>
      {/* With a header, Copy sits in it on the right; without one it floats over the code. */}
      {header && (
        <div className="flex h-8 items-center gap-2 border-b bg-muted/50 pr-1 pl-3 text-xs text-muted-foreground">
          <span className="font-medium">{title}</span>
          {language && <span className="ml-auto font-mono tracking-wide uppercase opacity-70">{language}</span>}
          <span className={cn(!language && 'ml-auto')}>{copy}</span>
        </div>
      )}
      <pre
        className={cn('scrollbar-thin overflow-x-auto p-3 font-mono text-[12.5px] leading-relaxed', !header && 'pr-12')}
      >
        <code>{code}</code>
      </pre>
      {!header && <div className="absolute top-1.5 right-1.5 rounded-md bg-background/70 backdrop-blur">{copy}</div>}
    </div>
  )
}

export function InlineCode({ children, className }: { children: React.ReactNode; className?: string }) {
  return <code className={cn('rounded bg-muted px-1 py-0.5 font-mono text-[0.85em]', className)}>{children}</code>
}
