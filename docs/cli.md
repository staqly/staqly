# staqly CLI

`staqly` is the command line client for staq.ly. Use it to push AI output
for review, block a script until a human decides, review the inbox from a
terminal, and manage collections, rules, watchers, webhooks and API keys.

It is a single static Go binary in `apps/cli`, with its own Go module. It
talks to the API only over HTTP (the contract is `apps/api/openapi.yaml`),
so it works against a local server or a cloud workspace.

## Install

```bash
# From the monorepo
cd apps/cli
make build            # → apps/cli/bin/staqly
make install          # → $(go env GOPATH)/bin/staqly

# With Go only
go install github.com/staqly/staqly/apps/cli/cmd/staqly@latest
```

Release archives for darwin, linux and windows (amd64 and arm64) are built
by `apps/cli/.goreleaser.yaml`. To build them locally without publishing,
run `goreleaser release --snapshot --clean`.

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

Some commands use `--url` for a different URL: `items create` and `push`
(the item's URL), `webhooks create` (the receiver URL) and
`watchers create` (the feed or page URL). With those commands, set the API
address with `--api-url` or `STAQLY_API_URL`.

### Profiles

```bash
staqly login                                                  # local server, keyless → profile "default"
staqly login --url https://staq.example.com --key stk_… --profile work
echo "$KEY" | staqly login --url https://staq.example.com --key - --profile ci
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
    "work": { "url": "https://staq.example.com", "key": "stk_…" }
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

Run `staqly <command> --help` to see every flag, with examples.

### status

```bash
staqly status            # server health + version, mode, workspace, principal, masked key, plan, usage vs limits, pending count
staqly status --json
```

### push (create an item and wait for a decision)

`push` is `items create … --wait` under a shorter name.

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
staqly items approve itm_1 [itm_2 …] [--feedback "…"|-m "…"] [--expected-version N]
staqly items reject  itm_1 [--feedback "…"]
staqly items action  itm_1 regenerate [--feedback "make her smile"] [--payload '{"n":2}']
staqly items update  itm_1 [--status completed] [--metadata-patch '{"url":"…","old":null}'] [--metadata '{…}'] [--title] [--content] [--url] [--collection] [--expected-version]
staqly items history itm_1                # activity, oldest first
staqly items result  itm_1 --status completed|failed [--result '{"url":"…"}'] [--error "…"]
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
| `g` | Refresh |
| `q` or Ctrl-C | Quit |

The list refreshes every `--interval` (default `5s`).

### collections

```bash
staqly collections list [--archived]
staqly collections create "Instagram Avatar" [--description … --icon … --color … --view gallery|table|list]
staqly collections get "Instagram Avatar"
staqly collections rename "Instagram Avatar" "Avatars"
staqly collections archive Avatars [--undo]
staqly collections delete Avatars [--yes]   # without --yes: type the name to confirm (refused when stdin is not a terminal)
```

### events

```bash
staqly events tail                          # last 20 events, oldest first
staqly events tail --follow --type 'item.*' # poll for new events (--interval 2s)
staqly events tail -f --json | jq -c 'select(.type=="item.rejected")'
staqly events tail --item itm_1 --collection Avatars --actor-type api_key --limit 100
```

`--follow` keeps track of the highest `seq` it has printed and pages back
until no event is missed. An exact `--type` is filtered by the server. A
glob such as `item.*` is filtered by the CLI.

### webhooks

```bash
staqly webhooks list                         # secrets masked
staqly webhooks create --url https://example.com/hook --events item.approved,item.rejected [--collection Avatars] [--description …] [--timeout-ms 10000] [--max-attempts 8] [--inactive]
staqly webhooks test wh_123
staqly webhooks deliveries [--webhook wh_123] [--status failed] [--limit 50]
staqly webhooks retry dlv_123
staqly webhooks delete wh_123
```

`create` prints the signing secret once.

### rules

```bash
staqly rules list
staqly rules create --name "Auto-approve staging" --action auto_approve --collection Deploys --when 'env eq staging' --when 'amount lte 100'
staqly rules create --name "Expire stale" --action expire --after 72h
staqly rules create --name "Big spend" --action escalate --when 'amount gt 1000' --after 1h --note "ping finance"
staqly rules create --file rule.json         # full RuleInput JSON ('-' = stdin); cannot be combined with flags
staqly rules delete rul_123
```

Rule flags:

* `--when 'path op value'` adds a metadata condition. The operators are
  `eq ne lt lte gt gte contains in not_in exists`.
* For `in` and `not_in`, give a comma list or a JSON array.
* Other match flags: `--type`, `--source` (a glob) and `--contains`.
* Parameter flags: `--feedback`, `--set-status`, `--after` (a duration) and
  `--note`.

### watchers

```bash
staqly watchers presets
staqly watchers create --preset github-releases --param owner=golang --param repo=go --collection Releases [--every 2h] [--name …] [--paused]
staqly watchers create --kind rss --url https://blog.example.com/feed.xml --collection Reading --keyword AI --exclude crypto
staqly watchers create --kind url_diff --url https://example.com/pricing --collection Competitors
staqly watchers list
staqly watchers run wat_123        # check now; exit 1 if the source errored
staqly watchers pause wat_123
staqly watchers resume wat_123
staqly watchers delete wat_123
```

### keys (admin)

```bash
staqly keys list
staqly keys create --name "CI gate" --scope items:read,items:write [--expires 2027-01-01]   # key printed once
staqly keys revoke key_123
```

API keys are a cloud-mode feature. A local server accepts them too, but
doesn't need them.

### mcp config

`mcp config` prints a ready-to-use MCP configuration for an agent client,
pointing at the current profile's server (`<url>/mcp`):

```bash
staqly mcp config claude                     # `claude mcp add …` command + .mcp.json snippet
staqly mcp config codex                      # `codex mcp add …` + ~/.codex/config.toml
staqly mcp config opencode --stdio           # docker exec -i staqly-api mcp-stdio
staqly mcp config cursor --snippet > .cursor/mcp.json
```

When a key is configured, the snippet references `$STAQLY_API_KEY`. The key
itself is never printed.

Notes per client:

* **Codex, STDIO with a key.** `codex mcp add --env STAQLY_API_KEY=…`
  stores the key's value in a `[mcp_servers.staqly.env]` table in
  `~/.codex/config.toml`. So the CLI prints only the command (with
  `$STAQLY_API_KEY`, which your shell expands) and no TOML. Local-mode STDIO
  needs no key: `codex mcp add staqly -- docker exec -i staqly-api mcp-stdio`.
  HTTP with a key uses `bearer_token_env_var = "STAQLY_API_KEY"`.
* **Cursor.** The format follows Cursor's MCP docs
  (<https://cursor.com/docs/context/mcp>). The config goes in
  `.cursor/mcp.json` (project) or `~/.cursor/mcp.json` (global). A remote
  server is `{"mcpServers":{"staqly":{"url":"…/mcp"}}}`, plus
  `"headers":{"Authorization":"Bearer ${env:STAQLY_API_KEY}"}` in cloud mode.
  STDIO uses `"type":"stdio"`, `command`, `args` and `env`. Cursor expands
  `${env:NAME}`. The snippets match [mcp.md](mcp.md) and
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
    STAQLY_API_URL: https://staq.example.com
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
  *) echo "staq.ly error"; exit 1 ;;
esac
```

### Close the loop: approve, act, report

```bash
id=$(staqly push Social --title "Post" --content "$text" --json | jq -r .id) || exit $?
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
staqly items list --status pending --q "typo fix" --all --json | jq -r '.data[].id' | xargs staqly items approve --feedback "bulk"
```

### React to decisions in a shell loop

```bash
staqly events tail -f --type item.approved --json | while read -r ev; do
  id=$(jq -r .item_id <<<"$ev"); publish "$id" && staqly items update "$id" --status completed
done
```

## Testing

```bash
cd apps/cli
make test          # go vet + go test -race (httptest fake server for every command group)
# Live smoke test against a real local-mode server:
(cd ../api && DATA_DIR=/tmp/staqly-cli ADDR=127.0.0.1:18790 go run ./cmd/server) &
make test-live     # STAQLY_LIVE=1: push --wait + approve → exit 0 (then items result), reject → exit 2, timeout → exit 3, 409 on double decision, --expires-in/--editable
```
