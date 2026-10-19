import type { Channel, Collection, Post, PostTarget, Provider, Schedule } from '@/lib/api'

export function collection(over: Partial<Collection> = {}): Collection {
  return {
    id: 'col_1',
    name: 'Instagram Avatar',
    description: 'Portraits for the brand account',
    icon: '',
    color: '#22c55e',
    processing_timeout_seconds: null,
    is_default: false,
    archived: false,
    archived_at: null,
    created_at: '2026-01-01T10:00:00Z',
    updated_at: '2026-01-01T10:00:00Z',
    post_count: 3,
    status_counts: { draft: 1, scheduled: 1, published: 1 },
    ...over,
  } as Collection
}

export function channel(over: Partial<Channel> = {}): Channel {
  return {
    id: 'ch_1',
    provider: 'mastodon',
    name: 'Brand on Mastodon',
    handle: '@brand@mastodon.social',
    avatar_url: '',
    profile_url: 'https://mastodon.social/@brand',
    status: 'connected',
    settings: {},
    last_error: '',
    created_at: '2026-01-01T10:00:00Z',
    updated_at: '2026-01-01T10:00:00Z',
    ...over,
  } as Channel
}

export function target(over: Partial<PostTarget> = {}): PostTarget {
  return {
    channel_id: 'ch_1',
    channel_name: 'Brand on Mastodon',
    provider: 'mastodon',
    avatar_url: '',
    content: null,
    settings: {},
    status: 'pending',
    external_id: '',
    external_url: '',
    error: '',
    attempts: 0,
    published_at: null,
    ...over,
  } as PostTarget
}

export function post(over: Partial<Post> = {}): Post {
  return {
    id: 'pst_1',
    collection_id: 'col_1',
    collection_name: 'Instagram Avatar',
    content: 'Spring sale starts Monday. Three days, every avatar half price.',
    media: [],
    media_items: [],
    status: 'draft',
    publish_at: null,
    published_at: null,
    tags: [],
    metadata: {},
    source: 'claude-code',
    external_ref: '',
    version: 1,
    targets: [target()],
    created_at: '2026-01-01T10:00:00Z',
    updated_at: '2026-01-01T10:00:00Z',
    ...over,
  } as Post
}

export function provider(over: Partial<Provider> = {}): Provider {
  return {
    id: 'mastodon',
    name: 'Mastodon',
    auth: 'oauth2',
    status: 'available',
    limits: {
      max_chars: 500,
      max_media: 4,
      media_kinds: ['image', 'video'],
      needs_title: false,
      needs_link: false,
      needs_media: false,
    },
    credential_fields: [],
    settings_fields: [
      {
        key: 'visibility',
        label: 'Visibility',
        kind: 'select',
        required: false,
        default: 'public',
        options: [
          { value: 'public', label: 'Public' },
          { value: 'unlisted', label: 'Unlisted' },
        ],
      },
    ],
    app_configured: true,
    docs_url: 'https://docs.staqly.ai/channels/mastodon',
    ...over,
  } as Provider
}

export function schedule(over: Partial<Schedule> = {}): Schedule {
  return {
    id: 'sch_1',
    collection_id: 'col_1',
    collection_name: 'Instagram Avatar',
    name: 'Weekday mornings',
    channels: ['ch_1'],
    channel_names: ['Brand on Mastodon'],
    kind: 'recurring',
    run_at: null,
    rule: { times: ['09:00'], weekdays: [1, 2, 3, 4, 5], tz: 'Europe/Warsaw' },
    pick: 'next',
    post_ids: [],
    source: 'none',
    endpoint_url: '',
    enabled: true,
    next_run_at: '2026-01-02T08:00:00Z',
    last_run_at: null,
    last_result: '',
    created_at: '2026-01-01T10:00:00Z',
    updated_at: '2026-01-01T10:00:00Z',
    ...over,
  } as Schedule
}
