import type { AlertType, Alert, AlertInput } from './api'
import { formatInterval } from './format'

// Pure helpers for notification channels: form model, validation mirroring
// apps/api/internal/app/notify_channels.go, request bodies and secret masking.

export const CHANNEL_TYPES: AlertType[] = ['telegram', 'slack', 'discord', 'ntfy', 'email']

export const CHANNEL_TYPE_INFO: Record<AlertType, { label: string; tagline: string }> = {
  telegram: { label: 'Telegram', tagline: 'Bot message with a link to the post' },
  slack: { label: 'Slack', tagline: 'Incoming webhook or a bot in a channel' },
  discord: { label: 'Discord', tagline: 'Channel webhook or bot; buttons with an application' },
  ntfy: { label: 'ntfy', tagline: 'Push to your phone with a link to the post' },
  email: { label: 'Email', tagline: 'SMTP message with a link to the post' },
}

const DEFAULT_CHANNEL_EVENTS = ['post.published', 'post.failed', 'channel.needs_reconnect']

/** Grouped like the webhook events picker; post.* covers (and greys out) the post events. */
export const CHANNEL_EVENTS: { value: string; hint: string; group: string }[] = [
  { value: 'post.*', hint: 'Every post event', group: 'Wildcards' },
  { value: 'post.published', hint: 'A post is out on every channel', group: 'Publishing' },
  { value: 'post.failed', hint: 'A channel failed for good', group: 'Publishing' },
  { value: 'post.target_failed', hint: 'One channel failed (retries follow)', group: 'Publishing' },
  { value: 'post.publish_requested', hint: 'A post waits for your automation', group: 'Publishing' },
  { value: 'post.created', hint: 'A post was added', group: 'Posts' },
  { value: 'post.scheduled', hint: 'A time was set or moved', group: 'Posts' },
  { value: 'schedule.posts_requested', hint: 'A schedule asked for new posts', group: 'Schedules' },
  { value: 'schedule.failed', hint: 'A schedule run failed', group: 'Schedules' },
  { value: 'channel.needs_reconnect', hint: 'A channel lost its sign-in', group: 'Channels' },
]

export const DIGEST_PRESETS = [0, 60, 300, 900, 3600, 86400]

export function digestLabel(seconds: number) {
  return seconds > 0 ? `Digest every ${formatInterval(seconds)}` : 'Immediately'
}

/** Config keys the server stores encrypted and returns masked. */
const SECRET_FIELDS = ['bot_token', 'webhook_url', 'access_token', 'password'] as const

export type SecretField = (typeof SECRET_FIELDS)[number]

const MASK = '••••'

/** A masked secret as returned by the API (e.g. "••••abcd" or "https://hooks.slack.com/••••"). */
export function isMasked(v: unknown): v is string {
  return typeof v === 'string' && v.includes(MASK)
}

/** Short human hint of a stored secret: "ends in abcd", "hooks.slack.com", or "". */
export function maskedHint(v: unknown): string {
  if (!isMasked(v)) {
    return ''
  }

  const url = /^https?:\/\/([^/]+)\//.exec(v)

  if (url) {
    return url[1]
  }

  const tail = v.slice(v.indexOf(MASK) + MASK.length)

  return tail ? `ends in ${tail}` : ''
}

export type Delivery = 'webhook' | 'bot'

/** Flat form model covering every type; only the fields of the chosen type are sent. */
export interface ChannelFormValues {
  name: string
  enabled: boolean
  events: string[]
  collection_ids: string[]
  digest_window_seconds: number
  // telegram
  bot_token: string
  chat_id: string
  // slack / discord
  delivery: Delivery
  webhook_url: string
  channel: string
  // ntfy
  server_url: string
  topic: string
  access_token: string
  priority: string
  // email
  host: string
  port: string
  tls: 'starttls' | 'tls' | 'none'
  username: string
  password: string
  from: string
  to: string[]
}

/**
 * What to do with each secret on save: "keep" the stored value (not sent),
 * "set" a new one (sent), or "remove" it (sent as null).
 */
export type SecretAction = 'keep' | 'set' | 'remove'
export type SecretState = Partial<Record<SecretField, SecretAction>>

export function defaultChannelValues(type: AlertType): ChannelFormValues {
  return {
    name: CHANNEL_TYPE_INFO[type].label,
    enabled: true,
    events: [...DEFAULT_CHANNEL_EVENTS],
    collection_ids: [],
    digest_window_seconds: 0,
    bot_token: '',
    chat_id: '',
    delivery: 'webhook',
    webhook_url: '',
    channel: '',
    server_url: 'https://ntfy.sh',
    topic: '',
    access_token: '',
    priority: '',
    host: '',
    port: '',
    tls: 'starttls',
    username: '',
    password: '',
    from: '',
    to: [],
  }
}

function str(v: unknown): string {
  return typeof v === 'string' ? v : typeof v === 'number' ? String(v) : ''
}

function strList(v: unknown): string[] {
  return Array.isArray(v) ? v.map(str).filter(Boolean) : []
}

/** Secrets that are currently stored for a channel (masked in its config). */
export function storedSecrets(ch: Pick<Alert, 'config'> | null | undefined): SecretField[] {
  if (!ch) {
    return []
  }

  return SECRET_FIELDS.filter((f) => isMasked(ch.config[f]))
}

/** Existing channel → form values. Secrets are never prefilled. */
export function channelToValues(ch: Alert): ChannelFormValues {
  const c = ch.config
  const d = defaultChannelValues(ch.type)
  const tls = str(c.tls)

  return {
    ...d,
    name: ch.name,
    enabled: ch.enabled,
    events: ch.events.length ? [...ch.events] : [...DEFAULT_CHANNEL_EVENTS],
    collection_ids: [...ch.collection_ids],
    digest_window_seconds: ch.digest_window_seconds ?? 0,
    chat_id: str(c.chat_id),
    delivery: isMasked(c.webhook_url) || !(isMasked(c.bot_token) || str(c.channel)) ? 'webhook' : 'bot',
    channel: str(c.channel),
    server_url: str(c.server_url) || d.server_url,
    topic: str(c.topic),
    priority: c.priority ? str(c.priority) : '',
    host: str(c.host),
    port: c.port ? str(c.port) : '',
    tls: tls === 'tls' || tls === 'none' ? tls : 'starttls',
    username: str(c.username),
    from: str(c.from),
    to: strList(c.to),
  }
}

// ---- validation (mirrors validateChannelConfig) ----

const RE = {
  telegramToken: /^[0-9]{3,20}:[A-Za-z0-9_-]{20,100}$/,
  telegramChat: /^(-?[0-9]{1,20}|@[A-Za-z0-9_]{4,64})$/,
  ntfyTopic: /^[A-Za-z0-9_-]{1,64}$/,
  slackId: /^[A-Za-z0-9_-]{1,64}$/,
  hex64: /^[0-9a-fA-F]{64}$/,
  digits: /^[0-9]{1,25}$/,
  email: /^[^\s@<>"]+@[^\s@<>"]+\.[^\s@<>"]+$/,
  // "Name <addr@host>" or a bare address (net/mail.ParseAddress accepts both).
  from: /^(?:[^<>\r\n]*<[^\s@<>"]+@[^\s@<>"]+>|[^\s@<>"]+@[^\s@<>"]+)$/,
}

function httpUrlError(v: string): string | null {
  try {
    const u = new URL(v)

    if ((u.protocol !== 'http:' && u.protocol !== 'https:') || !u.hostname || v.length > 2000) {
      return 'Must be an absolute http(s) URL'
    }

    if (u.username || u.password) {
      return 'Must not contain credentials'
    }

    return null
  } catch {
    return 'Must be an absolute http(s) URL'
  }
}

/** Fields the user must type: a secret is needed when none is stored or it is being replaced. */
function needsSecret(field: SecretField, stored: SecretField[], secrets: SecretState) {
  const action = secrets[field] ?? (stored.includes(field) ? 'keep' : 'set')

  return action === 'set'
}

function secretPresent(field: SecretField, v: ChannelFormValues, stored: SecretField[], secrets: SecretState) {
  const action = secrets[field] ?? (stored.includes(field) ? 'keep' : 'set')

  if (action === 'remove') {
    return false
  }

  if (action === 'keep') {
    return stored.includes(field)
  }

  return v[field].trim() !== ''
}

export type ChannelErrors = Partial<Record<keyof ChannelFormValues, string>>

/** The fields the form shows (and can put an error under) for this type and delivery. */
export function channelFormFields(type: AlertType, delivery: Delivery): (keyof ChannelFormValues)[] {
  const common: (keyof ChannelFormValues)[] = ['name', 'events', 'collection_ids', 'digest_window_seconds']

  switch (type) {
    case 'telegram':
      return [...common, 'bot_token', 'chat_id']
    case 'slack':
    case 'discord': {
      const connection: (keyof ChannelFormValues)[] =
        delivery === 'webhook' ? ['webhook_url'] : ['bot_token', 'channel']

      return [...common, ...connection]
    }

    case 'ntfy':
      return [...common, 'server_url', 'topic', 'access_token', 'priority']
    case 'email':
      return [...common, 'host', 'port', 'tls', 'username', 'password', 'from', 'to']
  }

  return common
}

/**
 * A 422 field name (`name`, `events`, `config.chat_id`) → the form field it belongs to, or null when the form
 * doesn't show that field for this type (then the error is toasted).
 */
export function channelApiFieldMap(type: AlertType, delivery: Delivery) {
  const fields = channelFormFields(type, delivery)

  return (apiField: string): keyof ChannelFormValues | null => {
    const key = apiField.replace(/^config\./, '') as keyof ChannelFormValues

    return fields.includes(key) ? key : null
  }
}

export function validateChannel(
  type: AlertType,
  v: ChannelFormValues,
  stored: SecretField[] = [],
  secrets: SecretState = {},
): ChannelErrors {
  const e: ChannelErrors = {}
  const name = v.name.trim()

  if (!name) {
    e.name = 'Give the channel a name'
  } else if (name.length > 100) {
    e.name = 'At most 100 characters'
  }

  if (!v.events.length) {
    e.events = 'Subscribe to at least one event'
  } else if (v.events.length > 50) {
    e.events = 'At most 50 event patterns'
  } else if (v.events.some((ev) => !ev || ev.length > 100 || /[\s/]/.test(ev))) {
    e.events = 'Event patterns have no spaces or slashes'
  }

  if (!Number.isInteger(v.digest_window_seconds) || v.digest_window_seconds < 0 || v.digest_window_seconds > 86400) {
    e.digest_window_seconds = 'Between 0 and 24 hours'
  }

  const typed = (f: SecretField) => v[f].trim()
  const has = (f: SecretField) => secretPresent(f, v, stored, secrets)

  switch (type) {
    case 'telegram':
      if (needsSecret('bot_token', stored, secrets) && !RE.telegramToken.test(typed('bot_token'))) {
        e.bot_token = 'Paste the bot token from @BotFather (123456:ABC...)'
      }

      if (!RE.telegramChat.test(v.chat_id.trim())) {
        e.chat_id = 'A numeric chat ID (groups are negative) or @channelname'
      }

      break
    case 'slack':
    case 'discord': {
      if (v.delivery === 'webhook') {
        if (needsSecret('webhook_url', stored, secrets)) {
          const err = typed('webhook_url') ? httpUrlError(typed('webhook_url')) : 'Paste the webhook URL'

          if (err) {
            e.webhook_url = err
          }
        }
      } else {
        if (!has('bot_token')) {
          e.bot_token = 'Paste the bot token'
        }

        if (!v.channel.trim()) {
          e.channel = 'Enter the channel ID'
        }
      }

      if (type === 'slack') {
        if (v.delivery === 'bot' && v.channel.trim() && !RE.slackId.test(v.channel.trim())) {
          e.channel = 'A Slack channel ID (C...)'
        }
      } else {
        if (v.delivery === 'bot' && v.channel.trim() && !RE.digits.test(v.channel.trim())) {
          e.channel = 'A numeric Discord channel ID'
        }
      }

      break
    }

    case 'ntfy': {
      const server = v.server_url.trim() || 'https://ntfy.sh'
      const err = httpUrlError(server.replace(/\/+$/, ''))

      if (err) {
        e.server_url = err
      }

      if (!RE.ntfyTopic.test(v.topic.trim())) {
        e.topic = '1–64 letters, digits, - or _'
      }

      break
    }

    case 'email': {
      const host = v.host.trim()

      if (!host || host.length > 253 || /[\s/:]/.test(host)) {
        e.host = 'Enter the SMTP host, e.g. smtp.example.com'
      }

      if (v.port.trim()) {
        const port = Number(v.port)

        if (!Number.isInteger(port) || port < 1 || port > 65535) {
          e.port = 'A TCP port (1–65535)'
        }
      }

      if (!RE.from.test(v.from.trim())) {
        e.from = 'An address, e.g. staqly <bot@example.com>'
      }

      if (!v.to.length) {
        e.to = 'Add at least one recipient'
      } else if (v.to.length > 20) {
        e.to = 'At most 20 recipients'
      } else {
        const bad = v.to.find((t) => !RE.email.test(t))

        if (bad) {
          e.to = `Invalid address ${bad}`
        }
      }

      if (/[\r\n]/.test(v.username)) {
        e.username = 'Is invalid'
      }

      break
    }
  }

  return e
}

// ---- request body ----

type Config = NonNullable<AlertInput['config']>

/**
 * Form values → POST/PATCH body. Unchanged secrets are left out (the server
 * keeps them), removed ones are sent as null. On edit, cleared plain fields
 * are sent as null because the server merges config keys.
 */
export function valuesToChannelInput(
  type: AlertType,
  v: ChannelFormValues,
  opts: { editing?: boolean; stored?: SecretField[]; secrets?: SecretState } = {},
): AlertInput {
  const stored = opts.stored ?? []
  const secrets = opts.secrets ?? {}
  const editing = !!opts.editing
  const config: Config = {}

  const plain = <K extends keyof Config>(key: K, value: Config[K] | '' | undefined) => {
    const empty = value === '' || value === undefined || (Array.isArray(value) && value.length === 0)

    if (!empty) {
      config[key] = value as Config[K]
    } else if (editing) {
      config[key] = null
    }
  }
  const secret = (field: SecretField, use = true) => {
    if (!use) {
      if (editing && stored.includes(field)) {
        config[field] = null
      }

      return
    }

    const action = secrets[field] ?? (stored.includes(field) ? 'keep' : 'set')

    if (action === 'remove') {
      if (editing) {
        config[field] = null
      }
    } else if (action === 'set' && v[field].trim()) {
      config[field] = v[field].trim()
    }
  }

  switch (type) {
    case 'telegram':
      secret('bot_token')
      plain('chat_id', v.chat_id.trim())
      break
    case 'slack':
    case 'discord': {
      const bot = v.delivery === 'bot'

      secret('webhook_url', !bot)
      secret('bot_token', bot)
      plain('channel', bot ? v.channel.trim() : '')

      break
    }

    case 'ntfy':
      config.server_url = (v.server_url.trim() || 'https://ntfy.sh').replace(/\/+$/, '')
      config.topic = v.topic.trim()
      secret('access_token')
      plain('priority', v.priority ? Number(v.priority) : undefined)
      break
    case 'email':
      config.host = v.host.trim()
      plain('port', v.port.trim() ? Number(v.port) : undefined)
      config.tls = v.tls
      plain('username', v.username.trim())
      secret('password')
      config.from = v.from.trim()
      config.to = v.to
      break
  }

  return {
    name: v.name.trim(),
    ...(editing ? {} : { type }),
    enabled: v.enabled,
    config,
    events: v.events,
    collection_ids: v.collection_ids,
    digest_window_seconds: v.digest_window_seconds,
  }
}

// ---- display ----

export type ChannelHealth = 'active' | 'paused' | 'disabled' | 'failing'

/** "disabled" = switched off by the server after repeated failures. */
export function channelHealth(ch: Pick<Alert, 'enabled' | 'disabled_reason' | 'fail_count'>): ChannelHealth {
  if (!ch.enabled) {
    return ch.disabled_reason ? 'disabled' : 'paused'
  }

  return ch.fail_count > 0 ? 'failing' : 'active'
}

/** One-line destination summary, e.g. "chat 12345 · polling" or "#C0123 via bot". */
export function channelTarget(ch: Alert): string {
  const c = ch.config

  switch (ch.type) {
    case 'telegram':
      return `Chat ${str(c.chat_id)}`
    case 'slack':
    case 'discord':
      if (isMasked(c.webhook_url)) {
        return `Webhook · ${maskedHint(c.webhook_url)}`
      }

      return `Bot · channel ${str(c.channel)}`
    case 'ntfy': {
      let host = str(c.server_url)

      try {
        host = new URL(host).host
      } catch {
        /* keep the raw value */
      }

      return `${host}/${str(c.topic)}`
    }

    case 'email': {
      const to = strList(c.to)

      return `${to[0] ?? ''}${to.length > 1 ? ` +${to.length - 1}` : ''} via ${str(c.host)}`
    }
  }

  return ''
}

/** A long random ntfy topic: anyone who knows a topic on ntfy.sh can read it. */
export function randomTopic() {
  const bytes = new Uint8Array(9)

  crypto.getRandomValues(bytes)

  return (
    'staqly-' +
    Array.from(bytes, (b) => b.toString(36).padStart(2, '0'))
      .join('')
      .slice(0, 14)
  )
}
