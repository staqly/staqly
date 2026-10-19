import { env } from './env'

export const DOCS = env.docsUrl
export const GITHUB = 'https://github.com/staqly/staqly'
// Hosted dashboard; /setup creates a workspace (apps/platform/app/setup), login lives there too.
export const APP = env.appUrl
export const SIGNUP = `${APP}/setup`
// ponytail: assumes the hosted API/MCP share the dashboard host (docs/cloud.md proxy setup).
export const MCP_URL = `${APP}/mcp`
// Self-hosting lives in the docs: install, run, connect an agent.
export const SELF_HOST = `${DOCS}/quickstart`
export const INSTALL = 'curl -fsSL https://raw.githubusercontent.com/staqly/staqly/main/scripts/install.sh | sh'

export const btnPrimary =
  'inline-flex h-11 items-center gap-2 rounded-xl sq-rainbow px-5 text-sm font-semibold shadow-[0_8px_24px_-8px_color-mix(in_oklch,var(--brand)_70%,transparent)] transition hover:brightness-110'
export const btnSecondary =
  'inline-flex h-11 items-center gap-2 rounded-xl border border-border bg-card px-5 text-sm font-semibold transition-colors hover:bg-accent'
// Standalone arrow links ("All use cases ->"). On touch screens the hit area grows to 40px tall without moving
// anything: the extra padding is cancelled by a negative margin. A caller with a top margin must lower it by 2.5 on
// pointer-coarse (mt-5 pointer-coarse:mt-2.5), or the -my-2.5 replaces it.
export const arrowLink =
  'inline-flex items-center gap-1.5 text-sm font-semibold text-brand-strong hover:underline pointer-coarse:-my-2.5 pointer-coarse:py-2.5'
export const wrap = 'mx-auto w-full max-w-6xl px-5 sm:px-8'

export const SITE_URL = env.siteUrl
export const TAGLINE = 'The post scheduler for people and their agents'
export const DESCRIPTION =
  'Staqly is a local-first post scheduler. You and your AI agents write posts into collections, connect the channels they go to, and a schedule publishes one every morning. Your drafts, credentials and history stay on your machine.'
export const KEYWORDS = [
  'post scheduler',
  'social media scheduler',
  'self-hosted social media scheduler',
  'open source social media scheduler',
  'Buffer alternative',
  'AI agent social media',
  'MCP server',
  'Model Context Protocol',
  'Claude Code',
  'OpenAI Codex',
  'OpenCode',
  'Cursor',
  'n8n',
  'content calendar',
  'schedule posts',
  'webhooks',
  'local-first',
  'self-hosted',
  'open source',
]
