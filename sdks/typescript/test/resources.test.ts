import { describe, expect, it } from 'vitest'

import type { Staqly } from '../src/index.js'
import { client, json, post } from './helpers.js'

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
    call: (c) => c.collections.get('Blog posts'),
    method: 'GET',
    path: '/v1/collections/Blog%20posts',
  },
  {
    name: 'collections.create',
    call: (c) => c.collections.create({ name: 'A', color: '#fff', processing_timeout_seconds: 3600 }),
    method: 'POST',
    path: '/v1/collections',
    body: { name: 'A', color: '#fff', processing_timeout_seconds: 3600 },
  },
  {
    name: 'collections.update',
    call: (c) => c.collections.update('col_1', { name: 'B', processing_timeout_seconds: null }),
    method: 'PATCH',
    path: '/v1/collections/col_1',
    body: { name: 'B', processing_timeout_seconds: null },
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
  },
  {
    name: 'collections.listPosts',
    call: (c) => c.collections.listPosts('Blog posts', { status: 'draft', limit: 10 }),
    reply: json({ data: [post()] }),
    method: 'GET',
    path: '/v1/collections/Blog%20posts/posts',
    query: { status: 'draft', limit: '10' },
  },
  // posts
  {
    name: 'posts.list',
    call: (c) =>
      c.posts.list({ collection_id: 'Posts', status: 'scheduled', channel_id: 'ch_1', tag: 'launch', limit: 10 }),
    reply: json({ data: [post()], next_cursor: 'n' }),
    method: 'GET',
    path: '/v1/posts',
    query: { collection_id: 'Posts', status: 'scheduled', channel_id: 'ch_1', tag: 'launch', limit: '10' },
  },
  {
    name: 'posts.list calendar',
    call: (c) => c.posts.list({ publish_after: '2026-11-01', publish_before: '2026-11-08', sort: 'publish_asc' }),
    reply: json({ data: [] }),
    method: 'GET',
    path: '/v1/posts',
    query: { publish_after: '2026-11-01', publish_before: '2026-11-08', sort: 'publish_asc' },
  },
  {
    name: 'posts.search',
    call: (c) => c.posts.search('launch', { status: 'draft' }),
    reply: json({ data: [] }),
    method: 'GET',
    path: '/v1/posts',
    query: { q: 'launch', status: 'draft' },
  },
  { name: 'posts.get', call: (c) => c.posts.get('pst_1'), method: 'GET', path: '/v1/posts/pst_1' },
  {
    name: 'posts.create draft',
    call: (c) =>
      c.posts.create({
        collection: 'Posts',
        content: 'Hello',
        media: ['med_1'],
        targets: [{ channel_id: 'ch_1' }, { channel_id: 'ch_2', content: 'Hi', settings: { visibility: 'unlisted' } }],
        tags: ['launch'],
        metadata: { campaign: 'q4' },
        source: 'sdk-ts',
      }),
    reply: json(post(), 201),
    method: 'POST',
    path: '/v1/posts',
    body: {
      collection: 'Posts',
      content: 'Hello',
      media: ['med_1'],
      targets: [{ channel_id: 'ch_1' }, { channel_id: 'ch_2', content: 'Hi', settings: { visibility: 'unlisted' } }],
      tags: ['launch'],
      metadata: { campaign: 'q4' },
      source: 'sdk-ts',
    },
  },
  {
    name: 'posts.create scheduled',
    call: (c) =>
      c.posts.create({
        collection: 'col_1',
        content: 'x',
        targets: [{ channel_id: 'ch_1' }],
        status: 'scheduled',
        publish_at: '2026-11-03T09:00:00Z',
      }),
    reply: json(post({ status: 'scheduled' }), 201),
    method: 'POST',
    path: '/v1/posts',
    body: {
      collection: 'col_1',
      content: 'x',
      targets: [{ channel_id: 'ch_1' }],
      status: 'scheduled',
      publish_at: '2026-11-03T09:00:00Z',
    },
  },
  {
    name: 'posts.create publish now',
    call: (c) =>
      c.posts.create({ collection: 'col_1', content: 'x', targets: [{ channel_id: 'ch_1' }], publish_now: true }),
    reply: json(post({ status: 'publishing' }), 201),
    method: 'POST',
    path: '/v1/posts',
    body: { collection: 'col_1', content: 'x', targets: [{ channel_id: 'ch_1' }], publish_now: true },
  },
  {
    name: 'posts.update',
    call: (c) => c.posts.update('pst_1', { content: 'Edited', tags: ['a'], expected_version: 3 }),
    method: 'PATCH',
    path: '/v1/posts/pst_1',
    body: { content: 'Edited', tags: ['a'], expected_version: 3 },
  },
  {
    name: 'posts.create repeating',
    call: (c) => c.posts.create({ collection: 'col_1', content: 'x', repeat_every: '1w' }),
    reply: json(post({ repeat_every: '1w' }), 201),
    method: 'POST',
    path: '/v1/posts',
    body: { collection: 'col_1', content: 'x', repeat_every: '1w' },
  },
  {
    name: 'posts.create with a thread',
    call: (c) =>
      c.posts.create({
        collection: 'col_1',
        content: 'x',
        thread: [{ content: 'first reply' }, { content: 'second', delay_seconds: 300 }],
      }),
    reply: json(post({ thread: [{ content: 'first reply', media: [], delay_seconds: 0, media_items: [] }] }), 201),
    method: 'POST',
    path: '/v1/posts',
    body: {
      collection: 'col_1',
      content: 'x',
      thread: [{ content: 'first reply' }, { content: 'second', delay_seconds: 300 }],
    },
  },
  {
    name: 'posts.update clear the thread',
    call: (c) => c.posts.update('pst_1', { thread: [] }),
    method: 'PATCH',
    path: '/v1/posts/pst_1',
    body: { thread: [] },
  },
  {
    name: 'posts.update stop repeating',
    call: (c) => c.posts.update('pst_1', { repeat_every: null }),
    method: 'PATCH',
    path: '/v1/posts/pst_1',
    body: { repeat_every: null },
  },
  {
    name: 'posts.update unschedule and move',
    call: (c) => c.posts.update('pst_1', { publish_at: null, collection: 'Archive', targets: [] }),
    method: 'PATCH',
    path: '/v1/posts/pst_1',
    body: { publish_at: null, collection: 'Archive', targets: [] },
  },
  {
    name: 'posts.delete',
    call: (c) => c.posts.delete('pst_1'),
    reply: empty(),
    method: 'DELETE',
    path: '/v1/posts/pst_1',
  },
  {
    name: 'posts.purge',
    call: (c) => c.posts.purge('pst_1'),
    reply: json({ post_id: 'pst_1', events_deleted: 3, media_deleted: 1 }),
    method: 'POST',
    path: '/v1/posts/pst_1/purge',
    result: { post_id: 'pst_1', events_deleted: 3, media_deleted: 1 },
  },
  {
    name: 'posts.schedule',
    call: (c) => c.posts.schedule('pst_1', '2026-11-03T09:00:00Z'),
    method: 'POST',
    path: '/v1/posts/pst_1/schedule',
    body: { publish_at: '2026-11-03T09:00:00Z' },
  },
  { name: 'posts.publish', call: (c) => c.posts.publish('pst_1'), method: 'POST', path: '/v1/posts/pst_1/publish' },
  { name: 'posts.cancel', call: (c) => c.posts.cancel('pst_1'), method: 'POST', path: '/v1/posts/pst_1/cancel' },
  {
    name: 'posts.duplicate',
    call: (c) => c.posts.duplicate('pst_1'),
    reply: json(post({ id: 'pst_2' }), 201),
    method: 'POST',
    path: '/v1/posts/pst_1/duplicate',
  },
  {
    name: 'posts.retryTarget',
    call: (c) => c.posts.retryTarget('pst_1', 'ch_1'),
    method: 'POST',
    path: '/v1/posts/pst_1/targets/ch_1/retry',
  },
  {
    name: 'posts.reportResult published',
    call: (c) => c.posts.reportResult('pst_1', 'ch_1', { status: 'published', url: 'https://x/1', external_id: '42' }),
    method: 'POST',
    path: '/v1/posts/pst_1/targets/ch_1/result',
    body: { status: 'published', url: 'https://x/1', external_id: '42' },
  },
  {
    name: 'posts.reportResult failed',
    call: (c) => c.posts.reportResult('pst_1', 'ch_1', { status: 'failed', error: 'API 500' }),
    method: 'POST',
    path: '/v1/posts/pst_1/targets/ch_1/result',
    body: { status: 'failed', error: 'API 500' },
  },
  {
    name: 'posts.preview',
    call: (c) => c.posts.preview('pst_1'),
    reply: json({ targets: [{ channel_id: 'ch_1', text: 'Hello', warnings: [], ok: true }] }),
    method: 'GET',
    path: '/v1/posts/pst_1/preview',
    result: { targets: [{ channel_id: 'ch_1', text: 'Hello', warnings: [], ok: true }] },
  },
  {
    name: 'posts.history',
    call: (c) => c.posts.history('pst_1', { limit: 5 }),
    reply: json({ data: [] }),
    method: 'GET',
    path: '/v1/posts/pst_1/events',
    query: { limit: '5' },
  },
  {
    name: 'posts.tags',
    call: (c) => c.posts.tags(),
    reply: list([{ name: 'launch', count: 3 }]),
    method: 'GET',
    path: '/v1/tags',
    result: [{ name: 'launch', count: 3 }],
  },
  // work
  {
    name: 'work.list',
    call: (c) => c.work.list(),
    reply: list([{ kind: 'target', requested_at: 't' }]),
    method: 'GET',
    path: '/v1/work',
    result: [{ kind: 'target', requested_at: 't' }],
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
    call: (c) => c.events.list({ type: 'post.published', post_id: 'pst_1', channel_id: 'ch_1', limit: 10 }),
    reply: json({ data: [] }),
    method: 'GET',
    path: '/v1/events',
    query: { type: 'post.published', post_id: 'pst_1', channel_id: 'ch_1', limit: '10' },
  },
  // channels and providers
  {
    name: 'providers.list',
    call: (c) => c.providers.list(),
    reply: list([{ id: 'telegram' }]),
    method: 'GET',
    path: '/v1/providers',
    result: [{ id: 'telegram' }],
  },
  {
    name: 'providers.setApp',
    call: (c) => c.providers.setApp('mastodon', { client_id: 'a', client_secret: 'b' }),
    reply: empty(),
    method: 'PUT',
    path: '/v1/providers/mastodon/app',
    body: { client_id: 'a', client_secret: 'b' },
  },
  {
    name: 'providers.deleteApp',
    call: (c) => c.providers.deleteApp('mastodon'),
    reply: empty(),
    method: 'DELETE',
    path: '/v1/providers/mastodon/app',
  },
  {
    name: 'channels.list',
    call: (c) => c.channels.list(),
    reply: list([{ id: 'ch_1' }]),
    method: 'GET',
    path: '/v1/channels',
    result: [{ id: 'ch_1' }],
  },
  { name: 'channels.get', call: (c) => c.channels.get('ch_1'), method: 'GET', path: '/v1/channels/ch_1' },
  {
    name: 'channels.connect token platform',
    call: (c) =>
      c.channels.connect({
        provider: 'telegram',
        name: 'News',
        fields: { bot_token: '123:abc', chat_id: '@news' },
        settings: { silent: true },
      }),
    reply: json({ id: 'ch_2' }, 201),
    method: 'POST',
    path: '/v1/channels',
    body: {
      provider: 'telegram',
      name: 'News',
      fields: { bot_token: '123:abc', chat_id: '@news' },
      settings: { silent: true },
    },
  },
  {
    name: 'channels.connect custom',
    call: (c) => c.channels.connect({ provider: 'custom', name: 'Pipeline' }),
    reply: json({ id: 'ch_3' }, 201),
    method: 'POST',
    path: '/v1/channels',
    body: { provider: 'custom', name: 'Pipeline' },
  },
  {
    name: 'channels.update',
    call: (c) =>
      c.channels.update('ch_1', { name: 'Brand', settings: { visibility: 'unlisted' }, fields: { bot_token: 'new' } }),
    method: 'PATCH',
    path: '/v1/channels/ch_1',
    body: { name: 'Brand', settings: { visibility: 'unlisted' }, fields: { bot_token: 'new' } },
  },
  {
    name: 'channels.disable',
    call: (c) => c.channels.disable('ch_1'),
    method: 'PATCH',
    path: '/v1/channels/ch_1',
    body: { status: 'disabled' },
  },
  {
    name: 'channels.enable',
    call: (c) => c.channels.enable('ch_1'),
    method: 'PATCH',
    path: '/v1/channels/ch_1',
    body: { status: 'connected' },
  },
  {
    name: 'channels.remove',
    call: (c) => c.channels.remove('ch_1'),
    reply: empty(),
    method: 'DELETE',
    path: '/v1/channels/ch_1',
  },
  {
    name: 'channels.test',
    call: (c) => c.channels.test('ch_1'),
    reply: json({ ok: true, profile: { handle: '@brand' } }),
    method: 'POST',
    path: '/v1/channels/ch_1/test',
    result: { ok: true, profile: { handle: '@brand' } },
  },
  {
    name: 'channels.oauthStart',
    call: (c) => c.channels.oauthStart('mastodon', { instance: 'mastodon.social' }),
    reply: json({ url: 'https://mastodon.social/oauth/authorize?x=1' }),
    method: 'GET',
    path: '/v1/channels/oauth/mastodon/start',
    query: { instance: 'mastodon.social' },
    result: 'https://mastodon.social/oauth/authorize?x=1',
  },
  // schedules
  {
    name: 'schedules.list',
    call: (c) => c.schedules.list({ collection_id: 'Posts' }),
    reply: list([{ id: 'sch_1' }]),
    method: 'GET',
    path: '/v1/schedules',
    query: { collection_id: 'Posts' },
    result: [{ id: 'sch_1' }],
  },
  { name: 'schedules.get', call: (c) => c.schedules.get('sch_1'), method: 'GET', path: '/v1/schedules/sch_1' },
  {
    name: 'schedules.create recurring',
    call: (c) =>
      c.schedules.create({
        collection: 'Posts',
        name: 'Mornings',
        kind: 'recurring',
        rule: { times: ['09:00'], weekdays: [1, 2, 3, 4, 5], tz: 'Europe/Warsaw' },
        channels: ['ch_1'],
        pick: 'next',
        source: 'agent',
      }),
    reply: json({ id: 'sch_1' }, 201),
    method: 'POST',
    path: '/v1/schedules',
    body: {
      collection: 'Posts',
      name: 'Mornings',
      kind: 'recurring',
      rule: { times: ['09:00'], weekdays: [1, 2, 3, 4, 5], tz: 'Europe/Warsaw' },
      channels: ['ch_1'],
      pick: 'next',
      source: 'agent',
    },
  },
  {
    name: 'schedules.create once from an endpoint',
    call: (c) =>
      c.schedules.create({
        collection: 'col_1',
        name: 'Launch',
        kind: 'once',
        run_at: '2026-11-03T09:00:00Z',
        pick: 'all',
        source: 'endpoint',
        endpoint_url: 'https://my.app/posts',
        endpoint_secret: 's',
      }),
    reply: json({ id: 'sch_2' }, 201),
    method: 'POST',
    path: '/v1/schedules',
    body: {
      collection: 'col_1',
      name: 'Launch',
      kind: 'once',
      run_at: '2026-11-03T09:00:00Z',
      pick: 'all',
      source: 'endpoint',
      endpoint_url: 'https://my.app/posts',
      endpoint_secret: 's',
    },
  },
  {
    name: 'schedules.update',
    call: (c) => c.schedules.update('sch_1', { rule: { every_minutes: 360 }, pick: 'selected', post_ids: ['pst_1'] }),
    method: 'PATCH',
    path: '/v1/schedules/sch_1',
    body: { rule: { every_minutes: 360 }, pick: 'selected', post_ids: ['pst_1'] },
  },
  {
    name: 'schedules.disable',
    call: (c) => c.schedules.disable('sch_1'),
    method: 'PATCH',
    path: '/v1/schedules/sch_1',
    body: { enabled: false },
  },
  {
    name: 'schedules.enable',
    call: (c) => c.schedules.enable('sch_1'),
    method: 'PATCH',
    path: '/v1/schedules/sch_1',
    body: { enabled: true },
  },
  {
    name: 'schedules.delete',
    call: (c) => c.schedules.delete('sch_1'),
    reply: empty(),
    method: 'DELETE',
    path: '/v1/schedules/sch_1',
  },
  {
    name: 'schedules.run',
    call: (c) => c.schedules.run('sch_1'),
    reply: json({ id: 'sch_1' }, 202),
    method: 'POST',
    path: '/v1/schedules/sch_1/run',
  },
  // jobs
  {
    name: 'jobs.list',
    call: (c) => c.jobs.list({ status: 'failed', kind: 'publish_post', limit: 5 }),
    reply: json({ data: [] }),
    method: 'GET',
    path: '/v1/jobs',
    query: { status: 'failed', kind: 'publish_post', limit: '5' },
  },
  {
    name: 'jobs.stats',
    call: (c) => c.jobs.stats(),
    reply: json({ queued: 1, running: 0, failed: 0, done_24h: 9, by_kind: {} }),
    method: 'GET',
    path: '/v1/jobs/stats',
    result: { queued: 1, running: 0, failed: 0, done_24h: 9, by_kind: {} },
  },
  { name: 'jobs.get', call: (c) => c.jobs.get('job_1'), method: 'GET', path: '/v1/jobs/job_1' },
  {
    name: 'jobs.retry',
    call: (c) => c.jobs.retry('job_1'),
    reply: json({ id: 'job_1' }, 202),
    method: 'POST',
    path: '/v1/jobs/job_1/retry',
  },
  { name: 'jobs.cancel', call: (c) => c.jobs.cancel('job_1'), method: 'POST', path: '/v1/jobs/job_1/cancel' },
  // webhooks
  {
    name: 'webhooks.list',
    call: (c) => c.webhooks.list(),
    reply: list([{ id: 'wh_1' }]),
    method: 'GET',
    path: '/v1/webhooks',
    result: [{ id: 'wh_1' }],
  },
  { name: 'webhooks.get', call: (c) => c.webhooks.get('wh_1'), method: 'GET', path: '/v1/webhooks/wh_1' },
  {
    name: 'webhooks.create',
    call: (c) => c.webhooks.create({ url: 'https://h', events: ['post.*'] }),
    reply: json({ id: 'wh_1' }, 201),
    method: 'POST',
    path: '/v1/webhooks',
    body: { url: 'https://h', events: ['post.*'] },
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
  },
  {
    name: 'webhooks.deliveries.list',
    call: (c) => c.webhooks.deliveries.list({ webhook_id: 'wh_1', post_id: 'pst_1' }),
    reply: list([]),
    method: 'GET',
    path: '/v1/webhook-deliveries',
    query: { webhook_id: 'wh_1', post_id: 'pst_1' },
  },
  {
    name: 'webhooks.deliveries.retry',
    call: (c) => c.webhooks.deliveries.retry('dlv_1'),
    reply: json({ id: 'dlv_1' }, 202),
    method: 'POST',
    path: '/v1/webhook-deliveries/dlv_1/retry',
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
    reply: json({}, 201),
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
    call: (c) => c.apiKeys.create({ name: 'bot', scopes: ['posts:read', 'posts:write'], expires_at: '2027-01-01' }),
    reply: json({ id: 'key_1' }, 201),
    method: 'POST',
    path: '/v1/api-keys',
    body: { name: 'bot', scopes: ['posts:read', 'posts:write'], expires_at: '2027-01-01' },
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
  // alerts
  {
    name: 'alerts.list',
    call: (c) => c.alerts.list(),
    reply: list([{ id: 'nch_1' }]),
    method: 'GET',
    path: '/v1/alerts',
    result: [{ id: 'nch_1' }],
  },
  { name: 'alerts.get', call: (c) => c.alerts.get('nch_1'), method: 'GET', path: '/v1/alerts/nch_1' },
  {
    name: 'alerts.create',
    call: (c) => c.alerts.create({ name: 'Phone', type: 'ntfy', config: { topic: 't' }, events: ['post.failed'] }),
    reply: json({ id: 'nch_1' }, 201),
    method: 'POST',
    path: '/v1/alerts',
    body: { name: 'Phone', type: 'ntfy', config: { topic: 't' }, events: ['post.failed'] },
  },
  {
    name: 'alerts.update',
    call: (c) => c.alerts.update('nch_1', { config: { priority: 4, access_token: null } }),
    method: 'PATCH',
    path: '/v1/alerts/nch_1',
    body: { config: { priority: 4, access_token: null } },
  },
  {
    name: 'alerts.enable',
    call: (c) => c.alerts.enable('nch_1'),
    method: 'PATCH',
    path: '/v1/alerts/nch_1',
    body: { enabled: true },
  },
  {
    name: 'alerts.disable',
    call: (c) => c.alerts.disable('nch_1'),
    method: 'PATCH',
    path: '/v1/alerts/nch_1',
    body: { enabled: false },
  },
  {
    name: 'alerts.delete',
    call: (c) => c.alerts.delete('nch_1'),
    reply: empty(),
    method: 'DELETE',
    path: '/v1/alerts/nch_1',
  },
  {
    name: 'alerts.test',
    call: (c) => c.alerts.test('nch_1'),
    reply: json({ ok: false, error: 'HTTP 401' }),
    method: 'POST',
    path: '/v1/alerts/nch_1/test',
    result: { ok: false, error: 'HTTP 401' },
  },
  {
    name: 'alerts.deliveries',
    call: (c) => c.alerts.deliveries('nch_1', { limit: 5 }),
    reply: list([{ id: 'ntd_1' }]),
    method: 'GET',
    path: '/v1/alerts/nch_1/deliveries',
    query: { limit: '5' },
    result: [{ id: 'ntd_1' }],
  },
  // exports, readiness
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
  { name: 'ready', call: (c) => c.ready(), reply: json({ status: 'ok', checks: {} }), method: 'GET', path: '/readyz' },
  {
    name: 'workspace.setPlan',
    call: (c) => c.workspace.setPlan('free'),
    reply: json({ plan: { id: 'free' } }),
    method: 'POST',
    path: '/v1/workspace/plan',
    body: { plan: 'free' },
  },
  {
    name: 'workspace.rotateRecoveryKey',
    call: (c) => c.workspace.rotateRecoveryKey(),
    reply: json({ recovery_key: 'rk' }),
    method: 'POST',
    path: '/v1/recovery-key/rotate',
    result: { recovery_key: 'rk' },
  },
  {
    name: 'workspace.delete',
    call: (c) => c.workspace.delete('Acme'),
    reply: empty(),
    method: 'DELETE',
    path: '/v1/workspace',
    body: { confirm: 'Acme' },
  },
  {
    name: 'billing.get',
    call: (c) => c.billing.get(),
    reply: json({ enabled: true, plan: 'pro', suspended: false, has_customer: true }),
    method: 'GET',
    path: '/v1/billing',
  },
  {
    name: 'billing.checkout',
    call: (c) => c.billing.checkout(),
    reply: json({ url: 'https://checkout.stripe.com/x' }),
    method: 'POST',
    path: '/v1/billing/checkout',
    result: { url: 'https://checkout.stripe.com/x' },
  },
  {
    name: 'billing.portal',
    call: (c) => c.billing.portal(),
    reply: json({ url: 'https://billing.stripe.com/x' }),
    method: 'POST',
    path: '/v1/billing/portal',
  },
  { name: 'sessions.current', call: (c) => c.sessions.current(), method: 'GET', path: '/v1/session' },
  {
    name: 'sessions.list',
    call: (c) => c.sessions.list(),
    reply: list([{ id: 'ses_1' }]),
    method: 'GET',
    path: '/v1/sessions',
    result: [{ id: 'ses_1' }],
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
    reply: json({ csrf_token: 'c', api_key: { key: 'stk_new' }, recovery_key: 'rk_2' }, 201),
    method: 'POST',
    path: '/v1/sessions/recover',
    body: { recovery_key: 'rk_1' },
    result: { csrf_token: 'c', api_key: { key: 'stk_new' }, recovery_key: 'rk_2' },
  },
  {
    name: 'sessions.signInWithApiKey',
    call: (c) => c.sessions.signInWithApiKey('stk_admin'),
    reply: json({ csrf_token: 'c' }, 201),
    method: 'POST',
    path: '/v1/sessions/api-key',
    body: { api_key: 'stk_admin' },
  },
  {
    name: 'deviceLinks.create',
    call: (c) => c.deviceLinks.create(),
    reply: json({ url: 'https://h/connect#t' }, 201),
    method: 'POST',
    path: '/v1/device-links',
  },
  {
    name: 'deviceLinks.redeem',
    call: (c) => c.deviceLinks.redeem('tok'),
    reply: json({ csrf_token: 'c' }, 201),
    method: 'POST',
    path: '/v1/device-links/redeem',
    body: { token: 'tok' },
  },
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

describe('media helpers', () => {
  it('upload sends multipart with the file', async () => {
    const { c, calls } = client(json({ id: 'med_1' }, 201))

    await c.media.upload(new Uint8Array([1, 2, 3]), { filename: 'a.png', contentType: 'image/png' })
    const form = calls[0]!.form!
    const file = form.get('file') as File

    expect(calls[0]!.path).toBe('/v1/media')
    expect(file.name).toBe('a.png')
    expect(file.type).toBe('image/png')
    expect(file.size).toBe(3)
  })

  it("upload keeps a File's name", async () => {
    const { c, calls } = client(json({ id: 'med_1' }, 201))

    await c.media.upload(new File([new Uint8Array([1])], 'photo.jpg', { type: 'image/jpeg' }))
    expect((calls[0]!.form!.get('file') as File).name).toBe('photo.jpg')
  })

  it('media.download returns bytes and url() is absolute', async () => {
    const { c } = client(new Response(new Uint8Array([9, 8])))

    expect(await c.media.download('med_1')).toEqual(new Uint8Array([9, 8]))
    expect(c.media.url('med 1')).toBe('http://api.test/v1/media/med%201')
  })
})

describe('pagination', () => {
  it('iterates posts across pages', async () => {
    const { c, calls } = client(
      json({ data: [post({ id: 'a' }), post({ id: 'b' })], next_cursor: 'c1' }),
      json({ data: [post({ id: 'c' })], next_cursor: 'c2' }),
      json({ data: [] }),
    )
    const ids: string[] = []

    for await (const p of c.posts.iterate({ status: 'draft', limit: 2 })) {
      ids.push(p.id)
    }

    expect(ids).toEqual(['a', 'b', 'c'])
    expect(calls.map((x) => x.url.searchParams.get('cursor'))).toEqual([null, 'c1', 'c2'])
  })

  it('stops early when the consumer breaks', async () => {
    const { c, calls } = client(json({ data: [post({ id: 'a' }), post({ id: 'b' })], next_cursor: 'c1' }))

    for await (const p of c.posts.iterate()) {
      if (p.id === 'a') {
        break
      }
    }

    expect(calls).toHaveLength(1)
  })

  it('iterates jobs and events', async () => {
    const { c } = client(json({ data: [{ id: 'job_1' }], next_cursor: 'n' }), json({ data: [{ id: 'job_2' }] }))
    const ids: string[] = []

    for await (const j of c.jobs.iterate({ status: 'failed' })) {
      ids.push(j.id)
    }

    expect(ids).toEqual(['job_1', 'job_2'])
    const { c: c2 } = client(json({ data: [{ id: 'e1', seq: 1 }] }))
    const seqs: number[] = []

    for await (const e of c2.events.iterate({ post_id: 'pst_1' })) {
      seqs.push(e.seq)
    }

    expect(seqs).toEqual([1])
  })
})
