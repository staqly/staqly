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
      {/* The initials are drawn by CSS, not text: the name next to the picture says it, so a button holding the
          avatar (the account menu) is named by that name alone. */}
      <AvatarFallback aria-hidden data-initials={initials} className="text-xs before:content-[attr(data-initials)]" />
    </Avatar>
  )
}
