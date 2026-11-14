import { cn } from '@/lib/utils'

export function PageHeader({
  title,
  description,
  actions,
  icon,
  className,
}: {
  title: React.ReactNode
  description?: React.ReactNode
  actions?: React.ReactNode
  icon?: React.ReactNode
  className?: string
}) {
  // Laid out by the page's own width (a container), not the window's: beside the assistant panel a wide window
  // still leaves a narrow page, where the actions go under the title instead of squeezing it letter by letter.
  return (
    <div className={cn('@container', className)}>
      <div className="flex flex-col gap-3 @xl:flex-row @xl:items-start @xl:justify-between">
        <div className="flex min-w-0 items-start gap-3">
          {icon && <PageIcon>{icon}</PageIcon>}
          <div className="min-w-0">
            <h1
              className="truncate text-xl font-semibold tracking-tight"
              title={typeof title === 'string' ? title : undefined}
            >
              {title}
            </h1>
            {description && <p className={cn(pageSubtitle, 'wrap-break-word')}>{description}</p>}
          </div>
        </div>
        {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
      </div>
    </div>
  )
}

export function PageContainer({ children, className }: { children: React.ReactNode; className?: string }) {
  return <div className={cn('mx-auto w-full max-w-6xl px-4 py-6 sm:px-6 lg:px-8', className)}>{children}</div>
}

/** The line under a page title: with the title 28 + 2 + 18 = 48 px, which PageIcon (42 px, 3 px down) sits centred on. */
export const pageSubtitle = 'mt-0.5 text-[13px] leading-[18px] text-muted-foreground'

/** The boxed icon beside every page title (PageHeader, and pages that build their own header row), centred on the
 * title and its subtitle together. */
export function PageIcon({ children }: { children: React.ReactNode }) {
  return (
    <div className="mt-[3px] flex size-[42px] shrink-0 items-center justify-center rounded-xl border bg-card text-muted-foreground shadow-xs [&_svg]:size-5">
      {children}
    </div>
  )
}
