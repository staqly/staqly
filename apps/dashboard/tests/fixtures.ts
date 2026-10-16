import type { ActionDef, Collection, Item } from '@/lib/api'

export const APPROVE: ActionDef = {
  name: 'approve',
  label: 'Approve',
  event: 'item.approved',
  requires_feedback: false,
  set_status: 'approved',
  allowed_statuses: ['pending'],
  builtin: true,
}
export const REJECT: ActionDef = {
  name: 'reject',
  label: 'Reject',
  event: 'item.rejected',
  requires_feedback: false,
  set_status: 'rejected',
  allowed_statuses: ['pending'],
  builtin: true,
}
export const REGENERATE: ActionDef = {
  name: 'regenerate',
  label: 'Regenerate',
  event: 'item.regenerate_requested',
  requires_feedback: true,
  set_status: 'processing',
  allowed_statuses: ['pending', 'rejected', 'failed'],
}

/** decision/run/archived_at as the API derives them from a status (apps/api syncDecisionRun). */
function stateOf(status: Item['status']): Pick<Item, 'decision' | 'run' | 'archived_at'> {
  if (status === 'processing' || status === 'completed' || status === 'failed') {
    return { decision: 'approved', run: status, archived_at: null }
  }

  if (status === 'archived') {
    return { decision: 'approved', run: 'none', archived_at: '2026-01-01T12:00:00Z' }
  }

  return { decision: status === 'draft' ? null : status, run: 'none', archived_at: null }
}

export function item(over: Partial<Item> = {}): Item {
  return {
    ...stateOf(over.status ?? 'pending'),
    id: 'itm_1',
    collection_id: 'col_1',
    title: 'Avatar v1',
    type: 'image',
    content: '',
    url: '',
    media: {
      id: 'med_1',
      mime_type: 'image/png',
      size_bytes: 2048,
      original_name: 'avatar.png',
      url: 'http://localhost:3333/v1/media/med_1',
    },
    metadata: { prompt: 'studio portrait' },
    status: 'pending',
    source: 'n8n',
    external_ref: '',
    feedback: '',
    version: 1,
    decided_at: null,
    expires_at: null,
    processing_timeout_seconds: null,
    run_action: null,
    summary: '',
    tags: [],
    editable: [],
    created_at: '2026-01-01T10:00:00Z',
    updated_at: '2026-01-01T10:00:00Z',
    ...over,
  }
}

export function collection(over: Partial<Collection> = {}): Collection {
  return {
    id: 'col_1',
    name: 'Instagram Avatar',
    description: 'Avatars',
    is_default: false,
    icon: '',
    color: '',
    default_view: 'gallery',
    actions: [REGENERATE],
    archived: false,
    archived_at: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    item_count: 2,
    status_counts: { pending: 2 },
    ...over,
  }
}
