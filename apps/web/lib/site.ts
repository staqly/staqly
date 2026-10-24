import { env } from './env'

export const DOCS = env.docsUrl
export const GITHUB = 'https://github.com/staqly/staqly'
// Hosted dashboard; /signup creates an account and its first workspace (apps/platform/app/signup), /login signs in.
export const APP = env.appUrl
export const SIGNUP = `${APP}/signup`
/** Sign-up with a plan picked on the pricing page: after the first sign-in the plan picker opens with it. */
export const signupFor = (plan: string, interval?: 'month' | 'year') =>
  `${SIGNUP}?${new URLSearchParams({ plan, ...(interval ? { interval } : {}) })}`
// ponytail: assumes the hosted API/MCP share the dashboard host (https://docs.staqly.ai/local-vs-cloud#staqly-cloud).
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
// The header and the home hero: as wide as the docs home hero (docs.staqly.ai), so the hero text lines up with the logo.
export const wrapWide = 'mx-auto w-full max-w-7xl px-5 sm:px-8'

export const SITE_URL = env.siteUrl
export const TAGLINE = 'The local-first social media scheduler for people and their AI agents'
export const DESCRIPTION =
  'Staqly is the local-first social media scheduler for people and their AI agents. You and your agents write posts into collections, and a schedule publishes them to X, LinkedIn, Instagram, TikTok and more, or through your own n8n workflow or script.'
