import { TRIAL_DAYS } from './plans'

/** The questions on the home page (its FAQ section and FAQPage structured data) and in llms-full.txt. */
export const faqs: [string, string][] = [
  [
    'What is Staqly, in one sentence?',
    'A social media scheduler: you (or your AI) write and design posts once, pick the channels and a time or a weekly rhythm, and Staqly publishes them to every platform you connected, retries failures and keeps the links.',
  ],
  [
    'Which platforms can it publish to?',
    "X, LinkedIn, Instagram, Facebook, Threads, TikTok, YouTube, Pinterest, Bluesky, Mastodon, Telegram, Discord, Slack, Reddit, WordPress, Medium and more, directly. Anything else goes through your own automation: an n8n workflow or a script that publishes the post and sends the link back. The composer knows each platform's limits and settings.",
  ],
  [
    'Can I use it without an AI agent?',
    'Yes. The dashboard has a composer, a calendar you drag posts around in, per-channel previews, a media library and design and video editors. The assistant and agents are optional.',
  ],
  [
    'Does AI publish without me?',
    'Only if you let it. The built-in assistant asks you to Allow every change. For your own agents, writing and publishing are separate permissions: a drafting agent fills collections, and only your schedules, you or a key with the publish permission put a post on your channels.',
  ],
  [
    'What happens when a platform is down?',
    'Publishing is a queued job. A failed channel retries on its own, the others are not held back, and you get an alert with the platform’s error and a Retry for that channel.',
  ],
  [
    'What does it cost?',
    `Plans are per user and start at €15 a month (€12 billed yearly), incl. VAT; each plan sets how many channels, new posts a month, AI allowance and storage the workspace shares. Starter has a ${TRIAL_DAYS}-day free trial. The AI assistant, the editors, MCP and the API come with every plan.`,
  ],
  [
    'What makes it different from other schedulers?',
    'It is built for agents as much as for people: Claude Code, Codex or n8n write posts over MCP or REST, collections carry their own schedules, and when a collection runs dry the schedule asks your agent for more. It is also open source and runs on your own machine if you want.',
  ],
  [
    'Can I host it myself?',
    'Yes. Staqly is AGPL-3.0: one binary or one Docker container, no plan limits, your data on your disk. It runs one workspace and prints an access token at start for your browser and agents; OAuth platforms such as X need your own developer app. The hosted version saves you backups and updates.',
  ],
]
