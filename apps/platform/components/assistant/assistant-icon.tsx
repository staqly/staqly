'use client'

import { Sparkles } from 'lucide-react'
import { useId } from 'react'

/** The AI Assistant's sparkles in a green-to-violet gradient, so it stands apart from the plain icons around it. */
export function AssistantIcon({ className }: { className?: string }) {
  const id = useId()

  return (
    <>
      <svg aria-hidden width="0" height="0" className="absolute">
        <defs>
          {/* userSpaceOnUse: one gradient across the whole 24 x 24 icon, not one per path. */}
          <linearGradient id={id} x1="0" y1="24" x2="24" y2="0" gradientUnits="userSpaceOnUse">
            <stop offset="0" stopColor="#22c55e" />
            <stop offset="0.5" stopColor="#3b82f6" />
            <stop offset="1" stopColor="#a855f7" />
          </linearGradient>
        </defs>
      </svg>
      <Sparkles className={className} style={{ stroke: `url("#${id}")` }} />
    </>
  )
}
