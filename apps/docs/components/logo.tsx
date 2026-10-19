import { Logo as BrandLogo, LogoMark as BrandLogoMark } from '@staqly/brand/logo'

import { env } from '@/lib/env.mjs'

// The shared staqly logo (packages/brand) for the docs: dark only, so only the dark colourway is loaded,
// and served under the base path.

export function LogoMark({ className }: { className?: string }) {
  return <BrandLogoMark className={className} basePath={env.basePath} colorway="dark" />
}

export function Logo() {
  return (
    <BrandLogo basePath={env.basePath} colorway="dark" markClassName="size-6" wordmarkClassName="text-[15px]">
      <span className="rounded-md border border-fd-border bg-fd-muted px-1.5 py-px text-[11px] font-medium text-fd-muted-foreground">
        docs
      </span>
    </BrandLogo>
  )
}
