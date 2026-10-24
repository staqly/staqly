'use client'

import { Check } from 'lucide-react'

import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import type { MyWorkspace } from '@/lib/api'

import { WorkspaceTile } from './workspace-tile'

/** Every workspace with a search, for people in more than the menu shows. */
export function WorkspaceSwitcherDialog({
  open,
  onOpenChange,
  workspaces,
  pending,
  onPick,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  workspaces: MyWorkspace[]
  pending: boolean
  onPick: (id: string) => void
}) {
  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Switch workspace"
      description="Search your workspaces by name"
    >
      <Command label="Search workspaces">
        <CommandInput placeholder="Search workspaces" />
        <CommandList>
          <CommandEmpty>No workspace with that name.</CommandEmpty>
          <CommandGroup heading={`${workspaces.length} workspaces`}>
            {workspaces.map((w) => (
              <CommandItem
                key={w.id}
                value={`${w.name} ${w.id}`}
                disabled={pending}
                onSelect={() => (w.current ? onOpenChange(false) : onPick(w.id))}
                aria-current={w.current || undefined}
              >
                <WorkspaceTile name={w.name} />
                <span className="min-w-0 flex-1 truncate" title={w.name}>
                  {w.name}
                </span>
                {w.current && (
                  <>
                    <span className="text-xs text-muted-foreground">Current</span>
                    <Check />
                  </>
                )}
              </CommandItem>
            ))}
          </CommandGroup>
        </CommandList>
      </Command>
    </CommandDialog>
  )
}
