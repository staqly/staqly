// Each use case mirrors a recipe in the docs cookbook (staqly/apps/docs/content/docs/cookbook).
export type UseCase = {
  slug: string
  title: string
  summary: string
  intro: string
  problem: string
  steps: string[]
  code: { title: string; body: string }
  result: string
  docs: [string, string][]
}

export const allCases: UseCase[] = [
  {
    slug: 'agent-fills-a-collection',
    title: 'Let your agent write a week of posts, publish one every morning',
    summary:
      'Claude Code or Codex drafts into a collection; a schedule publishes the next waiting post every weekday at 9 and asks the agent for more when it runs dry.',
    intro:
      'Writing is cheap when an agent does it; keeping a steady rhythm is the hard part. Staqly holds the drafts and sends one out each morning, and tells the agent when the collection is empty.',
    problem:
      'An agent can write ten posts in a minute, but someone still has to copy each one into a platform at the right time. Giving every post a timestamp by hand is the same chore in a calendar.',
    steps: [
      'You create a collection and a schedule: weekdays at 09:00, the Telegram and Bluesky channels, "the next waiting post".',
      'Your agent calls create_post five times; the drafts wait in the collection.',
      'Every morning the schedule publishes the oldest draft to both channels and records the links.',
      'When nothing waits, the schedule records a request. The agent sees it in list_requested_work and writes the next batch.',
      'You glance at the calendar on your phone and drag a post to another day when the news changes.',
    ],
    code: {
      title: 'MCP tool calls',
      body: `# the agent, once
create_schedule {"collection":"Daily tips","name":"Weekday mornings",
  "kind":"recurring","rule":{"times":["09:00"],"weekdays":[1,2,3,4,5],"tz":"Europe/Warsaw"},
  "channels":["ch_tg...","ch_bsky..."],"pick":"next","source":"agent"}

# every night
create_post {"collection":"Daily tips","content":"Tip: drag a post on the calendar to move it.",
  "targets":[{"channel_id":"ch_tg..."},{"channel_id":"ch_bsky...","content":"Drag a post to move it."}]}

# when the collection runs dry
list_requested_work → {"kind":"posts","schedule":{"name":"Weekday mornings"}, ...}`,
    },
    result:
      'The agent writes, staqly keeps the rhythm, you keep the final say on the calendar. Nothing is copied by hand.',
    docs: [
      ['Let an agent fill a collection', '/cookbook/agent-fills-a-collection'],
      ['Schedules', '/automation/schedules'],
      ['MCP tools reference', '/mcp/tools'],
    ],
  },
  {
    slug: 'one-post-every-channel',
    title: 'One post, every channel, each in its own length',
    summary:
      'Write once, pick the channels, and give X its 280 characters while LinkedIn gets the long version. Staqly checks each platform’s limits before anything goes out.',
    intro:
      'Every platform wants the same news in a different shape. The composer shows one text with a tab per channel, a counter per platform and a preview that says what each one objects to.',
    problem:
      'Posting the same announcement to four platforms means four tabs, four character limits and four places where a typo can slip in after the first one went out.',
    steps: [
      'Connect the channels once: a token for Telegram or Bluesky, your own developer app for OAuth platforms.',
      'Write the post, click the channel avatars, and open a channel’s tab to shorten or retitle it.',
      'The preview lists what a platform objects to: too long, media missing, a title required.',
      'Schedule it or publish now. Each channel publishes on its own; one failure does not stop the others.',
      'A failed channel shows the platform’s error and a Retry for that channel alone.',
    ],
    code: {
      title: 'REST',
      body: `$ curl -X POST $API/v1/posts -H 'Content-Type: application/json' -d '{
    "collection":"Product updates",
    "content":"v1.4 is out: schedules, channels and a calendar. Full notes on the blog.",
    "media":["med_t4w6..."],
    "targets":[
      {"channel_id":"ch_linkedin..."},
      {"channel_id":"ch_x...","content":"v1.4: schedules, channels, a calendar."},
      {"channel_id":"ch_reddit...","settings":{"title":"staqly v1.4"}}
    ],
    "publish_at":"2026-10-21T09:00:00Z","status":"scheduled"}'
→ 201 {"id":"post_oali...","status":"scheduled"}

$ curl $API/v1/posts/post_oali.../preview
→ {"targets":[{"channel_id":"ch_x...","ok":true}, ...]}`,
    },
    result: 'One place to write, one calendar to watch, and a link per platform when it is done.',
    docs: [
      ['Posts in the dashboard', '/dashboard/posts'],
      ['Channels', '/channels'],
      ['API overview', '/api'],
    ],
  },
  {
    slug: 'publish-through-your-own-automation',
    title: 'Publish through n8n or your own script, keep the calendar',
    summary:
      'A channel on "your own automation" hands each post to a webhook receiver and waits for the link back. Any platform, any internal tool, same calendar.',
    intro:
      'Staqly cannot reach every platform, and some publishing steps are yours alone. Add a channel that hands the post to your code; the post’s status and history look like any other.',
    problem:
      'A newsletter service, an internal tool or a platform without an official API falls outside every scheduler. You end up with a second calendar and a cron job nobody remembers.',
    steps: [
      'Add a channel: Your own automation. Subscribe a webhook to post.publish_requested.',
      'At the post’s time staqly records the request with the text, the media and how long it will wait.',
      'Your receiver (n8n, a script) publishes wherever it should and reports the URL back.',
      'The target shows Published with your link, or Failed with your error and a Retry.',
      'Nothing reported within the collection’s limit fails the target, so nothing hangs forever.',
    ],
    code: {
      title: 'Your receiver',
      body: `# staqly → you (signed webhook)
POST /hooks/staqly   X-Staqly-Event: post.publish_requested
{"data":{"post":{"id":"post_oali...","content":"..."},
         "channel":{"id":"ch_mine..."},"target":{"content":"..."},"timeout_seconds":3600}}

# you → staqly
$ curl -X POST $API/v1/posts/post_oali.../targets/ch_mine.../result \\
    -H 'Content-Type: application/json' \\
    -d '{"status":"published","url":"https://example.com/p/1790"}'`,
    },
    result: 'Every channel on one calendar, including the ones only your code can reach.',
    docs: [
      ['Your own automation', '/channels/your-own-automation'],
      ['Publish through n8n', '/cookbook/publish-through-n8n'],
      ['Webhooks', '/automation/webhooks'],
    ],
  },
  {
    slug: 'posts-from-your-endpoint',
    title: 'Feed a schedule from a tiny service of yours',
    summary:
      'When a collection runs dry, the schedule calls your endpoint, signed, and publishes the posts it answers with. A tip of the day in thirty lines.',
    intro:
      'Not every stream needs an agent. A schedule can ask a small HTTP service for the next post and publish whatever it returns, at that same slot.',
    problem:
      'A daily tip, a weekly stat, a quote from your changelog: content that a function can produce, but that still needs a calendar, channels and a record of what went out.',
    steps: [
      'Create a schedule with "Call an endpoint that returns posts" and a secret.',
      'Your endpoint verifies the signature and answers {"posts":[...]}.',
      'The run creates the posts in the collection and publishes them to the schedule’s channels.',
      'Drafts written by hand or by an agent are always picked first; the endpoint only fills the gaps.',
    ],
    code: {
      title: 'Your endpoint',
      body: `# staqly → you, signed like a webhook
POST /tips   X-Staqly-Signature: sha256=...
{"schedule":{"name":"Tip of the day"},"channels":["ch_tg..."],"slot":"2026-10-21T07:00:00Z"}

# you → staqly
200 {"posts":[{"content":"Tip of the day: retry one channel, not the whole post.","tags":["tips"]}]}`,
    },
    result: 'A stream that never runs dry, with every post on the calendar and in the history.',
    docs: [
      ['Posts from your endpoint', '/cookbook/posts-from-your-endpoint'],
      ['Schedules', '/automation/schedules'],
    ],
  },
  {
    slug: 'know-when-it-went-out',
    title: 'Know the moment a post went out, or failed',
    summary:
      'Telegram, Slack, Discord, ntfy or email tell you when a post was published, when a channel needs reconnecting and when a schedule asks for more.',
    intro:
      'A scheduler you have to watch is a chore. Alerts bring the three things that matter to your phone: published, failed, reconnect needed.',
    problem:
      'A token expires quietly, a platform refuses a post at 9:00, and you learn about it from a reader at noon.',
    steps: [
      'Create an alert: a Telegram bot, a Slack webhook, an ntfy topic or an SMTP account.',
      'By default it tells about post.published, post.failed and channel.needs_reconnect.',
      'Add schedule.posts_requested to hear when a collection runs dry, or post.scheduled for every new entry on the calendar.',
      'Each message links to the post in the dashboard; batch them with a digest window when a schedule publishes a lot.',
    ],
    code: {
      title: 'Terminal',
      body: `$ curl -X POST $API/v1/alerts -H 'Content-Type: application/json' -d '{
    "name":"My phone","type":"ntfy",
    "config":{"topic":"staqly-7f3k9q2","server_url":"https://ntfy.sh"},
    "events":["post.published","post.failed","channel.needs_reconnect","schedule.posts_requested"]}'

$ curl -X POST $API/v1/alerts/$ALERT_ID/test
→ {"ok":true}`,
    },
    result: 'You find out before your readers do, and fix it from your phone.',
    docs: [
      ['Alerts', '/automation/alerts'],
      ['Event types', '/api/events'],
    ],
  },
  {
    slug: 'manage-from-your-phone',
    title: 'Agents write overnight, you shuffle the calendar over breakfast',
    summary:
      'Scan a QR code once and your phone opens your workspace signed in. Move a post, publish one now, reconnect a channel from anywhere on your Wi-Fi.',
    intro:
      'The drafts are written by the time you wake up. Connect your phone once and decide what goes out today from the couch, instead of at the laptop.',
    problem: 'A calendar that lives on one computer is a calendar you check too late.',
    steps: [
      'In the dashboard, open "Connect a device" and scan the QR code with your phone.',
      'The phone opens staqly already signed in: no key to type. The link works once and expires after five minutes.',
      'The Schedule tab shows the day as a list; tap a post to publish it now, reschedule or edit.',
      'Channels says when an account needs reconnecting; Alerts reach the same phone.',
      'Revoke the phone any time under Settings → Devices.',
    ],
    code: {
      title: 'Terminal',
      body: `# open the dashboard to your Wi-Fi
$ PUBLIC_DASHBOARD_URL=http://192.168.1.23:8080 staqly serve --expose

# sign this computer in, then Settings → Devices → Connect a device
$ staqly serve connect-link`,
    },
    result: 'Your agents run overnight; you shape the calendar over breakfast.',
    docs: [
      ['Manage from your phone', '/cookbook/manage-from-your-phone'],
      ['Phones and tablets', '/dashboard/phones-and-tablets'],
    ],
  },
]
