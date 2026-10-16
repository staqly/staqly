'use client'

import { ListFilter } from 'lucide-react'

import { TYPE_LABELS } from '@/components/common/type-icon'
import { TagFilter } from '@/components/items/item-filters'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ITEM_TYPES, type Collection } from '@/lib/api'

export interface InboxFilters {
  collection: string
  source: string
  tags: string[]
  type: string
}

/**
 * Everything that narrows the list besides the step, behind one button: collection, source (the agent, n8n, a
 * watcher), tags and type. The button counts what is on.
 */
export function FilterPopover({
  value,
  collections,
  sources,
  onChange,
  onClear,
}: {
  value: InboxFilters
  collections: Collection[]
  sources: string[]
  onChange: (key: 'collection' | 'source' | 'tag' | 'type', v: string | undefined) => void
  onClear: () => void
}) {
  const on = [value.collection, value.source, value.tags.length ? 'x' : '', value.type].filter(Boolean).length

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button size="sm" variant="outline" aria-label="Filter">
          <ListFilter /> Filter
          {on > 0 && (
            <span className="rounded-full bg-brand px-1.5 text-[10px] text-brand-foreground tabular-nums">{on}</span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="grid w-72 gap-3 p-3">
        <div className="grid gap-1.5">
          <Label className="text-xs">Collection</Label>
          <Select
            value={value.collection || 'all'}
            onValueChange={(v) => onChange('collection', v === 'all' ? undefined : v)}
          >
            <SelectTrigger size="sm" className="w-full" aria-label="Collection filter">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All collections</SelectItem>
              {collections.map((c) => (
                <SelectItem key={c.id} value={c.id}>
                  {c.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="grid gap-1.5">
          <Label className="text-xs">Source</Label>
          <Select value={value.source || 'all'} onValueChange={(v) => onChange('source', v === 'all' ? undefined : v)}>
            <SelectTrigger size="sm" className="w-full" aria-label="Source filter">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All sources</SelectItem>
              {sources.map((s) => (
                <SelectItem key={s} value={s}>
                  {s.replace(/^watcher:/, 'Watcher: ')}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="grid gap-1.5">
          <Label className="text-xs">Tags</Label>
          <TagFilter value={value.tags} onChange={(v) => onChange('tag', v.join(',') || undefined)} />
        </div>
        <div className="grid gap-1.5">
          <Label className="text-xs">Type</Label>
          <Select value={value.type || 'all'} onValueChange={(v) => onChange('type', v === 'all' ? undefined : v)}>
            <SelectTrigger size="sm" className="w-full" aria-label="Type filter">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All types</SelectItem>
              {ITEM_TYPES.map((t) => (
                <SelectItem key={t} value={t}>
                  {TYPE_LABELS[t]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {on > 0 && (
          <Button size="sm" variant="ghost" onClick={onClear}>
            Clear filters
          </Button>
        )}
      </PopoverContent>
    </Popover>
  )
}
