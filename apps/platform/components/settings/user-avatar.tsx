import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import { safeImageSrc } from '@/lib/safe'
import { cn } from '@/lib/utils'

/** A person's picture (uploaded, or from Google), or their initials. */
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
  const picture = safeImageSrc(src)

  return (
    <Avatar className={cn('size-8', className)} data-logo={logo ? '' : undefined}>
      {picture && <AvatarImage src={picture} alt="" referrerPolicy="no-referrer" />}
      {/* The name next to the picture says it: a button holding the avatar (the account menu) is named by the name. */}
      <AvatarFallback aria-hidden className="text-xs">
        {initials}
      </AvatarFallback>
    </Avatar>
  )
}
