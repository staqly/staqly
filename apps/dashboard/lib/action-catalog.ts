import type { ActionDef, ItemStatus } from './api'

export type CatalogActionName = 'regenerate' | 'publish' | 'retry' | 'archive'

/** A collection action from the fixed catalog (apps/api/internal/app/collections.go, actionCatalog). */
export interface CatalogAction {
  name: CatalogActionName
  label: string
  /** The event it sends to webhooks. */
  event: string
  /** One line on what it does, for the collection settings. */
  description: string
  /** The run it starts; null when it doesn't start one. */
  runLabels: { running: string; done: string; failed: string } | null
  /** Statuses a collection may choose from; null when where it runs from is fixed. */
  options: readonly ItemStatus[] | null
  /** The default (or fixed) statuses it runs from; empty means any status. */
  defaultFrom: readonly ItemStatus[]
  /** Always enabled: every collection keeps approve, publish, done or failed, plus archive. */
  required: boolean
  /** May start on its own right after an approval (auto_run). */
  autoRunnable: boolean
}

export const ACTION_CATALOG: readonly CatalogAction[] = [
  {
    name: 'regenerate',
    label: 'Regenerate',
    event: 'item.regenerate_requested',
    description: 'Asks your automation for a new version, with the reviewer’s feedback.',
    runLabels: { running: 'Regenerating', done: 'Regenerated', failed: 'Regeneration failed' },
    options: ['rejected', 'failed', 'expired'],
    defaultFrom: ['rejected', 'failed'],
    required: false,
    autoRunnable: false,
  },
  {
    name: 'publish',
    label: 'Publish',
    event: 'item.publish_requested',
    description: 'Asks your automation to publish or deliver the approved item.',
    runLabels: { running: 'Publishing', done: 'Published', failed: 'Publish failed' },
    options: ['approved', 'completed'],
    defaultFrom: ['approved'],
    required: true,
    autoRunnable: true,
  },
  {
    name: 'retry',
    label: 'Retry',
    event: 'item.retry_requested',
    description: 'Runs the failed step again.',
    runLabels: { running: 'Retrying', done: 'Done', failed: 'Retry failed' },
    options: null,
    defaultFrom: ['failed'],
    required: false,
    autoRunnable: false,
  },
  {
    name: 'archive',
    label: 'Archive',
    event: 'item.archived',
    description: 'Moves the item out of the way; it can be unarchived later.',
    runLabels: null,
    options: null,
    defaultFrom: [],
    required: true,
    autoRunnable: false,
  },
]

/** Whether approving starts the publish run at once (publish auto_run, allowed from approved). */
export function publishesOnApproval(collection?: { actions?: ActionDef[] } | null): boolean {
  const publish = collection?.actions?.find((a) => a.name === 'publish')

  return !!publish?.auto_run && (!publish.allowed_statuses?.length || publish.allowed_statuses.includes('approved'))
}
