import { Braces } from 'lucide-react'

import { DisclosureCard } from '@/components/common/disclosure-card'
import { JsonBlock } from '@/components/common/json-code'
import type { Item } from '@/lib/api'

/** An item's metadata as JSON in the same expandable card as feedback and event data, with Copy and full screen. */
export function MetadataCard({ metadata, defaultOpen = true }: { metadata: Item['metadata']; defaultOpen?: boolean }) {
  const entries = Object.entries(metadata ?? {})

  if (!entries.length) {
    return null
  }

  return (
    <DisclosureCard
      label="Metadata"
      copy={JSON.stringify(metadata, null, 2)}
      defaultOpen={defaultOpen}
      bodyTestId="metadata-body"
      header={
        <>
          <Braces aria-hidden className="size-3.5 shrink-0" />
          <span className="shrink-0 font-medium text-foreground/80">Metadata</span>
        </>
      }
      preview={<span className="min-w-0 truncate">{entries.map(([k]) => k).join(', ')}</span>}
      meta={<span>{entries.length}</span>}
    >
      {/* Metadata is a JSON object: shown as JSON, nested values included. */}
      <JsonBlock
        json={JSON.stringify(metadata, null, 2)}
        title="Metadata"
        className="scrollbar-thin max-h-80 overflow-auto px-3 py-2.5"
      />
    </DisclosureCard>
  )
}
