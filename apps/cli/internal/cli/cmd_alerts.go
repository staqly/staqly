package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Alert struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	Type                string         `json:"type"`
	Enabled             bool           `json:"enabled"`
	Config              map[string]any `json:"config"`
	Events              []string       `json:"events"`
	CollectionIDs       []string       `json:"collection_ids"`
	DigestWindowSeconds int            `json:"digest_window_seconds"`
	FailCount           int            `json:"fail_count"`
	LastError           string         `json:"last_error"`
	DisabledReason      string         `json:"disabled_reason"`
	LastSentAt          *string        `json:"last_sent_at"`
	CreatedAt           string         `json:"created_at"`
}

type AlertDelivery struct {
	ID        string  `json:"id"`
	EventType string  `json:"event_type"`
	PostID    *string `json:"post_id"`
	Status    string  `json:"status"`
	Attempts  int     `json:"attempts"`
	LastError string  `json:"last_error"`
	SentAt    *string `json:"sent_at"`
	CreatedAt string  `json:"created_at"`
}

func alertsCommand() *Command {
	return &Command{
		Name: "alerts", Aliases: []string{"alert", "notifications"},
		Short: "Alerts about publishing on Telegram, Slack, Discord, ntfy or email",
		Subs: []*Command{
			leaf("list", "", "List alert channels", 0, 0, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var page struct{ Data []Alert }
					raw, err := cl.Get(c, "/v1/alerts", nil, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "NAME", "TYPE", "ENABLED", "EVENTS", "FAILS", "LAST SENT", "ERROR")
					for _, ch := range page.Data {
						t.row(ch.ID, truncate(ch.Name, 30), ch.Type, yesNo(ch.Enabled), strings.Join(ch.Events, ","),
							strconv.Itoa(ch.FailCount), fmtTime(strPtr(ch.LastSentAt)), truncate(orDash(ch.LastError), 40))
					}
					t.flush("No alerts. Create one with: staqly alerts create --type ntfy --name Phone --config topic=<topic>")
					return nil
				}
			}, "staqly alerts list"),

			leaf("get", "<id>", "Show an alert channel and its settings (secrets masked)", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var ch Alert
					raw, err := cl.Get(c, "/v1/alerts/"+url.PathEscape(args[0]), nil, &ch)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					printAlert(c, ch)
					return nil
				}
			}, "staqly alerts get nch_oalirbz67y4m4cqxq5dw"),

			alertWriteCommand(true),
			alertWriteCommand(false),

			leaf("delete", "<id>", "Delete an alert channel", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					return simpleDelete(c, "/v1/alerts/"+url.PathEscape(args[0]), "alert", args[0])
				}
			}, "staqly alerts delete nch_oalirbz67y4m4cqxq5dw"),

			leaf("test", "<id>", "Send a test message now (exit 1 if it fails)", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var res struct {
						OK    bool   `json:"ok"`
						Error string `json:"error"`
					}
					raw, err := cl.Post(c, "/v1/alerts/"+url.PathEscape(args[0])+"/test", nil, &res)
					if err != nil {
						return err
					}
					if c.JSON {
						if err := c.printRaw(raw); err != nil {
							return err
						}
						if !res.OK {
							return exitWith(ExitError)
						}
						return nil
					}
					if !res.OK {
						return &exitErr{code: ExitError, msg: "test message failed: " + res.Error}
					}
					fmt.Fprintf(c.Out, "Sent a test message to %s\n", args[0])
					return nil
				}
			}, "staqly alerts test nch_oalirbz67y4m4cqxq5dw"),

			leaf("deliveries", "<id>", "Show an alert channel's recent messages", 1, 1, func(fs *flag.FlagSet) runFunc {
				limit := fs.Int("limit", 50, "how many (max 200)")
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var page struct{ Data []AlertDelivery }
					raw, err := cl.Get(c, "/v1/alerts/"+url.PathEscape(args[0])+"/deliveries",
						url.Values{"limit": {strconv.Itoa(*limit)}}, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "EVENT", "POST", "STATUS", "ATTEMPTS", "CREATED", "SENT", "ERROR")
					for _, d := range page.Data {
						t.row(d.ID, d.EventType, orDash(strPtr(d.PostID)), c.status(d.Status), strconv.Itoa(d.Attempts),
							fmtTime(d.CreatedAt), fmtTime(strPtr(d.SentAt)), truncate(orDash(d.LastError), 40))
					}
					t.flush("No messages yet.")
					return nil
				}
			}, "staqly alerts deliveries nch_oalirbz67y4m4cqxq5dw --limit 20"),
		},
	}
}

// alertWriteCommand builds `alerts create` (create=true) or `alerts update`.
func alertWriteCommand(create bool) *Command {
	name, args, short, minArgs := "update", "<id>", "Update an alert channel (config keys merge; key=null removes one)", 1
	if create {
		name, args, short, minArgs = "create", "", "Create an alert channel", 0
	}
	cmd := leaf(name, args, short, minArgs, minArgs, func(fs *flag.FlagSet) runFunc {
		typ := new(string)
		if create {
			fs.StringVar(typ, "type", "", "telegram, slack, discord, ntfy or email (required)")
		}
		chName := fs.String("name", "", "alert name")
		var cfg multiFlag
		fs.Var(&cfg, "config", "setting `key=value` (repeatable), e.g. topic=staqly-alerts, chat_id=123, to=a@example.com,b@example.com")
		cfgJSON := fs.String("config-json", "", "settings as a JSON object; '-' reads stdin, '@path' reads a file (keeps secrets out of shell history)")
		events := &multiFlag{comma: true}
		fs.Var(events, "events", "event `patterns`, comma-separated or repeated (default post.published, post.failed, channel.needs_reconnect)")
		cols := &multiFlag{comma: true}
		fs.Var(cols, "collection", "limit to `collections` (ID or name; repeatable)")
		allCols := new(bool)
		if !create {
			fs.BoolVar(allCols, "all-collections", false, "alert for every collection again")
		}
		digest := fs.String("digest", "", "batch events within this `window` into one message, e.g. 5m (0 = send immediately)")
		disabled := new(bool)
		enable := new(bool)
		if create {
			fs.BoolVar(disabled, "disabled", false, "create disabled")
		} else {
			fs.BoolVar(enable, "enable", false, "enable the alert (also resets the failure counter)")
			fs.BoolVar(disabled, "disable", false, "disable the alert")
		}
		return func(c *Ctx, a []string) error {
			body := map[string]any{}
			setIf(body, "name", *chName)
			if create {
				if *typ == "" || *chName == "" {
					return usageErrf("--type and --name are required")
				}
				body["type"] = *typ
			}
			if *enable && *disabled {
				return usageErrf("--enable and --disable cannot be combined")
			}
			if *enable {
				body["enabled"] = true
			}
			if *disabled {
				body["enabled"] = false
			}
			config := map[string]any{}
			if *cfgJSON != "" {
				m, err := readJSONObjectArg(c, "config-json", *cfgJSON)
				if err != nil {
					return err
				}
				config = m
			}
			for _, kv := range cfg.vals {
				k, v, ok := strings.Cut(kv, "=")
				if !ok || k == "" {
					return usageErrf("--config expects key=value, got %q", kv)
				}
				config[k] = alertConfigValue(k, v)
			}
			if len(config) > 0 {
				body["config"] = config
			} else if create {
				return usageErrf("--config or --config-json is required (e.g. --config topic=<topic> for ntfy)")
			}
			if len(events.vals) > 0 {
				body["events"] = events.vals
			}
			if *digest != "" {
				secs, err := parseSeconds(*digest)
				if err != nil {
					return usageErrf("--digest must be a duration like 5m, or 0")
				}
				body["digest_window_seconds"] = secs
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			if *allCols {
				if len(cols.vals) > 0 {
					return usageErrf("--all-collections and --collection cannot be combined")
				}
				body["collection_ids"] = []string{}
			}
			if len(cols.vals) > 0 {
				ids := make([]string, len(cols.vals))
				for i, ref := range cols.vals {
					if ids[i], err = resolveCollectionID(c, cl, ref); err != nil {
						return err
					}
				}
				body["collection_ids"] = ids
			}
			var ch Alert
			var raw json.RawMessage
			if create {
				raw, err = cl.Post(c, "/v1/alerts", body, &ch)
			} else {
				if len(body) == 0 {
					return usageErrf("nothing to update; pass at least one of --name, --config, --config-json, --events, --collection, --all-collections, --digest, --enable, --disable")
				}
				raw, err = cl.Patch(c, "/v1/alerts/"+url.PathEscape(a[0]), body, &ch)
			}
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			verb := "Updated"
			if create {
				verb = "Created"
			}
			fmt.Fprintf(c.Out, "%s %s alert %s (%s)\n", verb, ch.Type, c.paint(bold, ch.Name), ch.ID)
			if create {
				fmt.Fprintf(c.Out, "Send a test message with: staqly alerts test %s\n", ch.ID)
			}
			return nil
		}
	})
	if create {
		cmd.Examples = []string{
			"staqly alerts create --type ntfy --name Phone --config topic=staqly-alerts-7f3k",
			"staqly alerts create --type telegram --name Team --config chat_id=-1001234567890 --config-json @telegram.json",
			"echo '{\"webhook_url\":\"https://hooks.slack.com/services/T000/B000/XXXX\"}' | staqly alerts create --type slack --name Reviews --config-json -",
			"staqly alerts create --type email --name Ops --config host=smtp.example.com --config port=587 --config from=staqly@example.com --config to=ops@example.com --events 'post.*' --digest 10m",
		}
		cmd.Long = `Create an alert channel: a message when a post is published or fails, or a
channel needs reconnecting. Settings depend on the type (* = required):

  telegram  bot_token*, chat_id*
  slack     webhook_url*, or bot_token* + channel*
  discord   webhook_url*, or bot_token* + channel*
  ntfy      topic*, server_url (default https://ntfy.sh), access_token, priority (1-5)
  email     host*, from*, to*, port, username, password, tls (starttls|tls|none)

Secrets (bot_token, webhook_url, access_token, password) are stored encrypted
and always shown masked. Pass them with --config-json - or --config-json @file
to keep them out of your shell history.`
	} else {
		cmd.Examples = []string{
			"staqly alerts update nch_oalirbz67y4m4cqxq5dw --disable",
			"staqly alerts update nch_oalirbz67y4m4cqxq5dw --events post.failed,channel.needs_reconnect --collection \"Blog posts\"",
			"staqly alerts update nch_oalirbz67y4m4cqxq5dw --config priority=5 --config access_token=null",
		}
	}
	return cmd
}

// alertConfigValue types a --config value: numbers for port/priority, a list
// for to, null removes the key, else a string.
func alertConfigValue(k, v string) any {
	switch {
	case v == "null":
		return nil
	case k == "port" || k == "priority":
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	case k == "to":
		if strings.HasPrefix(strings.TrimSpace(v), "[") {
			return scalarOrJSON(v)
		}
		out := []string{}
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return v
}

// parseSeconds accepts a Go duration (5m) or a whole number of seconds.
func parseSeconds(v string) (int, error) {
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return n, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid duration %q", v)
	}
	return int(d.Round(time.Second).Seconds()), nil
}

// readJSONObjectArg parses a JSON object given inline, as '-' (stdin) or as '@path'.
func readJSONObjectArg(c *Ctx, flagName, v string) (map[string]any, error) {
	switch {
	case v == "-":
		b, err := io.ReadAll(io.LimitReader(c.Env.Stdin, 1<<20))
		if err != nil {
			return nil, err
		}
		v = string(b)
	case strings.HasPrefix(v, "@"):
		b, err := os.ReadFile(v[1:])
		if err != nil {
			return nil, fmt.Errorf("read --%s: %w", flagName, err)
		}
		v = string(b)
	}
	return parseJSONObject(flagName, v)
}

func printAlert(c *Ctx, ch Alert) {
	cols := "all"
	if len(ch.CollectionIDs) > 0 {
		cols = strings.Join(ch.CollectionIDs, ", ")
	}
	digest := "immediately"
	if ch.DigestWindowSeconds > 0 {
		digest = (time.Duration(ch.DigestWindowSeconds) * time.Second).String()
	}
	c.kv("ID", ch.ID, "Name", ch.Name, "Type", ch.Type, "Enabled", yesNo(ch.Enabled), "Disabled because", ch.DisabledReason,
		"Events", strings.Join(ch.Events, ", "), "Collections", cols, "Digest", digest,
		"Failures", strconv.Itoa(ch.FailCount), "Last error", ch.LastError, "Last sent", fmtOptTime(ch.LastSentAt),
		"Created", fmtTime(ch.CreatedAt))
	if len(ch.Config) > 0 {
		fmt.Fprintln(c.Out)
		keys := make([]string, 0, len(ch.Config))
		for k := range ch.Config {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t := c.table("SETTING", "VALUE")
		for _, k := range keys {
			v := ch.Config[k]
			s, ok := v.(string)
			if !ok {
				b, _ := json.Marshal(v)
				s = string(b)
			}
			t.row(k, s)
		}
		t.flush("")
	}
}
