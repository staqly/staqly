'use client'

import { ArrowRight } from 'lucide-react'
import { useState } from 'react'

import { TerminalCard } from '@/components/terminal'

export type AgentSetup = { name: string; title: string; code: string; guide: string }

/** One install snippet at a time, picked with pill tabs; switching replays the terminal animation. */
export function AgentTabs({ agents }: { agents: AgentSetup[] }) {
  const [active, setActive] = useState(0)
  const a = agents[active]

  return (
    <div className="mt-10">
      {/* Toggle buttons, not ARIA tabs: one snippet is shown, the pressed button says which. */}
      <fieldset className="flex flex-wrap gap-2">
        <legend className="sr-only">Agent</legend>
        {agents.map((x, i) => (
          <button
            key={x.name}
            type="button"
            aria-pressed={i === active}
            onClick={() => setActive(i)}
            className={`rounded-full border px-4 py-1.5 text-sm transition-colors pointer-coarse:py-2.5 ${
              i === active
                ? 'border-border bg-accent text-foreground'
                : 'border-border/60 text-muted-foreground hover:text-foreground'
            }`}
          >
            {x.name}
          </button>
        ))}
      </fieldset>
      <div className="mt-5">
        <TerminalCard key={a.name} title={a.title} code={a.code} />
      </div>
      <p className="mt-4 text-sm text-muted-foreground">
        Self-hosted? Point it at{' '}
        <code className="rounded bg-muted px-1 font-mono text-[0.9em]">localhost:3333/mcp</code> and skip the key.{' '}
        <a
          href={a.guide}
          className="inline-flex items-center gap-1 font-medium text-brand-strong hover:underline pointer-coarse:-my-2.5 pointer-coarse:py-2.5"
        >
          Full guide <ArrowRight className="size-3.5" />
        </a>
      </p>
    </div>
  )
}
