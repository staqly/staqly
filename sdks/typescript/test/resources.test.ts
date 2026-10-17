import { describe, expect, it } from 'vitest'

import type { Staqly } from '../src/index.js'
import { client, item, json } from './helpers.js'

interface Case {
  name: string
  call: (c: Staqly) => Promise<unknown>
  reply?: Response
  method: string
  path: string
  query?: Record<string, string>
  body?: unknown
  result?: unknown
}

const list = (data: unknown[]) => json({ data })
const empty = () => new Response(null, { status: 204 })

const cases: Case[] = [
  // collections
  {
    name: 'collections.list',
    call: (c) => c.collections.list(),
    reply: list([{ id: 'col_1' }]),
    method: 'GET',
    path: '/v1/collections',
    result: [{ id: 'col_1' }],
  },
  {
    name: 'collections.list archived',
    call: (c) => c.collections.list({ include_archived: true }),
    reply: list([]),
    method: 'GET',
    path: '/v1/collections',
    query: { include_archived: 'true' },
  },
  {
    name: 'collections.get by name',
    call: (c) => c.collections.get('Instagram posts'),
    method: 'GET',
    path: '/v1/collections/Instagram%20posts',
  },
  {
    name: 'collections.create',
    call: (c) => c.collections.create({ name: 'A', color: '#fff' }),
    method: 'POST',
    path: '/v1/collections',
    body: { name: 'A', color: '#fff' },
  },
  {
    name: 'collections.update',
    call: (c) => c.collections.update('col_1', { name: 'B' }),
    method: 'PATCH',
    path: '/v1/collections/col_1',
    body: { name: 'B' },
  },
  {
    name: 'collections.setActions',
    call: (c) =>
      c.collections.setActions('col_1', [
        { name: 'regenerate', allowed_statuses: ['rejected'] },
        { name: 'publish', auto_run: true },
      ]),
    method: 'PATCH',
    path: '/v1/collections/col_1',
    body: {
      actions: [
        { name: 'regenerate', allowed_statuses: ['rejected'] },
        { name: 'publish', auto_run: true },
      ],
    },
  },
  {
    name: 'collections.setActions (round trip of a GET)',
    call: (c) =>
      c.collections.setActions('col_1', [
        {
          name: 'publish',
          label: 'Publish',
          event: 'item.publish_requested',
          requires_feedback: false,
          set_status: 'processing',
          allowed_statuses: ['approved'],
          auto_run: false,
        },
      ]),
    method: 'PATCH',
    path: '/v1/collections/col_1',
    body: {
      actions: [
        {
          name: 'publish',
          label: 'Publish',
          event: 'item.publish_requested',
          requires_feedback: false,
          set_status: 'processing',
          allowed_statuses: ['approved'],
          auto_run: false,
        },
      ],
    },
  },
  {
    name: 'collections.archive',
    call: (c) => c.collections.archive('col_1'),
    method: 'PATCH',
    path: '/v1/collections/col_1',
    body: { archived: true },
  },
  {
    name: 'collections.unarchive',
    call: (c) => c.collections.unarchive('col_1'),
    method: 'PATCH',
    path: '/v1/collections/col_1',
    body: { archived: false },
  },
  {
    name: 'collections.delete',
    call: (c) => c.collections.delete('col_1', 'A'),
    reply: empty(),
    method: 'DELETE',
    path: '/v1/collections/col_1',
    query: { confirm: 'A' },
    result: undefined,
  },
  // items
  {
    name: 'items.list',
    call: (c) => c.items.list({ status: 'pending', limit: 10 }),
    reply: json({ data: [] }),
    method: 'GET',
    path: '/v1/items',
    query: { status: 'pending', limit: '10' },
  },
  {
    name: 'items.list collection id',
    call: (c) => c.items.list({ collection: 'col_9' }),
    reply: json({ data: [] }),
    method: 'GET',
    path: '/v1/items',
    query: { collection_id: 'col_9' },
  },
  {
    name: 'items.search',
    call: (c) => c.items.search('sunset', { type: 'image' }),
    reply: json({ data: [] }),
    method: 'GET',
    path: '/v1/items',
    query: { q: 'sunset', type: 'image' },
  },
  { name: 'items.get', call: (c) => c.items.get('itm_1'), method: 'GET', path: '/v1/items/itm_1' },
  {
    name: 'items.create',
    call: (c) =>
      c.items.create('Posts', {
        title: 'x',
        content: { a: 1 },
        metadata: { k: 1 },
        expires_in: '24h',
        editable: ['content'],
      }),
    method: 'POST',
    path: '/v1/collections/Posts/items',
    body: { title: 'x', content: { a: 1 }, metadata: { k: 1 }, expires_in: '24h', editable: ['content'] },
  },
  {
    name: 'items.update',
    call: (c) => c.items.update('itm_1', { status: 'completed', metadata_patch: { post_url: 'u' } }),
    method: 'PATCH',
    path: '/v1/items/itm_1',
    body: { status: 'completed', metadata_patch: { post_url: 'u' } },
  },
  {
    name: 'items.delete',
    call: (c) => c.items.delete('itm_1'),
    reply: empty(),
    method: 'DELETE',
    path: '/v1/items/itm_1',
  },
  {
    name: 'items.approve',
    call: (c) => c.items.approve('itm_1', { feedback: 'ok', edits: { title: 'T' } }),
    method: 'POST',
    path: '/v1/items/itm_1/actions/approve',
    body: { feedback: 'ok', edits: { title: 'T' } },
  },
  {
    name: 'items.approve no body',
    call: (c) => c.items.approve('itm_1'),
    method: 'POST',
    path: '/v1/items/itm_1/actions/approve',
    body: {},
  },
  {
    name: 'items.reject',
    call: (c) => c.items.reject('itm_1', { feedback: 'too dark' }),
    method: 'POST',
    path: '/v1/items/itm_1/actions/reject',
    body: { feedback: 'too dark' },
  },
  {
    name: 'items.action',
    call: (c) =>
      c.items.action('itm_1', 'regenerate', { feedback: 'brighter', payload: { seed: 1 }, expected_version: 2 }),
    method: 'POST',
    path: '/v1/items/itm_1/actions/regenerate',
    body: { feedback: 'brighter', payload: { seed: 1 }, expected_version: 2 },
  },
  {
    name: 'items.listActions',
    call: (c) => c.items.listActions('itm_1'),
    reply: list([{ name: 'approve' }]),
    method: 'GET',
    path: '/v1/items/itm_1/actions',
    result: [{ name: 'approve' }],
  },
  {
    name: 'items.bulkAction',
    call: (c) => c.items.bulkAction(['a', 'b'], 'reject', { feedback: 'no' }),
    reply: list([{ id: 'a', ok: true }]),
    method: 'POST',
    path: '/v1/items/bulk-actions',
    body: { ids: ['a', 'b'], action: 'reject', feedback: 'no' },
    result: [{ id: 'a', ok: true }],
  },
  {
    name: 'items.history',
    call: (c) => c.items.history('itm_1', { limit: 5 }),
    reply: json({ data: [] }),
    method: 'GET',
    path: '/v1/items/itm_1/events',
    query: { limit: '5' },
  },
  {
    name: 'items.versions',
    call: (c) => c.items.versions('itm_1'),
    reply: list([]),
    method: 'GET',
    path: '/v1/items/itm_1/versions',
    result: [],
  },
  {
    name: 'items.wait',
    call: (c) => c.items.wait('itm_1', { timeout: 5 }),
    reply: json({ decided: false, item: item() }),
    method: 'GET',
    path: '/v1/items/itm_1/wait',
    query: { timeout: '5' },
  },
  {
    name: 'items.reportResult',
    call: (c) => c.items.reportResult('itm_1', { status: 'completed', result: { url: 'u' } }),
    method: 'POST',
    path: '/v1/items/itm_1/result',
    body: { status: 'completed', result: { url: 'u' } },
  },
  {
    name: 'items.list decision, run, tag, archived',
    call: (c) => c.items.list({ decision: ['approved', 'rejected'], run: 'failed', tag: ['a', 'b'], archived: false }),
    method: 'GET',
    path: '/v1/items',
    query: { decision: 'approved,rejected', run: 'failed', tag: 'a,b', archived: 'false' },
  },
  {
    name: 'items.reject follow_up',
    call: (c) => c.items.reject('itm_1', { feedback: 'redo', follow_up: 'regenerate' }),
    method: 'POST',
    path: '/v1/items/itm_1/actions/reject',
    body: { feedback: 'redo', follow_up: 'regenerate' },
  },
  {
    name: 'items.unarchive',
    call: (c) => c.items.unarchive('itm_1'),
    method: 'POST',
    path: '/v1/items/itm_1/unarchive',
  },
  // media
  {
    name: 'media.delete',
    call: (c) => c.media.delete('med_1'),
    reply: empty(),
    method: 'DELETE',
    path: '/v1/media/med_1',
  },
  // events
  {
    name: 'events.list',
    call: (c) => c.events.list({ type: 'item.approved', actor_type: 'api_key' }),
    reply: json({ data: [] }),
    method: 'GET',
    path: '/v1/events',
    query: { type: 'item.approved', actor_type: 'api_key' },
  },
  // webhooks
  {
    name: 'webhooks.list',
    call: (c) => c.webhooks.list(),
    reply: list([]),
    method: 'GET',
    path: '/v1/webhooks',
    result: [],
  },
  { name: 'webhooks.get', call: (c) => c.webhooks.get('wh_1'), method: 'GET', path: '/v1/webhooks/wh_1' },
  {
    name: 'webhooks.create',
    call: (c) => c.webhooks.create({ url: 'https://h', events: ['item.*'] }),
    method: 'POST',
    path: '/v1/webhooks',
    body: { url: 'https://h', events: ['item.*'] },
  },
  {
    name: 'webhooks.update',
    call: (c) => c.webhooks.update('wh_1', { active: false }),
    method: 'PATCH',
    path: '/v1/webhooks/wh_1',
    body: { active: false },
  },
  {
    name: 'webhooks.rotateSecret',
    call: (c) => c.webhooks.rotateSecret('wh_1'),
    method: 'PATCH',
    path: '/v1/webhooks/wh_1',
    body: { rotate_secret: true },
  },
  {
    name: 'webhooks.delete',
    call: (c) => c.webhooks.delete('wh_1'),
    reply: empty(),
    method: 'DELETE',
    path: '/v1/webhooks/wh_1',
  },
  {
    name: 'webhooks.test',
    call: (c) => c.webhooks.test('wh_1'),
    reply: json({ id: 'dlv_1' }, 202),
    method: 'POST',
    path: '/v1/webhooks/wh_1/test',
    result: { id: 'dlv_1' },
  },
  {
    name: 'webhooks.deliveries.list',
    call: (c) => c.webhooks.deliveries.list({ webhook_id: 'wh_1', status: 'failed' }),
    reply: list([]),
    method: 'GET',
    path: '/v1/webhook-deliveries',
    query: { webhook_id: 'wh_1', status: 'failed' },
  },
  {
    name: 'webhooks.deliveries.retry',
    call: (c) => c.webhooks.deliveries.retry('dlv_1'),
    reply: json({}, 202),
    method: 'POST',
    path: '/v1/webhook-deliveries/dlv_1/retry',
  },
  // watchers
  { name: 'watchers.list', call: (c) => c.watchers.list(), reply: list([]), method: 'GET', path: '/v1/watchers' },
  { name: 'watchers.get', call: (c) => c.watchers.get('wat_1'), method: 'GET', path: '/v1/watchers/wat_1' },
  {
    name: 'watchers.create',
    call: (c) => c.watchers.create({ name: 'HN', kind: 'rss', url: 'https://h/rss', collection: 'News', every: '1h' }),
    method: 'POST',
    path: '/v1/watchers',
    body: { name: 'HN', kind: 'rss', url: 'https://h/rss', collection: 'News', every: '1h' },
  },
  {
    name: 'watchers.update',
    call: (c) => c.watchers.update('wat_1', { keywords: ['ai'] }),
    method: 'PATCH',
    path: '/v1/watchers/wat_1',
    body: { keywords: ['ai'] },
  },
  {
    name: 'watchers.pause',
    call: (c) => c.watchers.pause('wat_1'),
    method: 'PATCH',
    path: '/v1/watchers/wat_1',
    body: { status: 'paused' },
  },
  {
    name: 'watchers.resume',
    call: (c) => c.watchers.resume('wat_1'),
    method: 'PATCH',
    path: '/v1/watchers/wat_1',
    body: { status: 'active' },
  },
  {
    name: 'watchers.delete',
    call: (c) => c.watchers.delete('wat_1'),
    reply: empty(),
    method: 'DELETE',
    path: '/v1/watchers/wat_1',
  },
  {
    name: 'watchers.run',
    call: (c) => c.watchers.run('wat_1'),
    reply: json({ status: 'ok', new_items: 2 }),
    method: 'POST',
    path: '/v1/watchers/wat_1/run',
    result: { status: 'ok', new_items: 2 },
  },
  // workspace, plans, keys
  { name: 'workspace.get', call: (c) => c.workspace.get(), method: 'GET', path: '/v1/workspace' },
  {
    name: 'workspace.rename',
    call: (c) => c.workspace.rename('Acme'),
    method: 'PATCH',
    path: '/v1/workspace',
    body: { name: 'Acme' },
  },
  { name: 'workspace.usage', call: (c) => c.workspace.usage(), method: 'GET', path: '/v1/workspace/usage' },
  {
    name: 'workspaces.create',
    call: (c) => c.workspaces.create({ name: 'New', setup_token: 't' }),
    method: 'POST',
    path: '/v1/workspaces',
    body: { name: 'New', setup_token: 't' },
  },
  {
    name: 'plans.list',
    call: (c) => c.plans.list(),
    reply: list([{ id: 'free' }]),
    method: 'GET',
    path: '/v1/plans',
    result: [{ id: 'free' }],
  },
  {
    name: 'apiKeys.list',
    call: (c) => c.apiKeys.list(),
    reply: json({ data: [{ id: 'key_1' }], available_scopes: [] }),
    method: 'GET',
    path: '/v1/api-keys',
    result: [{ id: 'key_1' }],
  },
  {
    name: 'apiKeys.create',
    call: (c) => c.apiKeys.create({ name: 'bot', scopes: ['items:read'], expires_at: '2027-01-01' }),
    method: 'POST',
    path: '/v1/api-keys',
    body: { name: 'bot', scopes: ['items:read'], expires_at: '2027-01-01' },
  },
  {
    name: 'apiKeys.revoke',
    call: (c) => c.apiKeys.revoke('key_1'),
    reply: empty(),
    method: 'DELETE',
    path: '/v1/api-keys/key_1',
  },
  {
    name: 'apiKeys.rotate',
    call: (c) => c.apiKeys.rotate('key_1'),
    reply: json({ id: 'key_2' }, 201),
    method: 'POST',
    path: '/v1/api-keys/key_1/rotate',
  },
  // notification channels
  {
    name: 'notificationChannels.list',
    call: (c) => c.notificationChannels.list(),
    reply: list([{ id: 'nch_1' }]),
    method: 'GET',
    path: '/v1/notification-channels',
    result: [{ id: 'nch_1' }],
  },
  {
    name: 'notificationChannels.get',
    call: (c) => c.notificationChannels.get('nch_1'),
    method: 'GET',
    path: '/v1/notification-channels/nch_1',
  },
  {
    name: 'notificationChannels.create',
    call: (c) =>
      c.notificationChannels.create({ name: 'Phone', type: 'ntfy', config: { topic: 't' }, events: ['item.created'] }),
    method: 'POST',
    path: '/v1/notification-channels',
    body: { name: 'Phone', type: 'ntfy', config: { topic: 't' }, events: ['item.created'] },
  },
  {
    name: 'notificationChannels.update',
    call: (c) => c.notificationChannels.update('nch_1', { digest_window_seconds: 60 }),
    method: 'PATCH',
    path: '/v1/notification-channels/nch_1',
    body: { digest_window_seconds: 60 },
  },
  {
    name: 'notificationChannels.enable',
    call: (c) => c.notificationChannels.enable('nch_1'),
    method: 'PATCH',
    path: '/v1/notification-channels/nch_1',
    body: { enabled: true },
  },
  {
    name: 'notificationChannels.disable',
    call: (c) => c.notificationChannels.disable('nch_1'),
    method: 'PATCH',
    path: '/v1/notification-channels/nch_1',
    body: { enabled: false },
  },
  {
    name: 'notificationChannels.delete',
    call: (c) => c.notificationChannels.delete('nch_1'),
    reply: empty(),
    method: 'DELETE',
    path: '/v1/notification-channels/nch_1',
  },
  {
    name: 'notificationChannels.test',
    call: (c) => c.notificationChannels.test('nch_1'),
    reply: json({ ok: false, error: 'x' }),
    method: 'POST',
    path: '/v1/notification-channels/nch_1/test',
    result: { ok: false, error: 'x' },
  },
  {
    name: 'notificationChannels.deliveries',
    call: (c) => c.notificationChannels.deliveries('nch_1', { limit: 5 }),
    reply: list([]),
    method: 'GET',
    path: '/v1/notification-channels/nch_1/deliveries',
    query: { limit: '5' },
    result: [],
  },
  // exports, purge, readiness
  {
    name: 'exports.createWorkspaceExport',
    call: (c) => c.exports.createWorkspaceExport(),
    reply: json({ id: 'exp_1', status: 'pending' }, 202),
    method: 'POST',
    path: '/v1/workspace/export',
  },
  {
    name: 'exports.getWorkspaceExport',
    call: (c) => c.exports.getWorkspaceExport('exp_1'),
    method: 'GET',
    path: '/v1/workspace/exports/exp_1',
  },
  {
    name: 'items.purge',
    call: (c) => c.items.purge('itm_1'),
    reply: json({ item_id: 'itm_1', events_deleted: 3, media_deleted: false }),
    method: 'POST',
    path: '/v1/items/itm_1/purge',
    result: { item_id: 'itm_1', events_deleted: 3, media_deleted: false },
  },
  { name: 'ready', call: (c) => c.ready(), reply: json({ status: 'ok', checks: {} }), method: 'GET', path: '/readyz' },
  {
    name: 'workspace.setPlan',
    call: (c) => c.workspace.setPlan('free'),
    reply: json({ plan: { id: 'free' } }),
    method: 'POST',
    path: '/v1/workspace/plan',
    body: { plan: 'free' },
    result: { plan: { id: 'free' } },
  },
  {
    name: 'workspace.rotateRecoveryKey',
    call: (c) => c.workspace.rotateRecoveryKey(),
    reply: json({ recovery_key: 'rk_new' }),
    method: 'POST',
    path: '/v1/recovery-key/rotate',
    result: { recovery_key: 'rk_new' },
  },
  {
    name: 'workspace.delete',
    call: (c) => c.workspace.delete('Acme'),
    reply: empty(),
    method: 'DELETE',
    path: '/v1/workspace',
    body: { confirm: 'Acme' },
    result: undefined,
  },
  // billing
  {
    name: 'billing.get',
    call: (c) => c.billing.get(),
    reply: json({ enabled: true, plan: 'pro', subscription_status: 'active', suspended: false }),
    method: 'GET',
    path: '/v1/billing',
    result: { enabled: true, plan: 'pro', subscription_status: 'active', suspended: false },
  },
  {
    name: 'billing.checkout',
    call: (c) => c.billing.checkout(),
    reply: json({ url: 'https://checkout.stripe.com/c/pay/cs_test_a1b2c3' }),
    method: 'POST',
    path: '/v1/billing/checkout',
    result: { url: 'https://checkout.stripe.com/c/pay/cs_test_a1b2c3' },
  },
  {
    name: 'billing.portal',
    call: (c) => c.billing.portal(),
    reply: json({ url: 'https://billing.stripe.com/p/session/test_a1b2c3' }),
    method: 'POST',
    path: '/v1/billing/portal',
    result: { url: 'https://billing.stripe.com/p/session/test_a1b2c3' },
  },
  // sessions, device links, decision links
  { name: 'sessions.current', call: (c) => c.sessions.current(), method: 'GET', path: '/v1/session' },
  {
    name: 'sessions.list',
    call: (c) => c.sessions.list(),
    reply: list([{ id: 'ses_1', current: true }]),
    method: 'GET',
    path: '/v1/sessions',
    result: [{ id: 'ses_1', current: true }],
  },
  {
    name: 'sessions.revoke',
    call: (c) => c.sessions.revoke('ses_1'),
    reply: empty(),
    method: 'DELETE',
    path: '/v1/sessions/ses_1',
  },
  { name: 'sessions.logout', call: (c) => c.sessions.logout(), reply: empty(), method: 'DELETE', path: '/v1/session' },
  {
    name: 'sessions.recover',
    call: (c) => c.sessions.recover('rk_1'),
    reply: json(
      { csrf_token: 'csrf', api_key: { id: 'key_1', key: 'stk_new', scopes: ['admin'] }, recovery_key: 'rk_2' },
      201,
    ),
    method: 'POST',
    path: '/v1/sessions/recover',
    body: { recovery_key: 'rk_1' },
    result: { csrf_token: 'csrf', api_key: { id: 'key_1', key: 'stk_new', scopes: ['admin'] }, recovery_key: 'rk_2' },
  },
  {
    name: 'sessions.signInWithApiKey',
    call: (c) => c.sessions.signInWithApiKey('stk_admin'),
    reply: json({ csrf_token: 'csrf' }, 201),
    method: 'POST',
    path: '/v1/sessions/api-key',
    body: { api_key: 'stk_admin' },
    result: { csrf_token: 'csrf' },
  },
  {
    name: 'deviceLinks.create',
    call: (c) => c.deviceLinks.create(),
    reply: json({ url: 'http://dash.test/connect#tok', expires_at: '2026-01-01T00:05:00Z', reachable: true }, 201),
    method: 'POST',
    path: '/v1/device-links',
  },
  {
    name: 'deviceLinks.redeem',
    call: (c) => c.deviceLinks.redeem('tok'),
    reply: json({ csrf_token: 'csrf' }, 201),
    method: 'POST',
    path: '/v1/device-links/redeem',
    body: { token: 'tok' },
  },
  {
    name: 'decisionLinks.decide',
    call: (c) => c.decisionLinks.decide('dl_tok', { feedback: 'no' }),
    reply: json({ status: 'rejected', item_id: 'itm_1', action: 'reject' }),
    method: 'POST',
    path: '/v1/decide/dl_tok',
    body: { feedback: 'no' },
    result: { status: 'rejected', item_id: 'itm_1', action: 'reject' },
  },
  // collection items
  {
    name: 'collections.listItems',
    call: (c) => c.collections.listItems('col_1', { status: 'pending', limit: 10 }),
    reply: json({ data: [], next_cursor: '' }),
    method: 'GET',
    path: '/v1/collections/col_1/items',
    query: { status: 'pending', limit: '10' },
  },
  // misc
  { name: 'whoami', call: (c) => c.whoami(), method: 'GET', path: '/v1/session' },
  { name: 'live', call: (c) => c.live(), reply: json({ status: 'ok' }), method: 'GET', path: '/livez' },
  {
    name: 'setup',
    call: (c) => c.setup(),
    reply: json({ initialized: true, mode: 'local', signup_open: false, setup_token_required: false }),
    method: 'GET',
    path: '/v1/setup',
    result: { initialized: true, mode: 'local', signup_open: false, setup_token_required: false },
  },
  { name: 'health', call: (c) => c.health(), method: 'GET', path: '/health' },
]

describe('request shapes', () => {
  for (const tc of cases) {
    it(tc.name, async () => {
      const reply = tc.reply ?? json({ id: 'x' })
      const { c, calls } = client(reply)
      const out = await tc.call(c)

      expect(calls).toHaveLength(1)
      const call = calls[0]!

      expect(call.method).toBe(tc.method)
      expect(call.path).toBe(tc.path)
      expect(Object.fromEntries(call.url.searchParams)).toEqual(tc.query ?? {})

      if (tc.body !== undefined) {
        expect(call.body).toEqual(tc.body)
      } else if (tc.method === 'GET') {
        expect(call.body).toBeUndefined()
      }

      if ('result' in tc) {
        expect(out).toEqual(tc.result)
      }
    })
  }
})

describe('items helpers', () => {
  it('resolves a collection name before listing', async () => {
    const { c, calls } = client(json({ id: 'col_7', name: 'Posts' }), json({ data: [] }))

    await c.items.list({ collection: 'Posts', status: 'approved' })
    expect(calls.map((x) => x.url.pathname + x.url.search)).toEqual([
      '/v1/collections/Posts',
      '/v1/items?status=approved&collection_id=col_7',
    ])
  })

  it('upload sends multipart with item JSON and the file', async () => {
    const { c, calls } = client(json(item({ type: 'image' }), 201))
    const png = new Uint8Array([0x89, 0x50, 0x4e, 0x47])

    await c.items.upload('Posts', png, {
      filename: 'a.png',
      contentType: 'image/png',
      title: 'Avatar',
      metadata: { k: 1 },
    })
    const form = calls[0]!.form!

    expect(calls[0]!.path).toBe('/v1/collections/Posts/items')
    expect(calls[0]!.headers.get('content-type')).toBeNull() // fetch sets the boundary
    expect(JSON.parse(form.get('item') as string)).toEqual({ title: 'Avatar', metadata: { k: 1 } })
    const file = form.get('file') as File

    expect(file.name).toBe('a.png')
    expect(file.type).toBe('image/png')
    expect(new Uint8Array(await file.arrayBuffer())).toEqual(png)
  })

  it("upload keeps a File's name", async () => {
    const { c, calls } = client(json(item(), 201))

    await c.media.upload(new File(['hi'], 'note.txt', { type: 'text/plain' }))
    expect((calls[0]!.form!.get('file') as File).name).toBe('note.txt')
  })

  it('media.download returns bytes and url() is absolute', async () => {
    const { c, calls } = client(new Response(new Uint8Array([1, 2, 3]), { headers: { 'Content-Type': 'image/png' } }))

    expect(await c.media.download('med_1')).toEqual(new Uint8Array([1, 2, 3]))
    expect(calls[0]!.path).toBe('/v1/media/med_1')
    expect(c.media.url('med_1')).toBe('http://api.test/v1/media/med_1')
  })

  it('decisionLinks.preview returns the HTML page', async () => {
    const { c, calls } = client(new Response('<h1>Approve?</h1>', { headers: { 'Content-Type': 'text/html' } }))

    expect(await c.decisionLinks.preview('dl_tok')).toBe('<h1>Approve?</h1>')
    expect(calls[0]!.path).toBe('/v1/decide/dl_tok')
  })
})

describe('collections helpers', () => {
  it('listItems resolves a collection name first', async () => {
    const { c, calls } = client(json({ id: 'col_9', name: 'Instagram posts' }), json({ data: [item()] }))
    const page = await c.collections.listItems('Instagram posts', { q: 'launch' })

    expect(calls.map((x) => x.path)).toEqual(['/v1/collections/Instagram%20posts', '/v1/collections/col_9/items'])
    expect(calls[1]!.url.searchParams.get('q')).toBe('launch')
    expect(page.data).toHaveLength(1)
  })
})

describe('pagination', () => {
  it('iterates items across pages', async () => {
    const { c, calls } = client(
      json({ data: [item({ id: 'a' }), item({ id: 'b' })], next_cursor: 'c1' }),
      json({ data: [item({ id: 'c' })], next_cursor: 'c2' }),
      json({ data: [] }),
    )
    const ids: string[] = []

    for await (const it of c.items.iterate({ status: 'pending', limit: 2 })) {
      ids.push(it.id)
    }

    expect(ids).toEqual(['a', 'b', 'c'])
    expect(calls.map((x) => x.url.searchParams.get('cursor'))).toEqual([null, 'c1', 'c2'])
    expect(calls.every((x) => x.url.searchParams.get('limit') === '2')).toBe(true)
  })

  it('stops early when the consumer breaks', async () => {
    const { c, calls } = client(json({ data: [item({ id: 'a' }), item({ id: 'b' })], next_cursor: 'c1' }))

    for await (const _ of c.items.iterate()) {
      break
    }

    expect(calls).toHaveLength(1)
  })

  it('iterates events and resolves collection names once', async () => {
    const { c, calls } = client(json({ data: [{ id: 'e1' }], next_cursor: 'x' }), json({ data: [{ id: 'e2' }] }))
    const ids: string[] = []

    for await (const e of c.events.iterate({ type: 'item.created' })) {
      ids.push(e.id)
    }

    expect(ids).toEqual(['e1', 'e2'])
    expect(calls[1]!.url.searchParams.get('cursor')).toBe('x')
  })

  it('iterate with a collection name', async () => {
    const { c, calls } = client(json({ id: 'col_3' }), json({ data: [item()] }))
    const out = []

    for await (const it of c.items.iterate({ collection: 'News' })) {
      out.push(it)
    }

    expect(out).toHaveLength(1)
    expect(calls[1]!.url.searchParams.get('collection_id')).toBe('col_3')
  })
})
