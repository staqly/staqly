# Notifications and approving from chat

staqly can tell you when something needs review and let you decide without
opening the dashboard. A workspace can have several **notification channels**:

| Type | Delivery | Decide from the message |
|---|---|---|
| `telegram` | Bot API message (images are uploaded as photos) | Inline **Approve / Reject / Reject with reason** buttons |
| `slack` | Incoming webhook or `chat.postMessage` (Block Kit) | Buttons with a Slack app (signing secret); one-tap links otherwise |
| `discord` | Channel webhook (images attached) or a bot message | Buttons and a reason form with a Discord application; one-tap links otherwise |
| `ntfy` | ntfy publish (ntfy.sh or self-hosted) | **Approve / Reject** notification actions |
| `email` | SMTP (STARTTLS by default), HTML + text | Approve / Reject links |

Every decision made from chat or a link goes through the same path as the
dashboard. Rules, version checks, `item.approved` / `item.rejected` events and
your webhooks all fire as usual. The actor is recorded as `Telegram: @name`,
`Slack: name`, `Discord: name` or `Link: <channel name>`.

## Managing channels

You can manage channels in the dashboard or through the API (scope
`channels:manage`; API keys with `admin` also work):

```bash
curl -X POST $API/v1/notification-channels -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' -d '{
    "name": "My phone",
    "type": "telegram",
    "config": {"bot_token": "123456:ABC…", "chat_id": "123456789"},
    "events": ["item.created", "item.escalated"],
    "collection_ids": [],
    "digest_window_seconds": 0
  }'
```

* **events.** These are glob patterns. The default is a new item waiting for
  review (`item.created`, which only fires when the new item is `pending`)
  plus escalations from rules (`item.escalated`). You can also add others,
  for example `item.approved`, `item.rejected`, `watcher.degraded` or
  `item.*`.
* **collection_ids.** Limits the channel to these collections. Leave it empty
  for all collections.
* **digest_window_seconds.** Collects everything that arrives within the
  window into one message, for example `300` to get at most one message every
  5 minutes. Digest messages still have per-item buttons for the first 10
  items (5 on Discord).
* **Secrets** (`bot_token`, `webhook_url`, `signing_secret`, `access_token`,
  `password`) are encrypted at rest and always come back **masked** (for
  example `••••abcd`). To keep a secret, send the masked value back or leave
  the key out. To remove a key, send `null`.
* **Changing the destination needs the secret again.** When an update changes
  where a channel sends — Telegram `chat_id`, Slack/Discord `channel`, ntfy
  `server_url`, email `host`/`port`/`tls`/`username` — the stored secret that
  would be sent there (`bot_token`, `access_token`, `password`) must be
  re-entered in the same request (a masked echo doesn't count), otherwise
  **422**. This stops someone who can manage channels but can't read secrets
  from pointing a channel at their own server to collect them.
* `POST /v1/notification-channels/{id}/test` sends a test message right away
  and returns `{"ok": true}` or `{"ok": false, "error": "…"}`. It is
  rate-limited per API key / session (burst 5, then 10 per minute; **429**).
* `GET /v1/notification-channels/{id}/deliveries` lists recent notifications
  with their status (`pending`, `sent`, `failed`, `skipped`).
* **Failures.** Sending is retried with backoff. A channel that fails
  `NOTIFY_MAX_FAILS` times in a row (default 10) is **disabled**:
  `enabled=false` and `disabled_reason` explains why. Fix the cause, then
  `PATCH {"enabled": true}`. Messages queued for a disabled channel (disabled
  by hand or automatically) stay `pending` and are not sent; re-enabling it
  within `NOTIFY_DISABLED_GRACE` (default 24h) delivers them. Once the channel
  has been disabled for longer than that, the next retention run (daily)
  marks them `skipped` with the reason "channel disabled", and re-enabling no
  longer sends them. The grace period restarts every time the channel is
  disabled. Disabling a channel revokes the one-tap decision links it sent;
  so does deleting it.
* **Delivery.** Up to `NOTIFY_CONCURRENCY` channels (default 4) are sent in
  parallel, so one slow SMTP server or dead webhook doesn't hold up the rest.
  Finished outbox rows (`sent`/`skipped`/`failed`) are pruned after
  `RETENTION_NOTIFICATIONS_DAYS` (default 30, `0` keeps them).
* **Stale items.** A notification about an item that is no longer pending by
  the time it's sent is skipped. For example, a rule may have auto-approved it
  in the same transaction.
* **Plan limits** (cloud): Free 2 channels, Indie 10, Team unlimited. Local
  installs have no limits.

## One-tap decision links

ntfy, email, Slack without an app, and Discord webhooks get **Approve /
Reject links**:

```
https://your-api/v1/decide/dl_…
```

* Each link is **single-use** and expires after 7 days (`NOTIFY_LINK_TTL`).
  Only a SHA-256 hash of the token is stored.
* Opening a link (**GET**) shows a confirmation page with the item. Only
  **POST**, which happens when you press the button on that page, makes the
  decision. This means link-preview bots in Slack, mail scanners and similar
  tools can't approve anything by opening the URL. ntfy's action buttons POST
  directly.
* The reject page has an optional *reason* field.
* If the item was already decided, you get a friendly "already approved"
  message (HTTP 409). A link that was used or has expired returns 410, and so
  does a link whose channel was disabled; links of deleted channels are gone
  (404).
* **You approve what you saw.** A link (and every chat button) is bound to the
  item version shown in the message. If the agent changed the item since,
  the link answers "item changed since this message — open it in staqly"
  (409) and nothing is decided; review the current version in the dashboard.
* Links are bearer credentials. Anyone who has the message can use them, so
  only send them to private destinations.

Links point to `NOTIFY_PUBLIC_API_URL`. By default that is the dashboard
proxy, `PUBLIC_WEB_URL` + `/api` (the API itself listens on 127.0.0.1, phones
reach only the dashboard), or `PUBLIC_API_URL` when `PUBLIC_WEB_URL` is unset.
The address must be reachable from the device where you tap the link.

* Local install: set `PUBLIC_WEB_URL=http://<this computer's LAN IP>:8080`
  and links work on any device on your Wi-Fi.
* Cloud: set `PUBLIC_WEB_URL=https://your-domain` (or `NOTIFY_PUBLIC_API_URL`
  to your public API URL).

Dashboard links in messages use `PUBLIC_WEB_URL` (`…/items/{id}`).

## Telegram

1. In Telegram, open **@BotFather** and send `/newbot`. Choose a name and copy
   the **bot token** (`123456:ABC…`).
2. Open a chat with your new bot and send `/start`.
3. Find your **chat ID**:
   * Option 1: open `https://api.telegram.org/bot<TOKEN>/getUpdates` in a
     browser and read `message.chat.id`.
   * Option 2: create the channel with any chat ID and send `/chatid` to the
     bot. It replies with the chat ID and your user ID.

   For a group, add the bot to the group and use the group's (negative) ID.
4. Create the channel: `{"type": "telegram", "config": {"bot_token": "…", "chat_id": "…"}}`.

**Who may decide:** by default only the user whose private chat is `chat_id`.
For groups, set `approver_user_ids` (numeric Telegram user IDs). Only those
users' button presses count. Anyone else gets "You are not on this channel's
approver list".

**Buttons:**

* **✅ Approve / ❌ Reject** decide immediately. The buttons are then replaced
  by the result, for example "✅ Approved by @you".
* **💬 Reject with reason** makes the bot ask you to *reply* with the reason.
  Your reply rejects the item, and the reply text becomes its feedback. The
  prompt is bound to the item version the original message showed: if the
  agent changed the item in the meantime, the reply is answered with "item
  changed since this message — open it in staqly" and nothing is decided.

**Two ways to receive button presses:**

* `mode: "polling"` (default). staqly long-polls Telegram (`getUpdates`
  every few seconds; `TELEGRAM_POLL_INTERVAL`). **This works on a laptop with
  no public URL.** Use one channel per bot: two pollers on the same bot steal
  each other's updates.
* `mode: "webhook"` (cloud). When the channel is saved, staqly calls
  `setWebhook` itself. The webhook URL is
  `NOTIFY_PUBLIC_API_URL/v1/integrations/telegram/{channel_id}`, with a random
  `secret_token`, so Telegram must be able to reach your API over HTTPS.
  Requests without the secret header are rejected. Switching back to polling
  calls `deleteWebhook`.

## Slack

**Simplest: messages with link buttons.**

1. Create an app at <https://api.slack.com/apps> and turn on **Incoming
   Webhooks**.
2. Choose **Add New Webhook to Workspace**, pick a channel and copy the URL.
3. Create the channel: `{"type": "slack", "config": {"webhook_url": "https://hooks.slack.com/services/…"}}`.

Messages carry **Approve… / Reject…** buttons that open the confirmation page.

**Interactive buttons (decide inside Slack):**

1. In the same app, open **Interactivity & Shortcuts**, turn it on and set the
   Request URL to `https://<your-api>/v1/integrations/slack/interactions`.
   The channel's `integration_url` shows this value.
2. Copy the **Signing Secret** from **Basic Information**.
3. Set `signing_secret` and `approver_user_ids` (Slack member IDs, `U…`; find
   them under *Profile → ⋯ → Copy member ID*).

Requests are verified with `X-Slack-Signature` and must be no more than 5
minutes old. Buttons pressed by anyone not on the list are refused. The list
is required: without it (or without the signing secret) messages fall back
to one-tap link buttons. Interactive messages never contain one-tap links —
anyone who can read the Slack channel could use those. After a decision, the
message is updated to show the result. **Reject with reason…** opens the
item in the dashboard (signed in), where you can reject with a reason.

Instead of a webhook, you can use `bot_token` (`xoxb-…`, scope `chat:write`)
plus `channel` (a channel ID). Interactive buttons need a public HTTPS URL, so
they won't work on a laptop. Socket Mode isn't supported.

## Discord

**Simplest: a channel webhook.** Go to *Server Settings → Integrations →
Webhooks → New Webhook* and copy the URL.

```json
{"type": "discord", "config": {"webhook_url": "https://discord.com/api/webhooks/…"}}
```

Messages are embeds with **Approve… / Reject…** links. Image items are
attached. `@everyone` and other mentions are neutralized.

**Buttons (decide inside Discord):**

1. Create an application at <https://discord.com/developers/applications>.
2. Under **Bot**, create a bot, copy its **token**, and invite it to your
   server with the *Send Messages* permission.
3. Copy the **Application ID** and **Public Key** from **General
   Information**.
4. Create the channel with `bot_token`, `channel` (channel ID, from *Developer
   Mode → Copy Channel ID*), `application_id`, `public_key` and
   `approver_user_ids` (Discord user IDs).
5. Set **Interactions Endpoint URL** to
   `https://<your-api>/v1/integrations/discord/interactions`. Discord sends a
   signed PING to check it, and staqly answers.

Each request is verified with Ed25519 (`X-Signature-Ed25519`) and must be no
more than 5 minutes old. **Reject with reason** opens a Discord form, bound
to the item version the message showed (a changed item gets "item changed
since this message — open it in staqly" instead of a decision). Like
Slack, this needs a public HTTPS URL.

## ntfy

```json
{"type": "ntfy", "config": {"topic": "staqly-7f3k9q2", "server_url": "https://ntfy.sh", "access_token": "tk_…", "priority": 4}}
```

1. Install the ntfy app.
2. Subscribe to the topic.

On ntfy.sh, **anyone who knows the topic name can read it**, so use a long
random topic or a protected topic with an access token (`access_token` is
sent as `Authorization: Bearer`).

Notifications have **Approve / Reject** actions, which POST the one-tap link,
and **Reject with reason**, which opens the confirmation page. Tapping the
notification opens the item. The action buttons can only reach staqly if
your phone can reach `NOTIFY_PUBLIC_API_URL`.

## Email (SMTP)

```json
{"type": "email", "config": {"host": "smtp.example.com", "port": 587, "username": "apikey",
 "password": "…", "from": "staqly <bot@example.com>", "to": ["me@example.com"], "tls": "starttls"}}
```

* `tls`:
  * `starttls` (default, port 587) requires the server to offer STARTTLS.
  * `tls` is implicit TLS (port 465).
  * `none` is plaintext, only for a trusted local relay. Credentials are never
    sent over a plaintext connection except to localhost.
* Messages are multipart text and HTML with Approve / Reject links and a
  dashboard link. Up to 20 recipients.

## Local vs cloud

| | Local (laptop) | Cloud / public URL |
|---|---|---|
| Telegram buttons | ✅ polling mode | ✅ polling or webhook |
| Slack / Discord messages | ✅ | ✅ |
| Slack / Discord buttons | ❌ needs a public HTTPS URL | ✅ |
| One-tap links (ntfy, email, …) | only on devices that can reach `NOTIFY_PUBLIC_API_URL` | ✅ |

Outbound requests are SSRF-guarded: by default only public internet
addresses are allowed, and cloud metadata or link-local addresses are always
blocked, as are IPv6 forms that embed such an IPv4 address (NAT64, 6to4,
IPv4-compatible) and reserved ranges. Carrier-grade NAT space
(`100.64.0.0/10`, also used by Tailscale) counts as private. Set
`NOTIFY_ALLOW_PRIVATE=true` for a LAN target such as a
self-hosted ntfy or SMTP relay. Set `NOTIFY_ALLOW_LOOPBACK=true` for a target
on the same host.

## Configuration

| Env | Default | Meaning |
|---|---|---|
| `NOTIFY_SECRET_KEY` | — | Key material for encrypting channel secrets (AES-256-GCM, key = SHA-256); **at least 32 bytes** (e.g. `openssl rand -hex 32`), shorter values refuse to start. If unset, a random key is generated once into `DATA_DIR/notify_secret.key` (mode 0600). **Back it up with the database**: without it, stored channel secrets can't be decrypted and must be re-entered. |
| `NOTIFY_PUBLIC_API_URL` | `PUBLIC_API_URL` | Base URL for decision links and the Telegram webhook |
| `PUBLIC_WEB_URL` | `http://localhost:8080` | Dashboard base for item links |
| `NOTIFY_MAX_FAILS` | `10` | Consecutive failures before a channel is disabled |
| `NOTIFY_MAX_ATTEMPTS` | `5` | Attempts per queued notification |
| `NOTIFY_RETRY_BASE` | `30s` | First retry delay (doubles, capped at 30 min) |
| `NOTIFY_POLL_INTERVAL` | `2s` | Outbox worker interval |
| `NOTIFY_CONCURRENCY` | `4` | Channels sent in parallel per pass |
| `RETENTION_NOTIFICATIONS_DAYS` | `30` | Finished outbox rows are pruned after N days; `0` keeps them |
| `NOTIFY_DISABLED_GRACE` | `24h` | How long a disabled channel keeps its queued messages; after that they are marked `skipped` |
| `NOTIFY_LINK_TTL` | `168h` | Decision link lifetime |
| `NOTIFY_ALLOW_PRIVATE` / `NOTIFY_ALLOW_LOOPBACK` | `false` | SSRF policy for channel targets |
| `NOTIFY_TELEGRAM_AUTOREGISTER` | `true` | Call `setWebhook`/`deleteWebhook` when a Telegram channel is saved |
| `TELEGRAM_POLL_INTERVAL` | `3s` | `getUpdates` interval in polling mode |
| `TELEGRAM_API_BASE`, `SLACK_API_BASE`, `DISCORD_API_BASE` | official APIs | Overrides (tests, proxies) |

## Security notes

* **Secrets.** Channel secrets are encrypted at rest, masked in every
  response, and never logged. Error messages never include bot tokens.
* **Chat buttons.** Every button payload carries an HMAC made with a
  per-channel key, so a modified client can't forge decisions for other items
  or actions. Approve, Reject and Reject-with-reason payloads (including the
  Telegram reason prompt and the Discord reason form) also carry the item
  version shown, so a stale button can't decide content that changed after
  the message. Older unversioned payloads still work, unbound, like before.
* **Allowlists.** Decisions from chat apply only to items in the channel's own
  workspace, and only from users on the channel's approver allowlist (for
  Telegram, the private-chat owner by default).
* **Webhooks and interactions.** Each one is verified: Telegram with its
  secret token, Slack with an HMAC signature, Discord with Ed25519, the last
  two within a 5-minute window.
* **Decision links.** The confirmation page has no JavaScript and a strict
  Content-Security-Policy. Tokens are kept out of request logs, and per-IP
  rate limits apply.
* **Untrusted content.** Item text is escaped for every target (Telegram HTML,
  Slack mrkdwn, Discord markdown and mentions, email HTML).
