import { cn } from '@/lib/utils'

export function SettingsCard({
  title,
  description,
  children,
  icon,
  action,
  className,
  tone,
}: {
  className?: string
  /** "danger" outlines the card in red (destructive actions). */
  tone?: 'danger'
  title: string
  description?: React.ReactNode
  children: React.ReactNode
  icon?: React.ReactNode
  action?: React.ReactNode
}) {
  return (
    <section
      className={cn('rounded-xl border bg-card', tone === 'danger' && 'border-rose-500/40', className)}
      aria-label={title}
    >
      <div
        className={cn('flex flex-wrap items-start gap-3 border-b px-4 py-3', tone === 'danger' && 'border-rose-500/30')}
      >
        {icon && (
          <span className={cn('mt-0.5 text-muted-foreground [&_svg]:size-4', tone === 'danger' && 'text-rose-600')}>
            {icon}
          </span>
        )}
        <div className="min-w-0 flex-1 basis-48">
          <h2 className="text-sm font-semibold">{title}</h2>
          {description && <p className="text-sm text-muted-foreground">{description}</p>}
        </div>
        {action && <div className="shrink-0">{action}</div>}
      </div>
      <div className="p-4">{children}</div>
    </section>
  )
}
