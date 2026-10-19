import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import { safeHttpUrl } from '@/lib/safe'
import { cn } from '@/lib/utils'

/** A person's picture (from Google), or their initials. */
export function UserAvatar({
  name,
  src,
  className,
  logo,
}: {
  name: string
  src?: string
  className?: string
  /** Stays visible in the collapsed sidebar rail (like the logo). */
  logo?: boolean
}) {
  const initials =
    name
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((w) => w[0]?.toUpperCase())
      .join('') || '?'
  const picture = safeHttpUrl(src)

  return (
    <Avatar className={cn('size-8', className)} data-logo={logo ? '' : undefined}>
      {picture && <AvatarImage src={picture} alt="" referrerPolicy="no-referrer" />}
      <AvatarFallback className="text-xs">{initials}</AvatarFallback>
    </Avatar>
  )
}
