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
    slug: 'content-publishing',
    title: 'Approve AI-generated posts and images before they publish',
    summary:
      'Your n8n workflow or agent generates captions and images. You pick the good ones on your phone; only those go live.',
    intro:
      'Generating content is cheap; publishing the wrong post is not. staqly sits between the generator and the publisher, so a human sees every image and caption before it goes out.',
    problem:
      'Generators are fast and inconsistent. One post in ten is off-brand, wrong or just weird, and you only notice after it is live. Reviewing in a spreadsheet or a shared folder does not scale, and the publisher has no idea what you decided.',
    steps: [
      'The workflow generates an image and caption and creates an item in the “Instagram Avatar” collection.',
      'It lands in your Inbox with an image preview, and the prompt and seed in its metadata.',
      'You approve, reject with feedback, edit the caption first, or ask for a regeneration.',
      'A signed webhook tells n8n what you decided. Approved items get published; the post ID is written back to the item.',
      'Rejected items keep your feedback, so the next generation can use it.',
    ],
    code: {
      title: 'n8n → staqly (REST)',
      body: `# upload the image and create the item in one call
$ curl -X POST "$API/v1/collections/Instagram%20Avatar/items" \\
    -F file=@avatar.png \\
    -F 'item={"title":"Avatar #1","content":"Golden hour ☀️","metadata":{"seed":42}}'
→ 201 {"id":"itm_8f2...","status":"pending"}

# after publishing, record what happened
$ curl -X PATCH $API/v1/items/itm_8f2... \\
    -H 'Content-Type: application/json' \\
    -d '{"status":"completed","metadata_patch":{"instagram_post_id":"1790..."}}'`,
    },
    result:
      'The generator keeps generating. You spend a minute a day swiping through the best output, and nothing reaches your audience without a yes.',
    docs: [
      ['Instagram Avatar, end to end', '/cookbook/instagram-avatar'],
      ['n8n integration', '/integrations/n8n'],
      ['Webhooks', '/automation/webhooks'],
    ],
  },
  {
    slug: 'risky-agent-actions',
    title: 'Make your coding agent ask before it does something risky',
    summary:
      'Claude Code or Codex proposes a migration, an email or a payment, waits for your decision and runs only the version you approved.',
    intro:
      'Some things an agent should never do on its own: run a production migration, email a customer, publish a post, spend money. With staqly the agent asks first, and acts only on what you approved, even if you edited it.',
    problem:
      'Agents are good at preparing the action and bad at knowing when it is safe to run. Approving in the terminal means sitting next to it; letting it run means trusting it with production.',
    steps: [
      'The agent calls create_item with a description of the action and an expiry, e.g. “Run migration 0042 on prod”.',
      'The item shows up in your Inbox and on your phone.',
      'The agent calls await_decision and blocks until you decide.',
      'You approve, reject with a reason, or edit the SQL and approve the fixed version.',
      'The agent runs exactly what you approved and calls report_result with the outcome.',
    ],
    code: {
      title: 'MCP tool calls',
      body: `# 1. store the proposal
create_item {"collection":"Deploys",
  "title":"Run the users migration",
  "content":"ALTER TABLE ...","expires_in":"1h"}

# 2. wait for the decision (call again on timeout)
await_decision {"item_id":"itm_..."}
→ approved · content edited by you

# 3. record what happened
report_result {"item_id":"itm_...",
  "status":"completed","result":{"rows":1200}}`,
    },
    result:
      'Your agent keeps its autonomy for the boring parts and stops for the parts that matter. Every decision, edit and result is in the item history.',
    docs: [
      ['Gate a risky agent action', '/cookbook/approval-gate'],
      ['MCP tools reference', '/mcp/tools'],
      ['Connect Claude Code', '/agents/claude-code'],
    ],
  },
  {
    slug: 'deploy-approval',
    title: 'Human sign-off before CI deploys to production',
    summary:
      'One step in GitHub Actions pauses the pipeline until someone approves the deploy, from the Inbox, a phone or chat.',
    intro:
      'You want a person to sign off before your pipeline deploys to production, without building a custom approval UI. staqly turns a human decision into an ordinary pipeline step.',
    problem:
      'Built-in environment approvals are tied to one CI vendor and one UI. You want the request where you already are, with the commit, the run link and a timeout.',
    steps: [
      'After build and test, the pipeline runs staqly push with the commit message and run URL.',
      'A “Deploy to production?” item appears in the Inbox and in your notification channels.',
      'Approve and the step exits 0, so the deploy runs.',
      'Reject and it exits 2; let it time out and it exits 3. Either way the job fails safely.',
    ],
    code: {
      title: '.github/workflows/deploy.yml',
      body: `- name: Wait for approval
  run: |
    staqly push Deploys \\
      --title "Deploy \${GITHUB_REF_NAME} to production" \\
      --content "$(git log -1 --pretty=%B)" \\
      --external-ref "$GITHUB_SHA" \\
      --timeout 2h
# exit 0 approved · 2 rejected · 3 timed out`,
    },
    result:
      'Deploys still run on their own schedule, but production only changes after a human said yes, and you can see who did and when.',
    docs: [
      ['Approval gate in CI', '/cookbook/ci-approval-gate'],
      ['staqly CLI', '/cli'],
      ['Cloud deployment', '/self-hosting/cloud-deployment'],
    ],
  },
  {
    slug: 'approve-from-chat',
    title: 'Approve from Telegram, Slack or Discord with one tap',
    summary:
      'Every new pending item arrives as a chat message with Approve and Reject buttons. No dashboard tab to keep open.',
    intro:
      'You do not want to keep the dashboard open just to catch new items. Connect a chat bot and every pending item comes to you, with buttons.',
    problem:
      'Approvals that wait for you to open a web app wait too long. Agents stall, expiries pass, and the queue grows while you are away from your desk.',
    steps: [
      'Create a bot (for Telegram, via @BotFather) and add it as a notification channel.',
      'staqly messages you about every new pending item.',
      'Tap Approve or Reject in the chat; the decision reaches agents and webhooks like any other.',
      'Telegram works the same in a hosted workspace and on a self-hosted laptop with no public URL; Slack and Discord buttons need a public HTTPS URL.',
    ],
    code: {
      title: 'Terminal',
      body: `$ curl -X POST $API/v1/notification-channels \\
    -H 'Content-Type: application/json' -d '{
      "name": "My phone",
      "type": "telegram",
      "config": {"bot_token": "123456:ABC...", "chat_id": "123456789"}
    }'

# send a test message
$ curl -X POST $API/v1/notification-channels/$CHANNEL_ID/test
→ {"ok":true}`,
    },
    result: 'Decisions take seconds instead of hours, and your agents stop waiting on you.',
    docs: [
      ['Approve from chat', '/cookbook/approve-from-chat'],
      ['Notifications', '/automation/notifications'],
    ],
  },
  {
    slug: 'research-radar',
    title: 'Turn ten open tabs into one sorted research inbox',
    summary: 'Watchers follow any RSS/Atom feed or web page; keyword filters drop the noise before it reaches you.',
    intro:
      'Keeping up with a fast-moving topic means checking the same sites every day. Let staqly do the checking and hand you a short, sorted list.',
    problem:
      'Feeds are noisy, and reading them is a daily chore. Most of it does not matter, and the one release you needed gets buried.',
    steps: [
      'Create a collection, e.g. “AI Radar”.',
      'Add a watcher per source: a blog or news feed, the GitHub releases feed of a tool you depend on, a changelog page without a feed.',
      'Give each watcher keywords to keep and words to skip, so obvious noise never becomes an item.',
      'Optionally get new items in a chat channel or send them to a webhook.',
    ],
    code: {
      title: 'Terminal',
      body: `# follow a tool's releases, checked every hour, skipping alphas
$ curl -X POST $API/v1/watchers \\
    -H 'Content-Type: application/json' -d '{
    "name": "Codex releases", "kind": "rss",
    "url": "https://github.com/openai/codex/releases.atom",
    "collection": "AI Radar", "every": "1h",
    "exclude_keywords": ["alpha"]}'`,
    },
    result: 'One Inbox instead of ten tabs, with the noise filtered out before it reaches you.',
    docs: [
      ['Watchers', '/automation/watchers'],
      ['Watchers in the dashboard', '/dashboard/watchers'],
    ],
  },
  {
    slug: 'review-on-your-phone',
    title: 'Let agents work overnight, review on your phone',
    summary:
      'Scan a QR code once and your phone opens your workspace signed in. Swipe through the Inbox from anywhere.',
    intro:
      'Agents keep producing while you are away from your desk. Connect your phone once and decide wherever you are, instead of finding a stalled run in the morning.',
    problem: 'A long agent run stops at the first question, and you only notice when you are back at the laptop.',
    steps: [
      'In the dashboard, open “Connect a device” and scan the QR code with your phone.',
      'The phone opens staqly already signed in: no key to type. The link works once and expires after five minutes.',
      'Your agent keeps working and waits on await_decision only where it needs you.',
      'Swipe between items and tap Approve or Reject; the agent gets your answer once the 10-second Undo has passed.',
      'Revoke the phone any time under Settings → Devices.',
    ],
    code: {
      title: 'claude',
      body: `# your agent, at 2 a.m.
create_item {"collection":"Refactor",
  "title":"Drop the legacy users table?"}
await_decision {"item_id":"itm_..."}

# you, at 8 a.m., on your phone → tap Approve
→ approved · the agent carries on`,
    },
    result: 'Your agents run overnight; you approve their output over breakfast.',
    docs: [
      ['Review on your phone', '/cookbook/review-on-your-phone'],
      ['Phones and tablets', '/dashboard/phones-and-tablets'],
    ],
  },
]
