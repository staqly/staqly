# staqly CLI

`staqly` is the command line client for staqly. Use it to push AI output
for review, block a script until a human decides, review the inbox from a
terminal, and manage collections, media, watchers, webhooks,
notification channels, API keys, sessions and the workspace (usage, plan,
exports).

It is a single static Go binary in `apps/cli`, with its own Go module. Its
client commands talk to the API only over HTTP (the contract is
`apps/api/openapi.yaml`), so they work against a local server or a cloud
workspace. `staqly serve` also runs the server itself, without Docker.

## Install

```bash
# Latest release (macOS, Linux; amd64, arm64) → ~/.local/bin/staqly
curl -fsSL https://raw.githubusercontent.com/staqly/staqly/main/scripts/install.sh | sh
# a specific version / another directory
curl -fsSL https://raw.githubusercontent.com/staqly/staqly/main/scripts/install.sh | STAQLY_VERSION=v0.2.0 STAQLY_INSTALL_DIR=/usr/local/bin sh
```

The script verifies the archive against the release's `checksums.txt`.
Windows: download the `.zip` from the
[releases](https://github.com/staqly/staqly/releases).

```bash
# From the monorepo (make dashboard first embeds the dashboard for `staqly serve`)
make dashboard
cd apps/cli
make build            # → apps/cli/bin/staqly
make install          # → $(go env GOPATH)/bin/staqly

```

Releases: `make release VERSION=v0.2.0` tags `origin/main` (only if its CI
is green) and `.github/workflows/release.yml` publishes the archives for
darwin, linux and windows (amd64, arm64) with `apps/cli/.goreleaser.yaml`,
dashboard included. To build them locally without publishing, run
`goreleaser release --snapshot --clean` in `apps/cli`.

```bash
staqly version
source <(staqly completion bash)    # or: zsh; fish → staqly completion fish > ~/.config/fish/completions/staqly.fish
```

## Configuration

| Setting | Flag | Environment | Profile field | Default |
|---|---|---|---|---|
| API base URL | `--api-url` (alias `--url`) | `STAQLY_API_URL` | `url` | `http://localhost:3333` |
| API key | `--key` | `STAQLY_API_KEY` | `key` | none (local mode is keyless) |
| Profile | `--profile` | `STAQLY_PROFILE` | `current` in the file | `default` |
| Config file | — | `STAQLY_CONFIG` | — | `$XDG_CONFIG_HOME/staqly/config.json`, else `~/.config/staqly/config.json` |

When a setting is given in more than one place, the order of precedence is:
flag, then environment variable, then the selected profile, then the
default.

A key stored in a profile is only sent to that profile's own URL. If
`--api-url` or `STAQLY_API_URL` points somewhere else, the CLI sends no key
and prints a note; pass `--key` or `STAQLY_API_KEY` to authenticate against
the other server. The CLI also warns when a key would go over plain `http://`
to a host other than `localhost` / loopback: use `https://` for remote servers.

Some commands use `--url` for a different URL: `items create` and `push`
(the item's URL), `webhooks create` and `webhooks update` (the receiver
URL) and `watchers create` (the feed or page URL). With those commands, set the API
address with `--api-url` or `STAQLY_API_URL`.

### Profiles

```bash
staqly login                                                  # local server, keyless → profile "default"
staqly login --url https://staqly.example.com --key stk_... --profile work
echo "$KEY" | staqly login --url https://staqly.example.com --key - --profile ci
staqly profiles                 # list (keys masked); * marks the current profile
staqly profiles use work        # switch
staqly status --profile ci      # one-off
staqly logout --profile ci      # remove a profile
```

`login` checks the credentials against `GET /v1/session` before it saves
anything. Pass `--no-verify` to skip the check. It writes
`~/.config/staqly/config.json` (directory `0700`, file `0600`) and makes the
new profile current. If you don't pass `--key`, `login` uses
`STAQLY_API_KEY`, or asks for the key at a hidden prompt.

```json
{
  "current": "work",
  "profiles": {
    "default": { "url": "http://localhost:3333" },
    "work": { "url": "https://staqly.example.com", "key": "stk_..." }
  }
}
```

### Output

* By default, output is human-readable tables and key/value lines.
* `--json` prints the API response unchanged, pretty-printed. Commands that
  stream (`events tail`, `inbox --watch`) print NDJSON, one JSON object per
  line.
* In `--json` mode errors go to stderr as
  `{"error":{"code","status","message","fields","details","retry_after_seconds"}}`.
* Colors are used only when stdout is a terminal. `NO_COLOR=1`,
  `TERM=dumb` and `--no-color` turn them off.
* Downloads (`media get`, `workspace export download`, `events export`)
  write the raw file. Binary data is never written to a terminal: without
  `--output` it goes to stdout when stdout is piped, otherwise to a new file
  named after the server's file name (an existing file is never
  overwritten). When a file is written, `--json` prints
  `{"path","bytes","content_type"}`.
* Text from the server (titles, content, metadata, event data, error
  messages) is untrusted. Human output strips control characters, ANSI/OSC
  escape sequences (screen clearing, clipboard writes, hyperlinks) and Unicode
  bidi overrides before printing. `--json` keeps the data intact but writes C1
  controls and bidi overrides as `\uXXXX` escapes, so the JSON is equivalent
  and still safe to view in a terminal.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success. With `--wait` or `push`: **approved**. |
| 1 | Error: API error, network error, failed confirmation, or one or more items in a multi-item command failed. |
| 2 | With `--wait` or `push`: **rejected**. |
| 3 | With `--wait` or `push`: **timed out** (still pending), or the item **expired**. |
| 4 | With `--wait` or `push`: the item left `pending` with another status (for example a custom action moved it to `processing`, or it was archived). |
| 64 | Usage error: unknown command or flag, or missing arguments. |
| 130 | Interrupted (Ctrl-C) during a command. `--follow` and `--watch` exit 0 on Ctrl-C. |

## Error messages

API errors are shown in plain language:

* **401**: the key is invalid or revoked. The message suggests `--key` or `staqly login`.
* **402 `plan_limit_exceeded`**: shows the limit, how much is used, the maximum and the plan (`details`).
* **403 `not_local`**: a keyless request came from another machine. Run on the server host, or use a key.
* **403 (scope)**: the key is missing a scope.
* **409**: the resource changed or was already decided. Fetch it again and retry.
* **422**: the error lists each invalid field.
* **429**: shows the `Retry-After` value.
* **5xx**: a server error. Check the server logs.

If the server can't be reached, the error names the URL and suggests
`--url` or `STAQLY_API_URL`.

## Commands

### serve (run staqly without Docker)

```bash
staqly serve                        # API + MCP on 127.0.0.1:3333, dashboard on http://localhost:8080
staqly serve --expose               # dashboard on all interfaces (phones); browsers sign in once
staqly serve connect-link           # one-time sign-in link for a browser
staqly serve backup ~/staqly.db     # consistent backup while the server runs
staqly serve workspaces             # ID, plan, plan end, created, name
staqly serve stats                  # operator statistics as JSON
staqly serve recovery-key [ws_id]   # new recovery key; signs out the workspace's browsers
staqly serve set-plan ws_123 pro --until 2027-01-01
staqly serve --data ~/staqly-data --port 4333 --dashboard-port 9090
```

One process: the same server as the `staqly` image (package
`apps/api/server`) plus the embedded dashboard (`DASHBOARD_ADDR`). Data
defaults to `~/.local/share/staqly` (`--data` / `STAQLY_DATA_DIR`). The
dashboard on loopback needs no sign-in; with `--expose` every browser signs
in once. Other server settings (such as `STAQLY_MODE` and `PUBLIC_DASHBOARD_URL`) are read
from the environment as usual; `serve` defaults `PUBLIC_API_URL` to
`http://localhost:<port>` and `LOG_FORMAT` to `text`.

Run `staqly <command> --help` to see every flag, with examples.

Command aliases: `items` `item`; `collections` `collection`, `cols`;
`media` `files`; `webhooks` `webhook`, `hooks`; `channels` `channel`,
`notification-channels`, `notifications`; `watchers`
`watcher`, `watch`; `workspace` `ws`; `plans` `plan`; `keys` `key`,
`api-keys`; `sessions` `session`; `profiles` `profile`.

### service (run staqly serve in the background)

```bash
staqly service install [--data DIR] [--port 3333] [--dashboard-port 8080] [--expose]
staqly service status
staqly service logs [-f]
staqly service uninstall            # your data is kept
```

Installs `staqly serve` as a background service of your user: a macOS
LaunchAgent (started at login) or a Linux systemd user unit (started at boot),
restarted if it stops. `STAQLY_*`, `TAGGER_*`, `PUBLIC_*`, `LOG_*`...
settings in the environment at install time are kept. Running `install` again
replaces it.

### status

```bash
staqly status            # server health + version, mode, workspace, principal, masked key, plan, usage vs limits, pending count
staqly status --json
```

### push (create an item and wait for a decision)

`push` is `items create <collection> --wait` under a shorter name.

```bash
staqly push "Blog posts" --title "Launch post" --content @post.md
echo "Delete 3 stale branches?" | staqly push Ops --title "Branch cleanup" --stdin
staqly push Avatars --file avatar.png --meta prompt="smiling" --meta seed=42
staqly push Deploys --title "Deploy v1.4" --external-ref "$GITHUB_SHA" --timeout 1h
staqly push Deploys --title "FYI" --no-wait
staqly push Ops --title "Drop table?" --expires-in 24h --editable title,metadata.sql
```

How `push` waits:

* It long-polls `GET /v1/items/{id}/wait?timeout=N` (up to 60 s per request)
  until the item is no longer `pending`, or until `--timeout` passes
  (default `30m`). There is no busy polling: the server holds each request
  open, and the CLI only backs off (1 s, growing ×1.5 up to 10 s) if a
  response comes back immediately.
* It keeps retrying after network errors, 5xx and 429 responses, and honours
  `Retry-After`.
* Older servers without `/wait` are detected (a 404 on `/wait` while the item
  exists), and the CLI falls back to polling `GET /v1/items/{id}`.
* An item that reaches `expired` (see `--expires-in`) exits with 3, the same
  as a timeout.
* At the end it prints the decision and the reviewer's feedback, and exits
  with the matching code (see [Exit codes](#exit-codes)).

With `--json` it prints the final item.

### items

```bash
staqly items list [--collection C] [--status pending] [--type image] [--q text] [--source s] [--sort created_asc] [--limit 50] [--cursor X] [--all]
staqly items get itm_123                  # fields, metadata, content, available actions
staqly items create <collection> [--title --content TEXT|-|@file --stdin --file PATH --url --type --metadata '{json}' --meta k=v --source --external-ref --status --idempotency-key --expires-in 24h --editable title,content] [--wait [--timeout 30m]]
staqly items approve itm_1 [itm_2 itm_3] [--feedback "<text>"|-m "<text>"] [--expected-version N]
staqly items reject  itm_1 [--feedback "<text>"]
staqly items action  itm_1 regenerate [--feedback "make her smile"] [--payload '{"n":2}']
staqly items update  itm_1 [--status completed] [--metadata-patch '{"url":"https://example.com/post/1","old":null}'] [--metadata '{"seed":42}'] [--title] [--content] [--url] [--collection] [--external-ref] [--media-id med_1] [--expected-version]
staqly items history itm_1                # activity, oldest first
staqly items versions itm_1               # snapshots before each change
staqly items result  itm_1 --status completed|failed [--result '{"url":"https://example.com/post/1"}'] [--error "<text>"]
staqly items bulk <action> itm_1 itm_2 [--stdin] [--feedback "<text>"] [--payload '{"n":2}']
staqly items delete itm_1 [itm_2 itm_3]   # history is kept
staqly items purge itm_1 [--yes]          # item + its events + unused media (admin)
```

* `--collection` accepts the collection's name or its ID.
* `--file` uploads the file as the item's media (`multipart/form-data`).
* `--content @path` reads text content from a file. `--content -` or
  `--stdin` reads it from stdin.
* `--meta k=v` values that parse as JSON keep their type: `seed=42` becomes
  a number, `tags=["a"]` an array.
* `--expires-in` takes a duration (`24h`) or a number of seconds (60 s to
  90 days). A pending item that nobody decides in that time becomes
  `expired`.
* `--editable` lists the fields a reviewer may edit before approving:
  `title`, `content`, `url`, `media_id`, `metadata` or `metadata.<key>`.
* `items result` reports what happened after you acted on an approved item.
  It sets the status to `completed` or `failed` and stores `--result` JSON in
  `metadata.result` and `--error` in `metadata.error`.
* JSON creates send an automatic `Idempotency-Key`. Pass your own key with
  `--idempotency-key` to make retries from your own scripts safe.
* `items update --media-id <media-id>` attaches a file uploaded with
  `staqly media upload`; `--media-id ""` removes the item's media.
* `items bulk` sends `POST /v1/items/bulk-actions` (100 IDs per request) with
  `approve`, `reject` or a custom action. IDs come from the arguments and,
  with `--stdin`, from stdin (whitespace separated). Each item succeeds or
  fails on its own; the command exits 1 if any failed. `--json` prints
  `{"data":[{"id","ok","item"|"error"}]}`.
* `items delete` keeps the item's events as history. `items purge` (admin)
  also deletes the events, their webhook deliveries and the media when no
  other item uses it. It asks you to type the item ID; `--yes` skips that
  and is required when stdin is not a terminal.

| Flag (`items bulk`) | Type | Required | Description |
|---|---|---|---|
| `<action>` | string | <Required /> | `approve`, `reject` or a custom collection action such as `regenerate` |
| `--stdin` | boolean | No | Also read item IDs from stdin, separated by spaces or newlines |
| `--feedback`, `-m` | string | No | Feedback for the agent, applied to every item |
| `--payload` | JSON | No | Any JSON forwarded in each event and webhook |

### media

```bash
staqly media upload cover.png                                # prints the media ID
staqly media get med_oalirbz67y4m4cqxq5dw -o cover.png
staqly media get med_oalirbz67y4m4cqxq5dw --width 320 -o thumb.jpg
staqly media get med_oalirbz67y4m4cqxq5dw | file -           # piped: bytes go to stdout
staqly media delete med_oalirbz67y4m4cqxq5dw                 # items using it lose their media
```

| Flag (`media get`) | Type | Required | Description |
|---|---|---|---|
| `--output`, `-o` | path | No | File to write; `-` writes to stdout. Default: stdout when piped, else a new file named after the original |
| `--width` | integer | No | Thumbnail width for images: `160`, `320`, `640` or `1280` |

### inbox

```bash
staqly inbox                              # interactive on a terminal
staqly inbox --collection "Blog posts"
staqly inbox --no-interactive             # plain table
staqly inbox --watch                      # keep printing new pending items
staqly inbox --watch --json               # NDJSON stream of new pending items
```

Interactive keys:

| Key | Action |
|---|---|
| `j`/`k` or ↑/↓ | Move |
| Enter, Space or `o` | Show details |
| `a` | Approve |
| `r` | Reject; type feedback, then Enter to send or Esc to cancel |
| `g` or `R` | Refresh |
| `q`, Ctrl-D or Ctrl-C | Quit |

The list refreshes every `--interval` (default `5s`) and shows up to
`--limit` items (default `100`).

### collections

```bash
staqly collections list [--archived]
staqly collections create "Instagram Avatar" [--description <text>] [--icon <emoji or name>] [--color "#3b82f6"]
staqly collections get "Instagram Avatar"
staqly collections rename "Instagram Avatar" "Avatars"
staqly collections archive Avatars [--undo]
staqly collections delete Avatars [--yes]   # without --yes: type the name to confirm (refused when stdin is not a terminal)
```

`--color` sets the colour of the collection's dot in the dashboard (the
dashboard's palette: `#22c55e`, `#3b82f6`, `#8b5cf6`, `#ec4899`, `#ef4444`,
`#f97316`, `#eab308`, `#14b8a6`). `--view gallery|table|list` still exists
and sets `default_view`, but only for API compatibility: the dashboard always
shows a collection as a table.

### events

```bash
staqly events tail                          # last 20 events, oldest first
staqly events tail --follow --type 'item.*' # poll for new events (--interval 2s)
staqly events tail -f --json | jq -c 'select(.type=="item.rejected")'
staqly events tail --item itm_1 --collection Avatars --actor-type api_key --limit 100
staqly events tail --actor-id key_r7y65khtqa23kj4btvur --from 2026-10-01 --to 2026-10-14
```

`--follow` (`-f`) keeps track of the highest `seq` it has printed and pages
back until no event is missed (`--interval`, default `2s`, minimum `500ms`).
`--limit` is how many recent events to print first (default 20, max 200).
An exact `--type` is filtered by the server. A glob such as `item.*` is
filtered by the CLI after the server returns the last `--limit` events.
`--actor-type`, `--actor-id`, `--from` and `--to` (RFC3339 or `YYYY-MM-DD`)
are passed to the server as `actor_type`, `actor_id`, `from` and `to`.

```bash
staqly events export > events.ndjson        # GET /v1/events/export, oldest first, streamed
staqly events export --format csv --from 2026-10-01 --to 2026-11-01 -o october.csv
staqly events export --type 'item.*' --collection "Blog posts" --actor-type api_key
```

| Flag (`events export`) | Type | Required | Description |
|---|---|---|---|
| `--format` | string | No | `ndjson` (default) or `csv` (`created_at,id,type,actor_type,actor_id,actor_name,collection_id,item_id,data`) |
| `--from` | time | No | Created at or after, RFC3339 or `YYYY-MM-DD` |
| `--to` | time | No | Created before, RFC3339 or `YYYY-MM-DD` |
| `--type` | string | No | Exact type, or a prefix ending in `*` (`item.*` is sent as `type_prefix=item.`; can't be combined with `--type-prefix`) |
| `--type-prefix` | string | No | Literal type prefix |
| `--actor-type` | string | No | `local`, `session`, `api_key` or `system` |
| `--actor-id` | string | No | Only events by this actor |
| `--collection` | string | No | Collection name or ID |
| `--item` | string | No | Item ID |
| `--output`, `-o` | path | No | Write to this file instead of stdout |

Output to a terminal is sanitized like other server text; output to a pipe
or file is byte-for-byte what the server sent.

### webhooks

```bash
staqly webhooks list                         # secrets masked
staqly webhooks create --url https://example.com/hook --events item.approved,item.rejected [--collection Avatars] [--description <text>] [--timeout-ms 10000] [--max-attempts 8] [--inactive]
staqly webhooks get wh_123                   # secret masked
staqly webhooks update wh_123 [--url <url>] [--events 'item.*'] [--collection Avatars | --all-collections] [--description <text>] [--timeout-ms 10000] [--max-attempts 8] [--enable | --disable] [--rotate-secret]
staqly webhooks test wh_123
staqly webhooks deliveries [--webhook wh_123] [--status failed] [--limit 50]
staqly webhooks retry dlv_123
staqly webhooks delete wh_123
```

`create` prints the signing secret once. `update --rotate-secret` prints
the new secret once; the old one stops verifying deliveries. Other `update`
flags change only the fields you pass (`--collection` replaces the filter,
`--all-collections` clears it, `--description ""` clears the description).

### channels (notification channels)

```bash
staqly channels list
staqly channels get nch_oalirbz67y4m4cqxq5dw                 # settings, secrets masked
staqly channels create --type ntfy --name Phone --config topic=staqly-alerts-7f3k
echo '{"bot_token":"<token>"}' | staqly channels create --type telegram --name Team --config chat_id=-1001234567890 --config-json -
staqly channels update nch_oalirbz67y4m4cqxq5dw [--name <text>] [--config key=value] [--config-json <json>] [--events <patterns>] [--collection C | --all-collections] [--digest 5m] [--enable | --disable]
staqly channels test nch_oalirbz67y4m4cqxq5dw                # exit 1 if the message failed
staqly channels deliveries nch_oalirbz67y4m4cqxq5dw [--limit 50]
staqly channels delete nch_oalirbz67y4m4cqxq5dw
```

| Flag | Type | Required | Description |
|---|---|---|---|
| `--type` | string | <Required /> | `create` only: `telegram`, `slack`, `discord`, `ntfy` or `email` (immutable) |
| `--name` | string | <Required /> | Channel name (required for `create`, optional for `update`) |
| `--config` | `key=value` | No | One setting, repeatable. `port`/`priority` are numbers, `to`/`approver_user_ids` comma lists, `key=null` removes a setting |
| `--config-json` | JSON | No | Settings object; `-` reads stdin, `@path` a file (keeps secrets out of shell history). `--config` is merged on top |
| `--events` | string list | No | Event patterns (default `item.created`) |
| `--collection` | string list | No | Only these collections (names or IDs) |
| `--all-collections` | boolean | No | `update` only: every collection again |
| `--digest` | duration | No | Batch window, e.g. `5m`; `0` = immediately |
| `--disabled` | boolean | No | `create` only: create disabled |
| `--enable`, `--disable` | boolean | No | `update` only: enabling resets the failure counter; disabling revokes one-tap links |
| `--limit` | integer | No | `deliveries` only: how many (default 50, max 200) |

Secrets (`bot_token`, `webhook_url`, `signing_secret`, `access_token`,
`password`) are encrypted at rest and only ever returned masked. Needs the
`channels:manage` scope.

### watchers

```bash
staqly watchers create --url https://github.com/golang/go/releases.atom --collection Releases [--every 2h] [--name <text>] [--paused]
staqly watchers create --url https://blog.example.com/feed.xml --collection Reading --collection Social --keyword AI --exclude crypto
staqly watchers create --url https://example.com/pricing --collection Competitors
staqly watchers create --url https://example.com/feed.xml --collection Competitors --kind url_diff   # default: detected from the URL
staqly watchers list
staqly watchers get wat_123
staqly watchers run wat_123        # check now; exit 1 if the source errored
staqly watchers pause wat_123
staqly watchers resume wat_123
staqly watchers delete wat_123
```

### keys (admin)

```bash
staqly keys list
staqly keys create --name "CI gate" --scope items:read,items:write [--expires 2027-01-01]   # key printed once
staqly keys rotate key_123                   # revoke + replacement with the same name and scopes; key printed once
staqly keys revoke key_123
```

API keys are a cloud-mode feature. A local server accepts them too, but
doesn't need them.

### workspace and plans

```bash
staqly workspace get
staqly workspace rename "Acme content"                       # admin
staqly workspace usage                                       # plan, usage vs limits, trial end, suspension
staqly workspace export start [--wait] [--output staqly-export.zip] [--timeout 30m] [--interval 2s]
staqly workspace export status exp_oalirbz67y4m4cqxq5dw
staqly workspace export download exp_oalirbz67y4m4cqxq5dw [-o backup.zip]
staqly plans list
staqly plans free [--yes]                                    # also: staqly plan free
```

| Flag | Type | Required | Description |
|---|---|---|---|
| `--wait` | boolean | No | `export start`: poll until ready; exit 1 if it failed, 3 on timeout |
| `--output`, `-o` | path | No | `export start`: download the ZIP when ready (implies `--wait`). `export download`: file to write, `-` = stdout |
| `--timeout` | duration | No | `export start`: how long `--wait` waits (default `30m`) |
| `--interval` | duration | No | `export start`: poll interval (default `2s`) |
| `--yes` | boolean | No | `plans free`: skip the `[y/N]` prompt; required when stdin is not a terminal |

Not in the CLI: `DELETE /v1/workspace` and `POST /v1/recovery-key/rotate`.
The API only allows them from a signed-in dashboard session (API keys get
403, and local mode cannot delete its only workspace), so use the
dashboard.

### billing

Cloud workspaces with Stripe billing enabled (admin key):

```bash
staqly billing status        # plan, subscription status, paid-through date, suspended
staqly billing checkout      # prints a Stripe Checkout link to pay for Pro
staqly billing portal        # prints a link to change the card, get invoices or cancel
```

The links are for a person to open in a browser; the plan changes when Stripe confirms the
payment.

### connect and sessions

```bash
staqly connect                                   # one-time sign-in link for another browser (5 minutes, single use)
staqly connect --json | jq -r .url
staqly sessions list                             # admin
staqly sessions revoke ses_oalirbz67y4m4cqxq5dw
```

`connect` calls `POST /v1/device-links`, which only the dashboard or the
local principal may use (a cloud API key gets 403). The URL goes to stdout,
the expiry and a warning to stderr: the link is a credential, open it only
on your own device. When it points at `localhost`, set `PUBLIC_DASHBOARD_URL` on
the server so other devices can open it.

### mcp config

`mcp config` prints a ready-to-use MCP configuration for an agent client,
pointing at the current profile's server (`<url>/mcp`):

```bash
staqly mcp config claude                     # `claude mcp add` command + .mcp.json snippet
staqly mcp config codex                      # `codex mcp add` command + ~/.codex/config.toml
staqly mcp config opencode --stdio           # docker exec -i staqly mcp-stdio
staqly mcp config cursor --snippet > .cursor/mcp.json
staqly mcp config windsurf                   # ~/.codeium/windsurf/mcp_config.json snippet
```

When a key is configured, the snippet references `$STAQLY_API_KEY`. The key
itself is never printed.

Notes per client:

* **Codex, STDIO with a key.** `codex mcp add --env STAQLY_API_KEY=<key>`
  stores the key's value in a `[mcp_servers.staqly.env]` table in
  `~/.codex/config.toml`. So the CLI prints only the command (with
  `$STAQLY_API_KEY`, which your shell expands) and no TOML. Local-mode STDIO
  needs no key: `codex mcp add staqly -- docker exec -i staqly mcp-stdio`.
  HTTP with a key uses `bearer_token_env_var = "STAQLY_API_KEY"`.
* **Cursor.** The format follows Cursor's MCP docs
  (<https://cursor.com/docs/context/mcp>). The config goes in
  `.cursor/mcp.json` (project) or `~/.cursor/mcp.json` (global). A remote
  server is `{"mcpServers":{"staqly":{"url":"<api-url>/mcp"}}}`, plus
  `"headers":{"Authorization":"Bearer ${env:STAQLY_API_KEY}"}` in cloud mode.
  STDIO uses `"type":"stdio"`, `command`, `args` and `env`. Cursor expands
  `${env:NAME}`.
* **Windsurf.** One global file, `~/.codeium/windsurf/mcp_config.json`
  (Settings → Cascade → MCP servers → View raw config). A remote server is
  `{"mcpServers":{"staqly":{"serverUrl":"<api-url>/mcp"}}}`, plus the same
  `headers` with `${env:STAQLY_API_KEY}` in cloud mode; STDIO uses
  `command`, `args` and `env`.

The snippets match [mcp.md](mcp.md) and
`integrations/*`. `--container` changes the container name used with
`--stdio`.

### Other

```bash
staqly version [--json]
staqly completion bash|zsh|fish
staqly --help · staqly <command> --help
```

## Scripting recipes

### CI gate: require a human approval before deploying

```yaml
# GitHub Actions
- name: Wait for human approval
  env:
    STAQLY_API_URL: https://staqly.example.com
    STAQLY_API_KEY: ${{ secrets.STAQLY_API_KEY }}   # scopes: items:write items:read
  run: |
    staqly push Deploys \
      --title "Deploy ${GITHUB_REF_NAME} to production" \
      --content "$(git log -1 --pretty=%B)" \
      --url "$GITHUB_SERVER_URL/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID" \
      --external-ref "$GITHUB_SHA" \
      --idempotency-key "deploy-$GITHUB_SHA-$GITHUB_RUN_ATTEMPT" \
      --timeout 2h
- name: Deploy
  run: ./deploy.sh
```

Exit code 0 means approved, so the next step runs. 2 (rejected) and 3 (timed
out) fail the job.

To branch on the outcome:

```bash
staqly push Deploys --title "Release v1.4" --timeout 1h
case $? in
  0) ./deploy.sh ;;
  2) echo "Rejected, not deploying"; exit 1 ;;
  3) echo "Nobody decided in time"; exit 1 ;;
  *) echo "staqly error"; exit 1 ;;
esac
```

### Close the loop: approve, act, report

```bash
out=$(staqly push Social --title "Post" --content "$text" --json); code=$?
[ "$code" -eq 0 ] || exit "$code"          # rejected, timed out or error: stop here
id=$(jq -r .id <<<"$out")
if url=$(./publish.sh "$text"); then
  staqly items result "$id" --status completed --result "{\"url\":\"$url\"}"
else
  staqly items result "$id" --status failed --error "publish failed"
fi
```

### Read the reviewer's feedback

```bash
result=$(staqly push Copy --title "Tagline" --content "$draft" --json); code=$?
feedback=$(jq -r .feedback <<<"$result")
[ $code -eq 2 ] && regenerate --hint "$feedback"
```

### Approve everything matching a search

```bash
staqly items list --status pending --q "typo fix" --all --json | jq -r '.data[].id' | staqly items bulk approve --stdin --feedback "bulk"
```

### React to decisions in a shell loop

```bash
staqly events tail -f --type item.approved --json | while read -r ev; do
  id=$(jq -r .item_id <<<"$ev"); publish "$id" && staqly items result "$id" --status completed
done
```

## Testing

```bash
cd apps/cli
make test          # go vet + go test -race (httptest fake server for every command group)
# or from the repository root: make test-cli (pnpm turbo run test --filter=@staqly/cli)
# Live smoke test against a real local-mode server:
(cd ../api && DATA_DIR=/tmp/staqly-cli ADDR=127.0.0.1:18790 go run ./cmd/server) &
make test-live     # STAQLY_LIVE=1: push --wait + approve → exit 0 (then items result), reject → exit 2, timeout → exit 3, 409 on double decision, --expires-in/--editable
```
