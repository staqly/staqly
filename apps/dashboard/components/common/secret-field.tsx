'use client'

import { Eye, EyeOff } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import { CopyButton } from './copy-button'

/** Monospace secret with copy (and optional mask/reveal). */
export function SecretField({
  value,
  label,
  masked = false,
  className,
}: {
  value: string
  label: string
  masked?: boolean
  className?: string
}) {
  const [revealed, setRevealed] = useState(!masked)

  return (
    <div
      className={cn(
        'flex items-center gap-1 rounded-lg border bg-muted/40 py-1 pr-1 pl-3 font-mono text-[13px]',
        className,
      )}
    >
      <span className="min-w-0 flex-1 break-all select-all" data-testid={`secret-${label}`}>
        {revealed ? value : '•'.repeat(Math.min(32, value.length))}
      </span>
      {masked && (
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={revealed ? `Hide ${label}` : `Reveal ${label}`}
          onClick={() => setRevealed((v) => !v)}
        >
          {revealed ? <EyeOff /> : <Eye />}
        </Button>
      )}
      <CopyButton value={value} label={`Copy ${label}`} />
    </div>
  )
}
